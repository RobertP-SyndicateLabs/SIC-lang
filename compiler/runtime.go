package compiler

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
)

// ---- ALTAR runtime ----

type altarServer struct {
	addr    string
	mux     *http.ServeMux
	started bool
}

var (
	altarMu     sync.Mutex
	globalAltar *altarServer
)

// Global entanglement state for the *current* CHAMBER.
//
// For v0 we model this as a single map that CHAMBER saves/restores,
// so nested CHAMBER blocks each see their own entanglement frame.
var entangledCores = map[string]bool{}

// ---- SIGIL ENVIRONMENT ----

type sigilTable map[string]string

func getSigil(sigils sigilTable, name string) (string, bool) {
	v, ok := sigils[name]
	return v, ok
}

func setSigil(sigils sigilTable, name, v string) {
	sigils[name] = v
}

func getSigilInt(sigils sigilTable, name string) (int64, error) {
	raw, ok := sigils[name]
	if !ok || raw == "" {
		// default 0 if unset
		return 0, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("SIGIL %s does not hold integer %q", name, raw)
	}
	return n, nil
}

func setSigilInt(sigils sigilTable, name string, v int64) {
	sigils[name] = strconv.FormatInt(v, 10)
}

// omenError is raised by RAISE OMEN and caught by OMEN ... FALLS_TO_RUIN.
type omenError struct {
	name string
}

func (e *omenError) Error() string {
	return "OMEN raised: " + e.name
}

// cloneSigils makes a shallow copy of the sigil table for transactional rollback.
func cloneSigils(in sigilTable) sigilTable {
	out := make(sigilTable, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// ---- OMEN STORAGE (layered on sigils) ----

const omenPrefix = "__OMEN__:"

func raiseOmen(sigils sigilTable, name string) {
	setSigil(sigils, omenPrefix+name, "1")
}

func clearOmen(sigils sigilTable, name string) {
	delete(sigils, omenPrefix+name)
}

func omenPresent(sigils sigilTable, name string) bool {
	v, ok := sigils[omenPrefix+name]
	return ok && v != "" && v != "0"
}

func clearAllOmens(sigils sigilTable) {
	for k := range sigils {
		if strings.HasPrefix(k, omenPrefix) {
			delete(sigils, k)
		}
	}
}

// ---- PUBLIC ENTRYPOINT ----

// RunFile: high-level entry to run a SIC Scroll.
func RunFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read error: %w", err)
	}

	src := string(data)
	lx := NewLexer(src, path)
	p := NewParser(lx)

	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "parse error:", e)
		}
		return fmt.Errorf("cannot run: parse failed")
	}

	return interpretProgram(prog)
}

func interpretProgram(prog *Program) error {
	if prog == nil {
		return fmt.Errorf("no program")
	}

	var mainWork *WorkDecl
	for _, w := range prog.Works {
		if w.Name == "MAIN" {
			mainWork = w
			break
		}
	}
	if mainWork == nil {
		return fmt.Errorf("no MAIN Work found")
	}

	sigils := make(sigilTable)
	_, err := execWork(prog, mainWork, sigils, false)
	return err
}

// findWork returns the WorkDecl with the given name, or nil.
func findWork(prog *Program, name string) *WorkDecl {
	for _, w := range prog.Works {
		if w.Name == name {
			return w
		}
	}
	return nil
}

// ---------------- Core execution over a Work ----------------

// cleanWorkBody strips the *header* newline from real WORK bodies,
// but leaves block bodies (IF / WHILE / OMEN / ARCWORK, etc.) alone.
//
// Heuristic: real WORK bodies coming from the parser *start* with a
// leading NEWLINE right after "WORK ... AS TEXT:". Synthetic block
// bodies we build at runtime start directly at the first real token.
func cleanWorkBody(raw []Token) []Token {
	if len(raw) == 0 {
		return raw
	}

	// Only strip a single leading NEWLINE, if present.
	if raw[0].Type == TOK_NEWLINE {
		return raw[1:]
	}

	return raw
}

// ----- Expression engine types -----

type exprKind int

const (
	exprText exprKind = iota
	exprInt
	exprFloat
	exprBool
)

type exprValue struct {
	kind exprKind
	s    string
	i    int64
	f    float64
	b    bool
}

func (v exprValue) String() string {
	switch v.kind {
	case exprInt:
		return fmt.Sprintf("%d", v.i)
	case exprFloat:
		return fmt.Sprintf("%g", v.f)
	case exprBool:
		if v.b {
			return "true"
		}
		return "false"
	case exprText:
		fallthrough
	default:
		return v.s
	}
}

func makeText(s string) exprValue   { return exprValue{kind: exprText, s: s} }
func makeInt(i int64) exprValue     { return exprValue{kind: exprInt, i: i} }
func makeFloat(f float64) exprValue { return exprValue{kind: exprFloat, f: f} }
func makeBool(b bool) exprValue     { return exprValue{kind: exprBool, b: b} }

// Try to treat value as float (int promotes to float, text parsed if possible).
func (v exprValue) asFloat() (float64, bool) {
	switch v.kind {
	case exprInt:
		return float64(v.i), true
	case exprFloat:
		return v.f, true
	case exprText:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v.s), 64); err == nil {
			return f, true
		}
	case exprBool:
		if v.b {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// Truthiness for boolean operators.
func (v exprValue) asBool() bool {
	switch v.kind {
	case exprBool:
		return v.b
	case exprInt:
		return v.i != 0
	case exprFloat:
		return v.f != 0
	case exprText:
		s := strings.TrimSpace(strings.ToLower(v.s))
		if s == "true" {
			return true
		}
		if s == "false" {
			return false
		}
		return s != ""
	default:
		return false
	}
}

// ----- Expression engine entry point -----
//
// evalStringExpr: given the tokens for an expression *and some trailing junk*,
// evaluate and return a string value.
//
// All callers may safely pass a larger slice; we will stop at "stop tokens"
// like DOT, COLON, FROM, TO, NEWLINE, ENDWORK, ENDWEAVE.
func evalStringExpr(prog *Program, tokens []Token, sigils sigilTable) (string, error) {
	if len(tokens) == 0 {
		return "", nil
	}

	// Trim off trailing control tokens that are not part of the expression.
	end := len(tokens)
	for idx, tok := range tokens {
		switch tok.Type {
		case TOK_DOT,
			TOK_COLON,
			TOK_NEWLINE,
			TOK_ENDWEAVE,
			TOK_ENDWORK,
			TOK_FROM:
			end = idx
			goto sliced
		}
	}
sliced:
	tokens = tokens[:end]
	if len(tokens) == 0 {
		return "", nil
	}

	i := 0
	val, err := parseOr(prog, tokens, &i, sigils)
	if err != nil {
		return "", err
	}
	return val.String(), nil
}

// Precedence:
//
// OR
// AND
// Equality (==, !=)
// Comparison (<, >, <=, >=)
// Term (+, -)
// Factor (*, /, %)
// Unary (-, NOT)
// Primary

func parseOr(prog *Program, tokens []Token, i *int, sigils sigilTable) (exprValue, error) {
	left, err := parseAnd(prog, tokens, i, sigils)
	if err != nil {
		return exprValue{}, err
	}
	for *i < len(tokens) && tokens[*i].Type == TOK_OR {
		*i++
		right, err := parseAnd(prog, tokens, i, sigils)
		if err != nil {
			return exprValue{}, err
		}
		left = makeBool(left.asBool() || right.asBool())
	}
	return left, nil
}

func parseAnd(prog *Program, tokens []Token, i *int, sigils sigilTable) (exprValue, error) {
	left, err := parseEquality(prog, tokens, i, sigils)
	if err != nil {
		return exprValue{}, err
	}
	for *i < len(tokens) && tokens[*i].Type == TOK_AND {
		*i++
		right, err := parseEquality(prog, tokens, i, sigils)
		if err != nil {
			return exprValue{}, err
		}
		left = makeBool(left.asBool() && right.asBool())
	}
	return left, nil
}

func parseEquality(prog *Program, tokens []Token, i *int, sigils sigilTable) (exprValue, error) {
	left, err := parseComparison(prog, tokens, i, sigils)
	if err != nil {
		return exprValue{}, err
	}
	for *i < len(tokens) &&
		(tokens[*i].Type == TOK_EQ || tokens[*i].Type == TOK_NEQ) {

		op := tokens[*i].Type
		*i++
		right, err := parseComparison(prog, tokens, i, sigils)
		if err != nil {
			return exprValue{}, err
		}

		var eq bool

		if lf, okL := left.asFloat(); okL {
			if rf, okR := right.asFloat(); okR {
				eq = lf == rf
			} else {
				eq = left.String() == right.String()
			}
		} else {
			eq = left.String() == right.String()
		}

		if op == TOK_EQ {
			left = makeBool(eq)
		} else {
			left = makeBool(!eq)
		}
	}
	return left, nil
}

func parseComparison(prog *Program, tokens []Token, i *int, sigils sigilTable) (exprValue, error) {
	left, err := parseTerm(prog, tokens, i, sigils)
	if err != nil {
		return exprValue{}, err
	}
	for *i < len(tokens) &&
		(tokens[*i].Type == TOK_LT ||
			tokens[*i].Type == TOK_LTE ||
			tokens[*i].Type == TOK_GT ||
			tokens[*i].Type == TOK_GTE) {

		op := tokens[*i].Type
		*i++
		right, err := parseTerm(prog, tokens, i, sigils)
		if err != nil {
			return exprValue{}, err
		}

		lf, okL := left.asFloat()
		rf, okR := right.asFloat()

		var res bool
		if okL && okR {
			switch op {
			case TOK_LT:
				res = lf < rf
			case TOK_LTE:
				res = lf <= rf
			case TOK_GT:
				res = lf > rf
			case TOK_GTE:
				res = lf >= rf
			}
		} else {
			ls := left.String()
			rs := right.String()
			switch op {
			case TOK_LT:
				res = ls < rs
			case TOK_LTE:
				res = ls <= rs
			case TOK_GT:
				res = ls > rs
			case TOK_GTE:
				res = ls >= rs
			}
		}

		left = makeBool(res)
	}
	return left, nil
}

func parseTerm(prog *Program, tokens []Token, i *int, sigils sigilTable) (exprValue, error) {
	left, err := parseFactor(prog, tokens, i, sigils)
	if err != nil {
		return exprValue{}, err
	}
	for *i < len(tokens) &&
		(tokens[*i].Type == TOK_PLUS || tokens[*i].Type == TOK_MINUS) {

		op := tokens[*i].Type
		*i++
		right, err := parseFactor(prog, tokens, i, sigils)
		if err != nil {
			return exprValue{}, err
		}

		lf, okL := left.asFloat()
		rf, okR := right.asFloat()

		if okL && okR {
			switch op {
			case TOK_PLUS:
				left = makeFloat(lf + rf)
			case TOK_MINUS:
				left = makeFloat(lf - rf)
			}
		} else {
			if op == TOK_PLUS {
				left = makeText(left.String() + right.String())
			} else {
				return exprValue{}, fmt.Errorf("cannot apply '-' to non-numeric values")
			}
		}
	}
	return left, nil
}

func parseFactor(prog *Program, tokens []Token, i *int, sigils sigilTable) (exprValue, error) {
	left, err := parseUnary(prog, tokens, i, sigils)
	if err != nil {
		return exprValue{}, err
	}
	for *i < len(tokens) &&
		(tokens[*i].Type == TOK_STAR ||
			tokens[*i].Type == TOK_SLASH ||
			tokens[*i].Type == TOK_PERCENT) {

		op := tokens[*i].Type
		*i++
		right, err := parseUnary(prog, tokens, i, sigils)
		if err != nil {
			return exprValue{}, err
		}

		lf, okL := left.asFloat()
		rf, okR := right.asFloat()
		if !okL || !okR {
			return exprValue{}, fmt.Errorf("non-numeric value in arithmetic")
		}

		switch op {
		case TOK_STAR:
			left = makeFloat(lf * rf)
		case TOK_SLASH:
			if rf == 0 {
				return exprValue{}, fmt.Errorf("division by zero")
			}
			left = makeFloat(lf / rf)
		case TOK_PERCENT:
			li := int64(lf)
			ri := int64(rf)
			if ri == 0 {
				return exprValue{}, fmt.Errorf("modulo by zero")
			}
			left = makeInt(li % ri)
		}
	}
	return left, nil
}

func parseUnary(prog *Program, tokens []Token, i *int, sigils sigilTable) (exprValue, error) {
	if *i >= len(tokens) {
		return exprValue{}, fmt.Errorf("unexpected end of expression")
	}

	tok := tokens[*i]

	if tok.Type == TOK_MINUS {
		*i++
		val, err := parseUnary(prog, tokens, i, sigils)
		if err != nil {
			return exprValue{}, err
		}
		lf, ok := val.asFloat()
		if !ok {
			return exprValue{}, fmt.Errorf("cannot negate non-numeric value")
		}
		return makeFloat(-lf), nil
	}

	if tok.Type == TOK_NOT {
		*i++
		val, err := parseUnary(prog, tokens, i, sigils)
		if err != nil {
			return exprValue{}, err
		}
		return makeBool(!val.asBool()), nil
	}

	return parsePrimary(prog, tokens, i, sigils)
}

func parsePrimary(prog *Program, tokens []Token, i *int, sigils sigilTable) (exprValue, error) {
	if *i >= len(tokens) {
		return exprValue{}, fmt.Errorf("unexpected end of expression")
	}

	tok := tokens[*i]

	switch tok.Type {
	case TOK_STRING:
		*i++
		return makeText(tok.Lexeme), nil

	case TOK_SIGIL:
		// consume '$'
		*i++
		if *i >= len(tokens) || tokens[*i].Type != TOK_IDENT {
			return exprValue{}, fmt.Errorf("expected SIGIL name after $ at %s:%d:%d",
				tok.File, tok.Line, tok.Column)
		}
		name := tokens[*i].Lexeme
		*i++

		val, ok := sigils[name]
		if !ok {
			return exprValue{}, fmt.Errorf("unknown SIGIL %s at %s:%d:%d",
				name, tok.File, tok.Line, tok.Column)
		}

		// auto-interpret the sigil value like IDENT does
		s := strings.TrimSpace(val)
		if strings.EqualFold(s, "true") {
			return makeBool(true), nil
		}
		if strings.EqualFold(s, "false") {
			return makeBool(false), nil
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return makeFloat(f), nil
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return makeInt(n), nil
		}

		return makeText(val), nil

	case TOK_NUM:
		*i++
		lex := strings.TrimSpace(tok.Lexeme)
		if strings.ContainsAny(lex, ".eE") {
			f, err := strconv.ParseFloat(lex, 64)
			if err != nil {
				return exprValue{}, fmt.Errorf("invalid float literal %q", lex)
			}
			return makeFloat(f), nil
		}
		n, err := strconv.ParseInt(lex, 10, 64)
		if err != nil {
			return exprValue{}, fmt.Errorf("invalid int literal %q", lex)
		}
		return makeInt(n), nil

	case TOK_IDENT:
		val, ok := sigils[tok.Lexeme]
		if !ok {
			return exprValue{}, fmt.Errorf("unknown SIGIL %s at %s:%d:%d",
				tok.Lexeme, tok.File, tok.Line, tok.Column)
		}
		*i++
		s := strings.TrimSpace(val)
		if strings.EqualFold(s, "true") {
			return makeBool(true), nil
		}
		if strings.EqualFold(s, "false") {
			return makeBool(false), nil
		}
		if strings.ContainsAny(s, ".eE") {
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				return makeFloat(f), nil
			}
		} else {
			if n, err := strconv.ParseInt(s, 10, 64); err == nil {
				return makeInt(n), nil
			}
		}
		return makeText(val), nil

	case TOK_LPAREN:
		*i++
		inner, err := parseOr(prog, tokens, i, sigils)
		if err != nil {
			return exprValue{}, err
		}
		if *i >= len(tokens) || tokens[*i].Type != TOK_RPAREN {
			return exprValue{}, fmt.Errorf("expected ')' in expression")
		}
		*i++
		return inner, nil

	case TOK_SUMMON:
		// Allow SUMMON as an expression: delegate to existing summoning logic.
		start := *i
		val, consumed, err := evalSummonExpr(prog, tokens, start, sigils)
		if err != nil {
			return exprValue{}, err
		}
		*i = start + consumed
		return makeText(val), nil
	}

	return exprValue{}, fmt.Errorf("unexpected %s in expression", tok.Type)
}

// Helper: interpret a SIGIL string as bool/int/float/text.
func classifySigilValue(val string) exprValue {
	s := strings.TrimSpace(val)
	if strings.EqualFold(s, "true") {
		return makeBool(true)
	}
	if strings.EqualFold(s, "false") {
		return makeBool(false)
	}
	if strings.ContainsAny(s, ".eE") {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return makeFloat(f)
		}
	} else {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return makeInt(n)
		}
	}
	return makeText(val)
}

// execWork runs a single WORK. If captureAnswer is true, it returns the
// first THUS WE ANSWER / SEND BACK value instead of printing it.
func execWork(prog *Program, w *WorkDecl, sigils sigilTable, captureAnswer bool) (string, error) {
	tokens := cleanWorkBody(w.Body)
	i := 0

	if w.Ephemeral {
		fmt.Printf("[SIC] Entering EPHEMERAL WORK %s.\n", w.Name)
	}

	// Track EPHEMERAL sigils created in this Work so we can scrub them
	// on *any* exit path (normal, OMEN path, etc.).
	ephemeral := make(map[string]bool)

	defer func() {
		for name := range ephemeral {
			delete(sigils, name)
		}
	}()

	for i < len(tokens) {
		tok := tokens[i]

		// Optional trace (you can uncomment while debugging)
		// fmt.Printf("[TRACE] token=%s (%s)\n", tok.Type, tok.Lexeme)

		// Skip NEWLINEs
		if tok.Type == TOK_NEWLINE {
			i++
			continue
		}

		switch tok.Type {

		case TOK_THUS:
			// THUS WE ANSWER WITH <expr>.
			msg, next, err := execThus(prog, tokens, i, sigils)
			if err != nil {
				return "", err
			}
			if captureAnswer {
				return msg, nil
			}
			fmt.Println(msg)
			_ = next
			return "", nil

		case TOK_SAY:
			// SAY: <expr>.
			next, err := execSay(prog, tokens, i, sigils)
			if err != nil {
				return "", err
			}
			i = next
			continue

		case TOK_LET:
			// LET SIGIL name BE <expr>.
			next, err := execLet(prog, tokens, i, sigils, ephemeral)
			if err != nil {
				return "", err
			}
			i = next
			continue

		case TOK_EPHEMERAL:
			// Two forms:
			//  1) EPHEMERAL SIGIL name BE <expr>.
			//  2) EPHEMERAL: ... END EPHEMERAL
			if i+1 < len(tokens) && tokens[i+1].Type == TOK_SIGIL {
				// EPHEMERAL SIGIL ...
				next, name, err := execEphemeralSigil(prog, tokens, i, sigils)
				if err != nil {
					return "", err
				}
				// Mark this sigil as ephemeral for scrubbing at Work exit.
				ephemeral[name] = true
				i = next
				continue
			}

			// Otherwise treat as an EPHEMERAL block.
			next, err := execEphemeralBlock(prog, tokens, i, sigils)
			if err != nil {
				return "", err
			}
			i = next
			continue

		case TOK_RAISE:
			// RAISE OMEN "name".
			next, err := execRaiseOmen(tokens, i, sigils)
			if err != nil {
				// omenError (or other) propagates up; OMEN blocks can catch it.
				return "", err
			}
			i = next
			continue

		case TOK_OMEN:
			// OMEN "name": ... FALLS_TO_RUIN: ... ENDOMEN.
			next, err := execOmenBlock(prog, tokens, i, sigils)
			if err != nil {
				return "", err
			}
			i = next
			continue

		case TOK_WEAVE:
			// WEAVE: ... ENDWEAVE.
			next, err := execWeaveBlock(prog, tokens, i, sigils)
			if err != nil {
				return "", err
			}
			i = next
			continue

		case TOK_CHOIR:
			next, err := execChoirBlock(prog, tokens, i, sigils)
			if err != nil {
				return "", err
			}
			i = next
			continue

		case TOK_WHILE:
			// WHILE condition ... ENDWHILE.
			next, err := execWhile(prog, tokens, i, sigils)
			if err != nil {
				return "", err
			}
			i = next
			continue

		case TOK_ALTAR:
			next, err := execAltarBlock(prog, tokens, i, sigils)
			if err != nil {
				return "", err
			}
			i = next
			continue

		case TOK_SUMMON:
			// Standalone SUMMON as a statement.
			next, err := execSummonStmt(prog, tokens, i, sigils)
			if err != nil {
				return "", err
			}
			i = next
			continue

		case TOK_IDENT:
			switch tok.Lexeme {
			case "SEND":
				// SEND BACK ...
				msg, next, err := execSendBack(prog, tokens, i, sigils)
				if err != nil {
					return "", err
				}
				if captureAnswer {
					return msg, nil
				}
				fmt.Println(msg)
				_ = next
				return "", nil

			case "FALLS_TO_RUIN":
				next, err := execFallsToRuin(prog, tokens, i, sigils)
				if err != nil {
					return "", err
				}
				i = next
				continue
			}
			// other idents fall through

		case TOK_IF:
			// IF OMEN ... IS PRESENT THEN:   (OMEN-aware IF)
			if i+1 < len(tokens) && tokens[i+1].Type == TOK_OMEN {
				next, err := execIfOmen(prog, tokens, i, sigils)
				if err != nil {
					return "", err
				}
				i = next
				continue
			}

			// Normal IF SIGIL ... EQUALS ...
			next, err := execIf(prog, tokens, i, sigils)
			if err != nil {
				return "", err
			}
			i = next
			continue

		case TOK_CHAMBER:
			// CHAMBER ... ENDCHAMBER.
			next, err := execChamberBlock(prog, tokens, i, sigils)
			if err != nil {
				return "", err
			}
			i = next
			continue

		case TOK_ENTANGLE:
			next, err := execEntangle(tokens, i)
			if err != nil {
				return "", err
			}
			i = next
			continue

		case TOK_RELEASE:
			next, err := execRelease(tokens, i)
			if err != nil {
				return "", err
			}
			i = next
			continue

		case TOK_ARCWORK:
			// ARCWORK: ... ENDARCWORK.
			next, err := execArcworkBlock(prog, tokens, i, sigils)
			if err != nil {
				return "", err
			}
			i = next
			continue
		}

		// Default: move on
		i++
	}

	// If we were summoned and expected to answer, but never did,
	// treat that as "empty answer" instead of an error.
	if captureAnswer {
		return "", nil
	}

	// Top-level or side-effect-only WORKs are allowed to finish
	// without an explicit THUS/SEND BACK.
	return "", nil
}

// ---------------- SAY ----------------

// SAY: <expr>.
func execSay(prog *Program, tokens []Token, i int, sigils sigilTable) (int, error) {
	// tokens[i] is TOK_SAY
	i++ // move past SAY

	// Expect COLON
	if i >= len(tokens) || tokens[i].Type != TOK_COLON {
		return i, fmt.Errorf("SAY: expected COLON after SAY at %s:%d:%d",
			tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	i++ // after COLON

	// Collect expression until we hit a '.' (TOK_DOT) or NEWLINE/ENDWORK
	exprStart := i
	for i < len(tokens) &&
		tokens[i].Type != TOK_DOT &&
		tokens[i].Type != TOK_NEWLINE &&
		tokens[i].Type != TOK_ENDWORK {
		i++
	}

	msg, err := evalStringExpr(prog, tokens[exprStart:i], sigils)
	if err != nil {
		return i, err
	}

	fmt.Println("[SIC SAY]", msg)

	// Skip the dot if present
	if i < len(tokens) && tokens[i].Type == TOK_DOT {
		i++
	}

	return i, nil
}

// ---------------- SCRIBE / LOG ----------------
//
// SCRIBE: <expr>.
// LOG: <expr>.
// (SCRIBE is the ritual name; LOG is a legacy alias.)
func execLog(prog *Program, tokens []Token, i int, sigils sigilTable) (int, error) {
	startTok := tokens[i] // TOK_LOG, lexeme "LOG" or "SCRIBE"
	i++                   // after LOG / SCRIBE

	// Expect COLON
	if i >= len(tokens) || tokens[i].Type != TOK_COLON {
		return i, fmt.Errorf("%s: expected COLON after %s at %s:%d:%d",
			startTok.Lexeme, startTok.Lexeme,
			startTok.File, startTok.Line, startTok.Column)
	}
	i++ // after COLON

	// Collect expression until DOT / NEWLINE / ENDWORK
	exprStart := i
	for i < len(tokens) &&
		tokens[i].Type != TOK_DOT &&
		tokens[i].Type != TOK_NEWLINE &&
		tokens[i].Type != TOK_ENDWORK {
		i++
	}

	msg, err := evalStringExpr(prog, tokens[exprStart:i], sigils)
	if err != nil {
		return i, err
	}

	// Ritual logging prefix; you can change this styling later.
	fmt.Println("[SIC SCRIBE]", msg)

	if i < len(tokens) && tokens[i].Type == TOK_DOT {
		i++
	}

	return i, nil
}

// ---------------- LET SIGIL ----------------

// LET [EPHEMERAL] SIGIL name BE <expr>.
func execLet(prog *Program, tokens []Token, i int, sigils sigilTable, ephemeral map[string]bool) (int, error) {
	// tokens[i] = TOK_LET
	i++

	// Optional EPHEMERAL
	isEphemeral := false
	if i < len(tokens) && tokens[i].Type == TOK_EPHEMERAL {
		isEphemeral = true
		i++
	}

	// Expect SIGIL
	if i >= len(tokens) || tokens[i].Type != TOK_SIGIL {
		return i, fmt.Errorf("LET: expected SIGIL after LET at %s:%d:%d",
			tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	i++

	// Expect IDENT (name)
	if i >= len(tokens) || tokens[i].Type != TOK_IDENT {
		return i, fmt.Errorf("LET: expected SIGIL name after SIGIL at %s:%d:%d",
			tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	name := tokens[i].Lexeme
	i++

	// Expect BE (either TOK_BE or IDENT "BE")
	if i >= len(tokens) ||
		!(tokens[i].Type == TOK_BE ||
			(tokens[i].Type == TOK_IDENT && tokens[i].Lexeme == "BE")) {
		return i, fmt.Errorf("LET: expected BE after SIGIL %s at %s:%d:%d",
			name, tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	i++ // after BE

	exprStart := i
	for i < len(tokens) &&
		tokens[i].Type != TOK_DOT &&
		tokens[i].Type != TOK_NEWLINE &&
		tokens[i].Type != TOK_ENDWORK {
		i++
	}

	val, err := evalStringExpr(prog, tokens[exprStart:i], sigils)
	if err != nil {
		return i, err
	}
	setSigil(sigils, name, val)

	if isEphemeral {
		ephemeral[name] = true
	}

	if i < len(tokens) && tokens[i].Type == TOK_DOT {
		i++
	}
	return i, nil
}

// EPHEMERAL SIGIL name BE <expr>.
//
// Binds a sigil exactly like LET SIGIL, but the caller of this function
// (execWork) is responsible for marking it as ephemeral for scrubbing.
func execEphemeralSigil(prog *Program, tokens []Token, i int, sigils sigilTable) (int, string, error) {
	startTok := tokens[i] // TOK_EPHEMERAL
	i++                   // after EPHEMERAL

	// Optional LET: "EPHEMERAL LET SIGIL ..." is tolerated.
	if i < len(tokens) && tokens[i].Type == TOK_LET {
		i++
	}

	// Expect SIGIL
	if i >= len(tokens) || tokens[i].Type != TOK_SIGIL {
		return i, "", fmt.Errorf("EPHEMERAL: expected SIGIL after EPHEMERAL at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}
	i++

	// Expect IDENT (name)
	if i >= len(tokens) || tokens[i].Type != TOK_IDENT {
		return i, "", fmt.Errorf("EPHEMERAL: expected SIGIL name after SIGIL at %s:%d:%d",
			tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	name := tokens[i].Lexeme
	i++

	// Expect BE (either TOK_BE or IDENT "BE")
	if i >= len(tokens) ||
		!(tokens[i].Type == TOK_BE ||
			(tokens[i].Type == TOK_IDENT && tokens[i].Lexeme == "BE")) {
		return i, "", fmt.Errorf("EPHEMERAL: expected BE after SIGIL %s at %s:%d:%d",
			name, tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	i++ // after BE

	// Expression until DOT / NEWLINE / ENDWORK
	exprStart := i
	for i < len(tokens) &&
		tokens[i].Type != TOK_DOT &&
		tokens[i].Type != TOK_NEWLINE &&
		tokens[i].Type != TOK_ENDWORK {
		i++
	}

	val, err := evalStringExpr(prog, tokens[exprStart:i], sigils)
	if err != nil {
		return i, "", err
	}
	setSigil(sigils, name, val)

	if i < len(tokens) && tokens[i].Type == TOK_DOT {
		i++
	}

	return i, name, nil
}

// ENTANGLE CORE calc_space WITH "STACK".
// ENTANGLE calc_space.
// For now, ENTANGLE is bookkeeping only: we track which core names
// are "entangled" inside the current CHAMBER.
func execEntangle(tokens []Token, i int) (int, error) {
	startTok := tokens[i] // TOK_ENTANGLE
	i++

	// Optional CORE keyword.
	if i < len(tokens) && tokens[i].Type == TOK_CORE {
		i++
	}

	if i >= len(tokens) || tokens[i].Type != TOK_IDENT {
		return i, fmt.Errorf("ENTANGLE: expected core name at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}
	name := tokens[i].Lexeme
	i++

	// Optional: WITH <mode> (ignored for now).
	if i < len(tokens) && tokens[i].Type == TOK_WITH {
		i++
		if i < len(tokens) && (tokens[i].Type == TOK_STRING || tokens[i].Type == TOK_IDENT) {
			i++ // storage mode, ignored for now
		}
	}

	// Optional trailing DOT.
	if i < len(tokens) && tokens[i].Type == TOK_DOT {
		i++
	}

	// Bookkeeping.
	if entangledCores[name] {
		return i, fmt.Errorf("ENTANGLE: core %s entangled twice in same CHAMBER at %s:%d:%d",
			name, startTok.File, startTok.Line, startTok.Column)
	}
	entangledCores[name] = true
	return i, nil
}

// RELEASE calc_space.
func execRelease(tokens []Token, i int) (int, error) {
	startTok := tokens[i] // TOK_RELEASE
	i++

	if i >= len(tokens) || tokens[i].Type != TOK_IDENT {
		return i, fmt.Errorf("RELEASE: expected core name at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}
	name := tokens[i].Lexeme
	i++

	// Optional trailing DOT.
	if i < len(tokens) && tokens[i].Type == TOK_DOT {
		i++
	}

	if !entangledCores[name] {
		return i, fmt.Errorf("RELEASE: core %s not entangled in this CHAMBER at %s:%d:%d",
			name, startTok.File, startTok.Line, startTok.Column)
	}
	delete(entangledCores, name)
	return i, nil
}

// ---------------- THUS WE ANSWER ----------------

// THUS WE ANSWER WITH <expr>.
// THUS WE ANSWER <expr>.   (WITH is optional)
func execThus(prog *Program, tokens []Token, i int, sigils sigilTable) (string, int, error) {
	// tokens[i] = TOK_THUS
	i++

	// Expect WE
	if i >= len(tokens) || tokens[i].Type != TOK_WE {
		return "", i, fmt.Errorf("THUS: expected WE after THUS at %s:%d:%d",
			tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	i++

	// Expect ANSWER
	if i >= len(tokens) || tokens[i].Type != TOK_ANSWER {
		return "", i, fmt.Errorf("THUS: expected ANSWER after WE at %s:%d:%d",
			tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	i++

	// Optional WITH:
	// - legacy form: IDENT "WITH"
	// - current form: TOK_WITH
	if i < len(tokens) &&
		((tokens[i].Type == TOK_IDENT && tokens[i].Lexeme == "WITH") ||
			tokens[i].Type == TOK_WITH) {
		i++
	}

	// Now everything up to DOT / NEWLINE / ENDWORK is the expression
	exprStart := i
	for i < len(tokens) &&
		tokens[i].Type != TOK_DOT &&
		tokens[i].Type != TOK_NEWLINE &&
		tokens[i].Type != TOK_ENDWORK {
		i++
	}

	val, err := evalStringExpr(prog, tokens[exprStart:i], sigils)
	if err != nil {
		return "", i, err
	}

	// Optional trailing dot
	if i < len(tokens) && tokens[i].Type == TOK_DOT {
		i++
	}

	return val, i, nil
}

// SEND BACK canticle
//
// Forms:
//
//	SEND BACK "literal".
//	SEND BACK left + right.
//	SEND BACK SIGIL answer.
//
// BACK is required in CHANT scrolls, but we match by lexeme so the
// token type (IDENT vs keyword) can't break us. We also tolerate an
// optional colon:  SEND BACK: "OK".
func execSendBack(prog *Program, tokens []Token, i int, sigils sigilTable) (string, int, error) {
	startTok := tokens[i] // "SEND"
	i++

	// Skip any NEWLINEs immediately after SEND
	for i < len(tokens) && tokens[i].Type == TOK_NEWLINE {
		i++
	}

	// Require the word BACK by lexeme (case-insensitive),
	// but don't care about token type.
	if i >= len(tokens) || !strings.EqualFold(tokens[i].Lexeme, "BACK") {
		return "", i, fmt.Errorf(
			"SEND BACK: expected BACK after SEND at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column,
		)
	}
	i++ // consume BACK

	// Optional colon, e.g. SEND BACK: "OK".
	if i < len(tokens) && tokens[i].Type == TOK_COLON {
		i++
	}

	// ----------------------------------------------------
	// Special-case: SEND BACK SIGIL name.
	// ----------------------------------------------------
	if i < len(tokens) && tokens[i].Type == TOK_SIGIL {
		i++ // SIGIL

		if i >= len(tokens) || tokens[i].Type != TOK_IDENT {
			return "", i, fmt.Errorf(
				"SEND BACK: expected SIGIL name after SIGIL at %s:%d:%d",
				tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column,
			)
		}
		name := tokens[i].Lexeme
		val, _ := getSigil(sigils, name)
		i++

		// Skip to end of statement (DOT / NEWLINE / ENDWORK)
		for i < len(tokens) &&
			tokens[i].Type != TOK_DOT &&
			tokens[i].Type != TOK_NEWLINE &&
			tokens[i].Type != TOK_ENDWORK {
			i++
		}
		if i < len(tokens) && tokens[i].Type == TOK_DOT {
			i++
		}
		return val, i, nil
	}

	// ----------------------------------------------------
	// General case: SEND BACK <expr>.
	// Expression runs until DOT / NEWLINE / ENDWORK.
	// ----------------------------------------------------
	exprStart := i
	for i < len(tokens) &&
		tokens[i].Type != TOK_DOT &&
		tokens[i].Type != TOK_NEWLINE &&
		tokens[i].Type != TOK_ENDWORK {
		i++
	}
	exprEnd := i

	// Slice out just the expression tokens for the engine
	exprTokens := make([]Token, exprEnd-exprStart)
	copy(exprTokens, tokens[exprStart:exprEnd])

	val, err := evalStringExpr(prog, exprTokens, sigils)
	if err != nil {
		return "", i, err
	}

	// Optional trailing DOT
	if i < len(tokens) && tokens[i].Type == TOK_DOT {
		i++
	}

	return val, i, nil
}

// ---------------- IF / ELSE / END ----------------

// Classic SIGIL-based IF:
//
// IF SIGIL name EQUALS "World" THEN:
//
//	SAY: "hi".
//
// ELSE:
//
//	SAY: "nope".
//
// END.
func execIf(prog *Program, tokens []Token, i int, sigils sigilTable) (int, error) {
	startTok := tokens[i]
	i++ // after IF

	// Optional NEWLINEs
	for i < len(tokens) && tokens[i].Type == TOK_NEWLINE {
		i++
	}

	// Expect SIGIL
	if i >= len(tokens) || tokens[i].Type != TOK_SIGIL {
		return i, fmt.Errorf("IF: expected SIGIL after IF at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}
	i++

	// SIGIL name
	if i >= len(tokens) || tokens[i].Type != TOK_IDENT {
		return i, fmt.Errorf("IF: expected SIGIL name after SIGIL at %s:%d:%d",
			tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	sigName := tokens[i].Lexeme
	i++

	// comparator: either IDENT "EQUALS" or TOK_EQUAL
	if i >= len(tokens) {
		return i, fmt.Errorf("IF: missing comparator for SIGIL %s", sigName)
	}
	compTok := tokens[i]
	if compTok.Type == TOK_IDENT && compTok.Lexeme != "EQUALS" {
		return i, fmt.Errorf("IF: expected EQUALS after SIGIL %s at %s:%d:%d",
			sigName, compTok.File, compTok.Line, compTok.Column)
	}
	if compTok.Type != TOK_IDENT && compTok.Type != TOK_EQUAL {
		return i, fmt.Errorf("IF: expected comparator after SIGIL %s at %s:%d:%d",
			sigName, compTok.File, compTok.Line, compTok.Column)
	}
	i++

	// Right-hand side: STRING or SIGIL <name> or bare ident (sigil)
	if i >= len(tokens) {
		return i, fmt.Errorf("IF: missing right-hand side after comparator for SIGIL %s", sigName)
	}

	var rhs string
	switch tokens[i].Type {
	case TOK_STRING:
		rhs = tokens[i].Lexeme
		i++
	case TOK_SIGIL:
		i++
		if i >= len(tokens) || tokens[i].Type != TOK_IDENT {
			return i, fmt.Errorf("IF: expected SIGIL name after SIGIL at %s:%d:%d",
				tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
		}
		rhsName := tokens[i].Lexeme
		rhs, _ = getSigil(sigils, rhsName)
		i++
	case TOK_IDENT:
		// treat as sigil name
		rhs, _ = getSigil(sigils, tokens[i].Lexeme)
		i++
	default:
		return i, fmt.Errorf("IF: unsupported RHS token %s at %s:%d:%d",
			tokens[i].Type, tokens[i].File, tokens[i].Line, tokens[i].Column)
	}

	// Optional IDENT "THEN"
	if i < len(tokens) &&
		tokens[i].Type == TOK_IDENT &&
		tokens[i].Lexeme == "THEN" {
		i++
	}

	// Expect COLON
	if i >= len(tokens) || tokens[i].Type != TOK_COLON {
		return i, fmt.Errorf("IF: expected COLON after condition at %s:%d:%d",
			tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	i++ // after COLON

	// Skip NEWLINEs
	for i < len(tokens) && tokens[i].Type == TOK_NEWLINE {
		i++
	}

	// Find ELSE / END boundaries
	thenStart := i
	elseStart := -1
	endPos := -1
	depth := 1 // nested IFs future-proof

	for j := i; j < len(tokens); j++ {
		t := tokens[j]
		if t.Type == TOK_IF {
			depth++
		} else if t.Type == TOK_ELSE && depth == 1 {
			elseStart = j
		} else if t.Type == TOK_END && depth == 1 {
			endPos = j
			break
		} else if t.Type == TOK_END && depth > 1 {
			depth--
		}
	}

	if endPos == -1 {
		return i, fmt.Errorf("IF: unmatched END for IF at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}

	// Evaluate condition (simple equality)
	lhs, _ := getSigil(sigils, sigName)
	cond := (lhs == rhs)

	if cond {
		// Execute THEN block
		thenEnd := endPos
		if elseStart != -1 {
			thenEnd = elseStart
		}
		if err := execBlock(prog, tokens[thenStart:thenEnd], sigils); err != nil {
			return endPos + 1, err
		}
	} else if elseStart != -1 {
		// Execute ELSE block
		k := elseStart + 1
		// Optional COLON
		if k < endPos && tokens[k].Type == TOK_COLON {
			k++
		}
		// Skip NEWLINEs
		for k < endPos && tokens[k].Type == TOK_NEWLINE {
			k++
		}
		if err := execBlock(prog, tokens[k:endPos], sigils); err != nil {
			return endPos + 1, err
		}
	}

	// Resume AFTER END.
	return endPos + 1, nil
}

// OMEN-based IF:
//
// IF OMEN "network_failure" IS PRESENT THEN:
//
//	...
//
// END.
func execIfOmen(prog *Program, tokens []Token, i int, sigils sigilTable) (int, error) {
	startTok := tokens[i]
	i++ // after IF

	// Expect OMEN
	if i >= len(tokens) || tokens[i].Type != TOK_OMEN {
		return i, fmt.Errorf("IF OMEN: expected OMEN after IF at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}
	i++

	// OMEN name: STRING or IDENT
	if i >= len(tokens) ||
		(tokens[i].Type != TOK_STRING && tokens[i].Type != TOK_IDENT) {
		return i, fmt.Errorf("IF OMEN: expected OMEN name at %s:%d:%d",
			tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	omenName := tokens[i].Lexeme
	i++

	// Optional "IS PRESENT"
	if i+1 < len(tokens) &&
		tokens[i].Type == TOK_IDENT && tokens[i].Lexeme == "IS" &&
		tokens[i+1].Type == TOK_IDENT && tokens[i+1].Lexeme == "PRESENT" {
		i += 2
	}

	// Optional THEN
	if i < len(tokens) &&
		tokens[i].Type == TOK_IDENT &&
		tokens[i].Lexeme == "THEN" {
		i++
	}

	// Expect COLON
	if i >= len(tokens) || tokens[i].Type != TOK_COLON {
		return i, fmt.Errorf("IF OMEN: expected COLON after condition at %s:%d:%d",
			tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	i++ // after COLON

	// Skip NEWLINEs
	for i < len(tokens) && tokens[i].Type == TOK_NEWLINE {
		i++
	}

	// Find ELSE / END boundaries
	thenStart := i
	elseStart := -1
	endPos := -1
	depth := 1

	for j := i; j < len(tokens); j++ {
		t := tokens[j]
		if t.Type == TOK_IF {
			depth++
		} else if t.Type == TOK_ELSE && depth == 1 {
			elseStart = j
		} else if t.Type == TOK_END && depth == 1 {
			endPos = j
			break
		} else if t.Type == TOK_END && depth > 1 {
			depth--
		}
	}

	if endPos == -1 {
		return i, fmt.Errorf("IF OMEN: unmatched END for IF at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}

	cond := omenPresent(sigils, omenName)

	if cond {
		thenEnd := endPos
		if elseStart != -1 {
			thenEnd = elseStart
		}
		if err := execBlock(prog, tokens[thenStart:thenEnd], sigils); err != nil {
			return endPos + 1, err
		}
	} else if elseStart != -1 {
		k := elseStart + 1
		if k < endPos && tokens[k].Type == TOK_COLON {
			k++
		}
		for k < endPos && tokens[k].Type == TOK_NEWLINE {
			k++
		}
		if err := execBlock(prog, tokens[k:endPos], sigils); err != nil {
			return endPos + 1, err
		}
	}

	return endPos + 1, nil
}

// EPHEMERAL SIGIL name BE <expr>.
func execEphemeral(prog *Program, tokens []Token, i int, sigils sigilTable) (int, string, error) {
	// tokens[i] = TOK_EPHEMERAL
	startTok := tokens[i]
	i++

	// Expect SIGIL
	if i >= len(tokens) || tokens[i].Type != TOK_SIGIL {
		return i, "", fmt.Errorf("EPHEMERAL: expected SIGIL after EPHEMERAL at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}
	i++

	// Expect IDENT (name)
	if i >= len(tokens) || tokens[i].Type != TOK_IDENT {
		return i, "", fmt.Errorf("EPHEMERAL: expected SIGIL name after SIGIL at %s:%d:%d",
			tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	name := tokens[i].Lexeme
	i++

	// Expect BE
	if i >= len(tokens) || tokens[i].Type != TOK_BE {
		return i, "", fmt.Errorf("EPHEMERAL: expected BE after SIGIL %s at %s:%d:%d",
			name, tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	i++ // after BE

	// Collect expression until DOT / NEWLINE / ENDWORK
	exprStart := i
	for i < len(tokens) &&
		tokens[i].Type != TOK_DOT &&
		tokens[i].Type != TOK_NEWLINE &&
		tokens[i].Type != TOK_ENDWORK {
		i++
	}

	val, err := evalStringExpr(prog, tokens[exprStart:i], sigils)
	if err != nil {
		return i, "", err
	}

	setSigil(sigils, name, val)

	// Optional trailing DOT
	if i < len(tokens) && tokens[i].Type == TOK_DOT {
		i++
	}

	return i, name, nil
}

// ---------------- WHILE ----------------
//
// WHILE SIGIL count EQUALS "0":
//
//	SAY: "Loop turn " + count + ".".
//	ARCWORK:
//	    RAISE SIGIL count BY 1.
//	ENDARCWORK
//
// ENDWHILE.
func execWhile(prog *Program, tokens []Token, i int, sigils sigilTable) (int, error) {
	startTok := tokens[i] // TOK_WHILE
	i++                   // after WHILE

	// Skip NEWLINEs
	for i < len(tokens) && tokens[i].Type == TOK_NEWLINE {
		i++
	}

	// Expect SIGIL
	if i >= len(tokens) || tokens[i].Type != TOK_SIGIL {
		return i, fmt.Errorf("WHILE: expected SIGIL after WHILE at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}
	i++

	// SIGIL name
	if i >= len(tokens) || tokens[i].Type != TOK_IDENT {
		return i, fmt.Errorf("WHILE: expected SIGIL name after SIGIL at %s:%d:%d",
			tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	sigName := tokens[i].Lexeme
	i++

	// comparator: IDENT "EQUALS" or '='
	if i >= len(tokens) {
		return i, fmt.Errorf("WHILE: missing comparator for SIGIL %s", sigName)
	}
	compTok := tokens[i]
	if compTok.Type == TOK_IDENT && compTok.Lexeme != "EQUALS" {
		return i, fmt.Errorf("WHILE: expected EQUALS after SIGIL %s at %s:%d:%d",
			sigName, compTok.File, compTok.Line, compTok.Column)
	}
	if compTok.Type != TOK_IDENT && compTok.Type != TOK_EQUAL {
		return i, fmt.Errorf("WHILE: expected comparator after SIGIL %s at %s:%d:%d",
			sigName, compTok.File, compTok.Line, compTok.Column)
	}
	i++

	// Right–hand side: everything up to COLON is our condition expression.
	condExprStart := i
	for i < len(tokens) && tokens[i].Type != TOK_COLON {
		i++
	}
	if i >= len(tokens) || tokens[i].Type != TOK_COLON {
		return i, fmt.Errorf("WHILE: expected COLON after condition at %s:%d:%d",
			tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	condExprEnd := i
	condTokens := tokens[condExprStart:condExprEnd]
	i++ // after COLON

	// Skip NEWLINEs before body
	for i < len(tokens) && tokens[i].Type == TOK_NEWLINE {
		i++
	}

	// Find matching ENDWHILE, respecting nesting.
	bodyStart := i
	endPos := -1
	depth := 1

	for j := i; j < len(tokens); j++ {
		t := tokens[j]

		// Nested WHILE → increase depth
		if t.Type == TOK_WHILE {
			depth++
			continue
		}

		// ENDWHILE can be a dedicated token or just IDENT "ENDWHILE"
		if t.Type == TOK_ENDWHILE ||
			(t.Type == TOK_IDENT && t.Lexeme == "ENDWHILE") {
			depth--
			if depth == 0 {
				endPos = j
				break
			}
			continue
		}
	}

	if endPos == -1 {
		return i, fmt.Errorf("WHILE: unmatched ENDWHILE for WHILE at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}

	// Main loop with a safety cap
	const maxWhileIterations = 100000
	iterations := 0

	for {
		if iterations >= maxWhileIterations {
			return endPos + 1, fmt.Errorf("WHILE: exceeded %d iterations; possible infinite loop", maxWhileIterations)
		}
		iterations++

		// Evaluate condition: SIGIL value == RHS expression
		lhs, _ := getSigil(sigils, sigName)
		rhs, err := evalStringExpr(prog, condTokens, sigils)
		if err != nil {
			return endPos + 1, err
		}

		if lhs != rhs {
			break
		}

		// Run body as a mini-Work
		if err := execBlock(prog, tokens[bodyStart:endPos], sigils); err != nil {
			return endPos + 1, err
		}
	}

	// Resume just after ENDWHILE (and optional trailing '.')
	k := endPos + 1
	if k < len(tokens) && tokens[k].Type == TOK_DOT {
		k++
	}
	return k, nil
}

// ---------------- CHAMBER v0.1 ----------------
//
// CHAMBER my_scope:
//     LET SIGIL gold BE "999".
//     SAY: "Inside: " + gold + ".".
// ENDCHAMBER.
//
// Semantics v0.1:
// - CHAMBER creates a *scoped* execution environment.
// - We clone the parent's sigils into a child table.
// - We execute the body using execWork on a synthetic WorkDecl.
// - Any changes made inside the CHAMBER (even non-EPHEMERAL) are discarded
//   when we return; the parent sigils are untouched.

// CHAMBER name:
//
//	...
//
// ENDCHAMBER.
//
// For now, CHAMBER:
//   - clones the current sigils into a child scope
//   - executes its body
//   - discards any sigil changes on exit
//   - enforces ENTANGLE/RELEASE correctness within its body
func execChamberBlock(prog *Program, tokens []Token, i int, sigils sigilTable) (int, error) {
	startTok := tokens[i] // TOK_CHAMBER
	i++

	// Optional CHAMBER name.
	if i < len(tokens) && tokens[i].Type == TOK_IDENT {
		// chamberName := tokens[i].Lexeme // currently unused
		i++
	}

	// Expect COLON.
	if i >= len(tokens) || tokens[i].Type != TOK_COLON {
		return i, fmt.Errorf("CHAMBER: expected COLON after header at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}
	i++ // move past COLON

	bodyStart := i

	// Find matching ENDCHAMBER, respecting nesting.
	depth := 1
	endPos := -1
	for j := i; j < len(tokens); j++ {
		t := tokens[j]
		switch t.Type {
		case TOK_CHAMBER:
			depth++
		case TOK_ENDCHAMBER:
			depth--
			if depth == 0 {
				endPos = j
				goto foundEnd
			}
		}
	}
foundEnd:

	if endPos == -1 {
		return i, fmt.Errorf("CHAMBER: unmatched ENDCHAMBER for CHAMBER at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}

	// New sigil scope (does not leak back out of the chamber).
	childSigils := cloneSigils(sigils)

	// Save/restore entanglement frame for this CHAMBER.
	oldEntangled := entangledCores
	entangledCores = map[string]bool{}

	// Execute the chamber body.
	if err := execBlock(prog, tokens[bodyStart:endPos], childSigils); err != nil {
		entangledCores = oldEntangled
		return endPos + 1, err
	}

	// Check for entangle leaks.
	if len(entangledCores) != 0 {
		entangledCores = oldEntangled
		return endPos + 1, fmt.Errorf(
			"EPHEMERAL: entangle leak in CHAMBER at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column,
		)
	}

	// Restore outer entanglement frame.
	entangledCores = oldEntangled

	// Move index to just after ENDCHAMBER (and optional trailing DOT).
	i = endPos + 1
	if i < len(tokens) && tokens[i].Type == TOK_DOT {
		i++
	}
	return i, nil
}

// execBlock executes a slice of tokens as if it were a mini-Work.
func execBlock(prog *Program, tokens []Token, sigils sigilTable) error {
	w := &WorkDecl{
		Name: "BLOCK",
		Body: tokens,
	}
	_, err := execWork(prog, w, sigils, false)
	return err
}

// execBlockWithOmen executes a slice of tokens like a mini-Work,
// but returns (possibly nil) *omenError separately from other errors.
func execBlockWithOmen(prog *Program, tokens []Token, sigils sigilTable) (*omenError, error) {
	w := &WorkDecl{
		Name: "BLOCK",
		Body: tokens,
	}
	_, err := execWork(prog, w, sigils, true)
	if err == nil {
		return nil, nil
	}
	if oe, ok := err.(*omenError); ok {
		return oe, nil
	}
	return nil, err
}

// ---------------- OMEN statements ----------------

// RAISE OMEN "network_failure".
func execRaiseOmen(tokens []Token, i int, sigils sigilTable) (int, error) {
	startTok := tokens[i] // RAISE
	i++

	// Expect OMEN
	if i >= len(tokens) || tokens[i].Type != TOK_OMEN {
		return i, fmt.Errorf("RAISE: expected OMEN after RAISE at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}
	i++

	// Expect STRING omen name
	if i >= len(tokens) || tokens[i].Type != TOK_STRING {
		return i, fmt.Errorf("RAISE: expected OMEN name string after OMEN at %s:%d:%d",
			tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	omenName := tokens[i].Lexeme
	i++

	// Skip until DOT / NEWLINE / ENDWORK
	for i < len(tokens) &&
		tokens[i].Type != TOK_DOT &&
		tokens[i].Type != TOK_NEWLINE &&
		tokens[i].Type != TOK_ENDWORK {
		i++
	}
	if i < len(tokens) && tokens[i].Type == TOK_DOT {
		i++
	}

	// Mark the OMEN as present (no longer a fatal runtime error here).
	raiseOmen(sigils, omenName)

	return i, nil
}

// FALLS_TO_RUIN: <expr>.
// Logs the recovery message and clears all OMENs.
func execFallsToRuin(prog *Program, tokens []Token, i int, sigils sigilTable) (int, error) {
	startTok := tokens[i]
	i++ // after FALLS_TO_RUIN

	// Expect COLON
	if i >= len(tokens) || tokens[i].Type != TOK_COLON {
		return i, fmt.Errorf("FALLS_TO_RUIN: expected COLON after FALLS_TO_RUIN at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}
	i++ // after COLON

	exprStart := i
	for i < len(tokens) &&
		tokens[i].Type != TOK_DOT &&
		tokens[i].Type != TOK_NEWLINE &&
		tokens[i].Type != TOK_ENDWORK {
		i++
	}

	msg, err := evalStringExpr(prog, tokens[exprStart:i], sigils)
	if err != nil {
		return i, err
	}

	fmt.Println("[SIC RUIN]", msg)
	clearAllOmens(sigils)

	if i < len(tokens) && tokens[i].Type == TOK_DOT {
		i++
	}
	return i, nil
}

// ---------------- OMEN / FALLS_TO_RUIN ----------------
//
// OMEN "network_failure":
//
//	... risky operations ...
//
// FALLS_TO_RUIN:
//
//	... recovery / logging ...
//
// ENDOMEN.
func execOmenBlock(prog *Program, tokens []Token, i int, sigils sigilTable) (int, error) {
	startTok := tokens[i] // TOK_OMEN
	i++

	// Expect STRING omen name
	if i >= len(tokens) || tokens[i].Type != TOK_STRING {
		return i, fmt.Errorf("OMEN: expected OMEN name string after OMEN at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}
	omenName := tokens[i].Lexeme
	i++

	// Optional COLON
	if i < len(tokens) && tokens[i].Type == TOK_COLON {
		i++
	}

	// Body starts here
	bodyStart := i

	// Find FALLS_TO_RUIN (if any) and ENDOMEN, respecting nesting.
	ruinStart := -1
	endPos := -1
	depth := 1

	// Track previous token type so we only treat OMEN at a statement boundary
	// as a nested block, not the OMEN in "RAISE OMEN" or "IF OMEN".
	prevType := TOK_NEWLINE

	for j := i; j < len(tokens); j++ {
		t := tokens[j]

		// Nested OMEN blocks: only when OMEN appears at a statement boundary.
		if t.Type == TOK_OMEN &&
			(prevType == TOK_NEWLINE ||
				prevType == TOK_COLON ||
				prevType == TOK_END ||
				prevType == TOK_ENDWEAVE ||
				prevType == TOK_ENDWORK ||
				prevType == TOK_ENDOMEN ||
				prevType == TOK_ENDCHAMBER ||
				prevType == TOK_ENDALTAR) {
			depth++
			prevType = t.Type
			continue
		}

		// ENDOMEN closes one level of OMEN
		if t.Type == TOK_ENDOMEN {
			depth--
			if depth == 0 {
				endPos = j
				break
			}
			prevType = t.Type
			continue
		}

		// Only notice FALLS_TO_RUIN at the top level of *this* OMEN
		if depth == 1 && t.Type == TOK_IDENT && t.Lexeme == "FALLS_TO_RUIN" {
			ruinStart = j
		}

		prevType = t.Type
	}

	if endPos == -1 {
		return i, fmt.Errorf("OMEN: unmatched ENDOMEN for OMEN at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}

	tryEnd := endPos
	if ruinStart != -1 {
		tryEnd = ruinStart
	}

	// Snapshot sigils BEFORE the OMEN body for rollback.
	snapshot := cloneSigils(sigils)

	// Run the protected body, catching omenError if raised.
	raised, err := execBlockWithOmen(prog, tokens[bodyStart:tryEnd], sigils)
	if err != nil {
		return endPos + 1, err
	}

	// If no OMEN was raised, skip any FALLS_TO_RUIN block.
	if raised == nil {
		return endPos + 1, nil
	}

	// If a different OMEN was raised, bubble it up.
	if raised.name != omenName {
		return endPos + 1, raised
	}

	// Matching OMEN: rollback sigils to snapshot.
	for k := range sigils {
		delete(sigils, k)
	}
	for k, v := range snapshot {
		sigils[k] = v
	}

	// If there is no FALLS_TO_RUIN block, we just swallow the OMEN.
	if ruinStart == -1 {
		return endPos + 1, nil
	}

	// RUIN block starts after "FALLS_TO_RUIN" and optional COLON/NEWLINEs.
	k := ruinStart + 1
	if k < endPos && tokens[k].Type == TOK_COLON {
		k++
	}
	for k < endPos && tokens[k].Type == TOK_NEWLINE {
		k++
	}

	// Execute the FALLS_TO_RUIN block (errors here are normal runtime errors).
	if err := execBlock(prog, tokens[k:endPos], sigils); err != nil {
		return endPos + 1, err
	}

	return endPos + 1, nil
}

// ---------------- EPHEMERAL block ----------------
//
// EPHEMERAL:
//
//	LET SIGIL SECRET BE "doom".
//	SAY: "Inside ONE_SHOT, SECRET is " + SECRET + ".".
//
// END EPHEMERAL.
func execEphemeralBlock(prog *Program, tokens []Token, i int, sigils sigilTable) (int, error) {
	startTok := tokens[i] // TOK_EPHEMERAL
	i++                   // after EPHEMERAL

	// Optional COLON
	if i < len(tokens) && tokens[i].Type == TOK_COLON {
		i++
	}

	// Body starts here
	bodyStart := i

	endPos := -1
	depth := 1

	for j := i; j < len(tokens); j++ {
		t := tokens[j]

		// Nested EPHEMERAL blocks (rare, but we handle them)
		if t.Type == TOK_EPHEMERAL {
			depth++
			continue
		}

		// Look for "END EPHEMERAL"
		if t.Type == TOK_END {
			if j+1 < len(tokens) && tokens[j+1].Type == TOK_EPHEMERAL {
				depth--
				if depth == 0 {
					endPos = j
					break
				}
				continue
			}
		}
	}

	if endPos == -1 {
		return i, fmt.Errorf("EPHEMERAL: unmatched END EPHEMERAL for block at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}

	// Execute the EPHEMERAL body.
	if err := execBlock(prog, tokens[bodyStart:endPos], sigils); err != nil {
		return endPos + 1, err
	}

	// Skip past "END EPHEMERAL" and optional trailing DOT.
	k := endPos + 2 // skip END + EPHEMERAL
	if k < len(tokens) && tokens[k].Type == TOK_DOT {
		k++
	}
	return k, nil
}

// ---------------- ARCWORK v0.1 ----------------
//
// ARCWORK:
//
//	RAISE SIGIL count BY 1.
//	LOWER SIGIL health BY 5.
//
// ENDARCWORK.
func execArcworkBlock(prog *Program, tokens []Token, i int, sigils sigilTable) (int, error) {
	startTok := tokens[i]
	i++ // after ARCWORK

	// Expect COLON
	if i >= len(tokens) || tokens[i].Type != TOK_COLON {
		return i, fmt.Errorf("ARCWORK: expected COLON after ARCWORK at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}
	i++ // after COLON

	for i < len(tokens) {
		tok := tokens[i]

		if tok.Type == TOK_NEWLINE {
			i++
			continue
		}

		// ENDARCWORK.
		if tok.Type == TOK_IDENT && tok.Lexeme == "ENDARCWORK" {
			i++
			if i < len(tokens) && tokens[i].Type == TOK_DOT {
				i++
			}
			return i, nil
		}

		// RAISE SIGIL ...
		if tok.Type == TOK_RAISE {
			next, err := execArcRaise(tokens, i, sigils)
			if err != nil {
				return next, err
			}
			i = next
			continue
		}

		// LOWER SIGIL ...
		if tok.Type == TOK_IDENT && tok.Lexeme == "LOWER" {
			next, err := execArcLower(tokens, i, sigils)
			if err != nil {
				return next, err
			}
			i = next
			continue
		}

		return i, fmt.Errorf("ARCWORK: unexpected token %s at %s:%d:%d",
			tok.Type, tok.File, tok.Line, tok.Column)
	}

	return i, fmt.Errorf("ARCWORK: missing ENDARCWORK for block starting at %s:%d:%d",
		startTok.File, startTok.Line, startTok.Column)
}

func readArcOperand(tokens []Token, i int, sigils sigilTable) (int64, int, error) {
	if i >= len(tokens) {
		return 0, i, fmt.Errorf("ARCWORK: missing operand")
	}
	tok := tokens[i]

	switch tok.Type {
	case TOK_NUM:
		v, err := strconv.ParseInt(tok.Lexeme, 10, 64)
		if err != nil {
			return 0, i + 1, fmt.Errorf("ARCWORK: invalid number %q", tok.Lexeme)
		}
		return v, i + 1, nil

	case TOK_SIGIL:
		i++
		if i >= len(tokens) || tokens[i].Type != TOK_IDENT {
			return 0, i, fmt.Errorf("ARCWORK: expected SIGIL name after SIGIL at %s:%d:%d",
				tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
		}
		name := tokens[i].Lexeme
		v, err := getSigilInt(sigils, name)
		return v, i + 1, err

	case TOK_IDENT:
		// bare SIGIL name
		name := tok.Lexeme
		v, err := getSigilInt(sigils, name)
		return v, i + 1, err

	default:
		return 0, i + 1, fmt.Errorf("ARCWORK: unexpected operand token %s at %s:%d:%d",
			tok.Type, tok.File, tok.Line, tok.Column)
	}
}

func execArcRaise(tokens []Token, i int, sigils sigilTable) (int, error) {
	startTok := tokens[i]
	i++ // after RAISE

	// Expect SIGIL
	if i >= len(tokens) || tokens[i].Type != TOK_SIGIL {
		return i, fmt.Errorf("ARCWORK RAISE: expected SIGIL after RAISE at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}
	i++

	// SIGIL name
	if i >= len(tokens) || tokens[i].Type != TOK_IDENT {
		return i, fmt.Errorf("ARCWORK RAISE: expected SIGIL name after SIGIL at %s:%d:%d",
			tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	name := tokens[i].Lexeme
	i++

	// BY
	if i >= len(tokens) || tokens[i].Type != TOK_IDENT || tokens[i].Lexeme != "BY" {
		return i, fmt.Errorf("ARCWORK RAISE: expected BY after SIGIL %s at %s:%d:%d",
			name, tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	i++

	delta, next, err := readArcOperand(tokens, i, sigils)
	if err != nil {
		return next, err
	}
	i = next

	cur, err := getSigilInt(sigils, name)
	if err != nil {
		return i, err
	}
	setSigilInt(sigils, name, cur+delta)

	// consume until DOT / NEWLINE / ENDWORK
	for i < len(tokens) &&
		tokens[i].Type != TOK_DOT &&
		tokens[i].Type != TOK_NEWLINE &&
		tokens[i].Type != TOK_ENDWORK {
		i++
	}
	if i < len(tokens) && tokens[i].Type == TOK_DOT {
		i++
	}
	return i, nil
}

func execArcLower(tokens []Token, i int, sigils sigilTable) (int, error) {
	startTok := tokens[i]
	i++ // after LOWER

	// Expect SIGIL
	if i >= len(tokens) || tokens[i].Type != TOK_SIGIL {
		return i, fmt.Errorf("ARCWORK LOWER: expected SIGIL after LOWER at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}
	i++

	// SIGIL name
	if i >= len(tokens) || tokens[i].Type != TOK_IDENT {
		return i, fmt.Errorf("ARCWORK LOWER: expected SIGIL name after SIGIL at %s:%d:%d",
			tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	name := tokens[i].Lexeme
	i++

	// BY
	if i >= len(tokens) || tokens[i].Type != TOK_IDENT || tokens[i].Lexeme != "BY" {
		return i, fmt.Errorf("ARCWORK LOWER: expected BY after SIGIL %s at %s:%d:%d",
			name, tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	i++

	delta, next, err := readArcOperand(tokens, i, sigils)
	if err != nil {
		return next, err
	}
	i = next

	cur, err := getSigilInt(sigils, name)
	if err != nil {
		return i, err
	}
	setSigilInt(sigils, name, cur-delta)

	// consume until DOT / NEWLINE / ENDWORK
	for i < len(tokens) &&
		tokens[i].Type != TOK_DOT &&
		tokens[i].Type != TOK_NEWLINE &&
		tokens[i].Type != TOK_ENDWORK {
		i++
	}
	if i < len(tokens) && tokens[i].Type == TOK_DOT {
		i++
	}
	return i, nil
}

// ---------------- WEAVE ----------------
//
// WEAVE:
//
//	SUMMON WORK Alpha WITH SIGIL "one".
//	SUMMON WORK Beta  WITH SIGIL "two".
//
// ENDWEAVE.
func execWeaveBlock(prog *Program, tokens []Token, i int, sigils sigilTable) (int, error) {
	startTok := tokens[i]
	i++ // after WEAVE

	// Expect COLON
	if i >= len(tokens) || tokens[i].Type != TOK_COLON {
		return i, fmt.Errorf("WEAVE: expected COLON after WEAVE at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}
	i++ // after COLON

	for i < len(tokens) {
		tok := tokens[i]

		if tok.Type == TOK_NEWLINE {
			i++
			continue
		}

		if tok.Type == TOK_ENDWEAVE {
			i++
			if i < len(tokens) && tokens[i].Type == TOK_DOT {
				i++
			}
			return i, nil
		}

		if tok.Type == TOK_SUMMON {
			next, err := execSummonStmt(prog, tokens, i, sigils)
			if err != nil {
				return next, err
			}
			i = next
			continue
		}

		return i, fmt.Errorf("WEAVE: unexpected token %s at %s:%d:%d",
			tok.Type, tok.File, tok.Line, tok.Column)
	}

	return i, fmt.Errorf("WEAVE: missing ENDWEAVE for block starting at %s:%d:%d",
		startTok.File, startTok.Line, startTok.Column)
}

// ---------------- CHOIR (structured parallel SUMMONs) ----------------
//
// CHOIR:
//   SUMMON WORK A WITH SIGIL "one".
//   SUMMON WORK B WITH SIGIL "two".
// ENDCHOIR.
//
// Semantics v0.1:
// - Each SUMMON runs in its own goroutine.
// - Parent waits for all to complete before continuing.
// - Child sigils are isolated via SUMMON semantics.
// - Output ordering *between* tasks is not guaranteed.

func execChoirBlock(prog *Program, tokens []Token, i int, sigils sigilTable) (int, error) {
	startTok := tokens[i] // TOK_CHOIR
	i++                   // after CHOIR

	// Optional colon: "CHOIR:" vs "CHOIR"
	if i < len(tokens) && tokens[i].Type == TOK_COLON {
		i++
	}

	// Skip leading newlines
	for i < len(tokens) && tokens[i].Type == TOK_NEWLINE {
		i++
	}

	// Find ENDCHOIR
	endPos := -1
	for j := i; j < len(tokens); j++ {
		t := tokens[j]
		if t.Type == TOK_ENDCHOIR || (t.Type == TOK_IDENT && t.Lexeme == "ENDCHOIR") {
			endPos = j
			break
		}
	}
	if endPos == -1 {
		return i, fmt.Errorf("CHOIR: missing ENDCHOIR for block starting at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}

	// Collect starting indices of SUMMON statements inside the CHOIR body.
	var starts []int
	j := i
	for j < endPos {
		tok := tokens[j]
		if tok.Type == TOK_NEWLINE {
			j++
			continue
		}

		if tok.Type != TOK_SUMMON {
			return j, fmt.Errorf(
				"CHOIR: only SUMMON statements are allowed inside CHOIR (found %s at %s:%d:%d)",
				tok.Type, tok.File, tok.Line, tok.Column,
			)
		}

		stmtStart := j
		starts = append(starts, stmtStart)

		// Walk to end of this statement: DOT / NEWLINE / ENDCHOIR / ENDWORK
		for j < endPos &&
			tokens[j].Type != TOK_DOT &&
			tokens[j].Type != TOK_NEWLINE &&
			tokens[j].Type != TOK_ENDWORK &&
			tokens[j].Type != TOK_ENDCHOIR {
			j++
		}
		if j < endPos && tokens[j].Type == TOK_DOT {
			j++
		}

		// Skip blank lines before next statement
		for j < endPos && tokens[j].Type == TOK_NEWLINE {
			j++
		}
	}

	// No SUMMONs? Just skip CHOIR and move on.
	if len(starts) == 0 {
		k := endPos + 1
		if k < len(tokens) && tokens[k].Type == TOK_DOT {
			k++
		}
		return k, nil
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(starts))

	for _, start := range starts {
		wg.Add(1)

		go func(startIdx int) {
			defer wg.Done()
			// Use regular SUMMON semantics; ignore returned index since
			// CHOIR controls its own scanning.
			_, err := execSummonStmt(prog, tokens, startIdx, sigils)
			if err != nil {
				errs <- err
			}
		}(start)
	}

	wg.Wait()
	close(errs)

	// If any SUMMON failed, bubble the first error.
	for err := range errs {
		if err != nil {
			return endPos + 1, err
		}
	}

	// Resume after ENDCHOIR (and optional DOT)
	k := endPos + 1
	if k < len(tokens) && tokens[k].Type == TOK_DOT {
		k++
	}
	return k, nil
}

// injectRequestSigils populates SIGILs for the current HTTP request.
// These are available inside any WORK run via ALTAR, or inline SEND BACK.
//
// Exposed SIGILs (all TEXT):
//
//	REQUEST_METHOD  -> "GET", "POST", etc.
//	REQUEST_PATH    -> "/hello"
//	REQUEST_QUERY   -> raw query string, e.g. "name=Ada&x=1"
//	REQUEST_BODY    -> request body as text (best-effort)
//
// Additionally, each query parameter key is exposed as:
//
//	Q_<UPPERCASE_KEY>  -> first value
//
// e.g. ?name=Ada  => SIGIL Q_NAME BE "Ada"
func injectRequestSigils(child sigilTable, r *http.Request) {
	if r == nil {
		return
	}

	// Basic fields
	child["REQUEST_METHOD"] = r.Method
	if r.URL != nil {
		child["REQUEST_PATH"] = r.URL.Path
		child["REQUEST_QUERY"] = r.URL.RawQuery
	} else {
		child["REQUEST_PATH"] = ""
		child["REQUEST_QUERY"] = ""
	}

	// Query params: Q_<KEY>
	if r.URL != nil {
		q := r.URL.Query()
		for key, vals := range q {
			if len(vals) == 0 {
				continue
			}
			sigilName := "Q_" + strings.ToUpper(key)
			child[sigilName] = vals[0]
		}
	}

	// Body (best-effort)
	if r.Body != nil {
		bodyBytes, err := io.ReadAll(r.Body)
		if err == nil {
			child["REQUEST_BODY"] = string(bodyBytes)
		} else {
			child["REQUEST_BODY"] = ""
		}
		// We don't need the body again in this runtime, so we don't re-wrap it.
	} else {
		child["REQUEST_BODY"] = ""
	}
}

// chooseContentType picks the HTTP Content-Type for an ALTAR response.
// Priority:
//  1. SIGIL response_content_type (if set by the WORK or inline route)
//  2. ".json" suffix on the route path -> application/json
//  3. fallback to the provided defaultCT.
func chooseContentType(defaultCT, path string, sigils sigilTable) string {
	if ct, ok := sigils["response_content_type"]; ok && ct != "" {
		return ct
	}
	if strings.HasSuffix(path, ".json") {
		return "application/json; charset=utf-8"
	}
	return defaultCT
}

// ---------------- ALTAR / ROUTE Canticle ----------------
//
// ALTAR my_server AT PORT 15080:
//
//	ROUTE GET "/hello" TO WORK HELLO.
//
// ENDALTAR.
//
// ALTAR AT :15080:
//
//	ROUTE GET "/hello" TO WORK HELLO.
//	ROUTE GET "/ok"    TO SEND BACK "OK".
//	ROUTE GET "/sum"   TO SEND BACK "1 + 2 * 3".
//
// ENDALTAR.
//
// v1 semantics:
// - Start (or reuse) an HTTP server at given addr.
// - Register each ROUTE inside this ALTAR block.
// - Then block the main goroutine so the server stays alive.
func execAltarBlock(prog *Program, tokens []Token, i int, sigils sigilTable) (int, error) {
	startTok := tokens[i] // TOK_ALTAR
	i++

	// Expect: AT
	if i >= len(tokens) || tokens[i].Type != TOK_AT {
		return i, fmt.Errorf("ALTAR: expected AT after ALTAR at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}
	i++

	// ---------- parse address after AT ----------
	if i >= len(tokens) {
		return i, fmt.Errorf("ALTAR: expected port or address after AT")
	}

	var addr string
	tok := tokens[i]

	switch tok.Type {
	case TOK_STRING:
		// e.g. ":15080" or "localhost:15080"
		addr = tok.Lexeme
		i++

	case TOK_COLON:
		// :15080
		if i+1 >= len(tokens) || tokens[i+1].Type != TOK_NUM {
			return i, fmt.Errorf("ALTAR: expected numeric port after ':' at %s:%d:%d",
				tok.File, tok.Line, tok.Column)
		}
		addr = ":" + tokens[i+1].Lexeme
		i += 2

	case TOK_NUM:
		// bare 15080 → ":15080"
		addr = ":" + tok.Lexeme
		i++

	default:
		return i, fmt.Errorf("ALTAR: invalid address token %s at %s:%d:%d",
			tok.Type, tok.File, tok.Line, tok.Column)
	}

	// Optional colon after address: ALTAR AT :15080:
	if i < len(tokens) && tokens[i].Type == TOK_COLON {
		i++
	}

	fmt.Printf("[SIC ALTAR] ALTAR awakening at %s.\n", addr)

	// ---------- init / start server (singleton) ----------
	altarMu.Lock()
	if globalAltar == nil {
		globalAltar = &altarServer{
			addr: addr,
			mux:  http.NewServeMux(),
		}
	} else if globalAltar.addr != addr {
		prev := globalAltar.addr
		altarMu.Unlock()
		return i, fmt.Errorf("ALTAR: server already bound to %s, cannot rebind to %s",
			prev, addr)
	}

	srv := globalAltar
	if !srv.started {
		srv.started = true
		go func(s *altarServer) {
			fmt.Fprintf(os.Stderr, "[SIC ALTAR] HTTP server listening on %s.\n", s.addr)
			if err := http.ListenAndServe(s.addr, s.mux); err != nil {
				fmt.Fprintf(os.Stderr, "[SIC ALTAR] server error: %v\n", err)
			}
		}(srv)
	}
	altarMu.Unlock()

	// ---------- parse ROUTE statements ----------
	for i < len(tokens) {
		tok := tokens[i]

		// Skip blank lines
		if tok.Type == TOK_NEWLINE {
			i++
			continue
		}

		// ENDALTAR.
		if tok.Type == TOK_ENDALTAR {
			i++
			if i < len(tokens) && tokens[i].Type == TOK_DOT {
				i++
			}
			break
		}

		if tok.Type != TOK_ROUTE {
			return i, fmt.Errorf("ALTAR: expected ROUTE or ENDALTAR, found %s at %s:%d:%d",
				tok.Type, tok.File, tok.Line, tok.Column)
		}
		i++ // after ROUTE

		// HTTP method
		if i >= len(tokens) ||
			!((tokens[i].Type == TOK_GET) ||
				(tokens[i].Type == TOK_POST) ||
				(tokens[i].Type == TOK_PUT) ||
				(tokens[i].Type == TOK_DELETE)) {
			return i, fmt.Errorf("ALTAR: expected HTTP method after ROUTE at %s:%d:%d",
				tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
		}
		method := tokens[i].Lexeme
		i++

		// Path
		if i >= len(tokens) {
			return i, fmt.Errorf("ALTAR: missing path after method at %s:%d:%d",
				tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
		}
		var path string
		if tokens[i].Type == TOK_STRING {
			path = tokens[i].Lexeme
			i++
		} else {
			path = tokens[i].Lexeme
			i++
		}

		// Expect IDENT "TO"
		if i >= len(tokens) || !(tokens[i].Type == TOK_IDENT && strings.EqualFold(tokens[i].Lexeme, "TO")) {
			return i, fmt.Errorf("ALTAR: expected TO after ROUTE %s %s at %s:%d:%d",
				method, path, tokens[i].File, tokens[i].Line, tokens[i].Column)
		}
		i++ // after TO

		if i >= len(tokens) {
			return i, fmt.Errorf("ALTAR: expected WORK or SEND after TO at %s:%d:%d",
				tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
		}

		// ----------------------------------------------------
		// 1) ROUTE ... TO WORK <handler>.
		// ----------------------------------------------------
		if tokens[i].Type == TOK_WORK {
			i++ // WORK

			if i >= len(tokens) || tokens[i].Type != TOK_IDENT {
				return i, fmt.Errorf("ALTAR: expected WORK name after TO at %s:%d:%d",
					tokens[i].File, tokens[i].Line, tokens[i].Column)
			}
			handlerName := tokens[i].Lexeme
			i++

			// Optional DOT
			if i < len(tokens) && tokens[i].Type == TOK_DOT {
				i++
			}

			fmt.Printf("[SIC ALTAR ROUTE] Route %s %s -> WORK %s\n",
				method, path, handlerName)

			// Register handler using existing WORK semantics
			altarMu.Lock()
			m := method
			p := path
			h := handlerName
			parentSigils := sigils
			mux := srv.mux

			mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != m {
					http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
					return
				}

				work := findWork(prog, h)
				if work == nil {
					fmt.Fprintf(os.Stderr, "[SIC ALTAR] handler WORK %s not found\n", h)
					http.Error(w, "handler not found", http.StatusInternalServerError)
					return
				}

				// Clone sigils like SUMMON does
				child := make(sigilTable)
				for k, v := range parentSigils {
					child[k] = v
				}

				// Inject request details into SIGILs
				injectRequestSigils(child, r)

				// Capture the ritual's answer as HTTP body
				body, err := execWork(prog, work, child, true /* captureAnswer */)
				if err != nil {
					fmt.Fprintf(os.Stderr, "[SIC ALTAR] handler %s error: %v\n", h, err)
					http.Error(w, "internal error", http.StatusInternalServerError)
					return
				}

				if body == "" {
					body = "OK"
				}

				// Decide Content-Type:
				// - SIGIL response_content_type wins if set,
				// - else .json suffix -> application/json,
				// - else fall back to text/plain.
				ct := chooseContentType("text/plain; charset=utf-8", p, child)
				w.Header().Set("Content-Type", ct)
				_, _ = w.Write([]byte(body + "\n"))
			})

			altarMu.Unlock()
			continue
		}

		// ----------------------------------------------------
		// 2) ROUTE ... TO SEND BACK <expr>.
		//    BACK is optional; SEND <expr> also works.
		// ----------------------------------------------------
		if tokens[i].Type == TOK_SEND {
			i++ // consume SEND

			// Allow blank lines between SEND and BACK/expression
			for i < len(tokens) && tokens[i].Type == TOK_NEWLINE {
				i++
			}

			// Optional BACK keyword (by lexeme, any token type)
			if i < len(tokens) && strings.EqualFold(tokens[i].Lexeme, "BACK") {
				i++ // after BACK

				// Optional colon, e.g. SEND BACK: "OK".
				if i < len(tokens) && tokens[i].Type == TOK_COLON {
					i++
				}
			}

			// Expression starts here and runs up to DOT / NEWLINE / ENDALTAR
			exprStart := i
			for i < len(tokens) &&
				tokens[i].Type != TOK_DOT &&
				tokens[i].Type != TOK_NEWLINE &&
				tokens[i].Type != TOK_ENDALTAR {
				i++
			}
			exprEnd := i

			// Copy expression tokens so the closure has a stable slice
			exprTokens := make([]Token, exprEnd-exprStart)
			copy(exprTokens, tokens[exprStart:exprEnd])

			// Optional DOT at end of route line
			if i < len(tokens) && tokens[i].Type == TOK_DOT {
				i++
			}

			fmt.Printf("[SIC ALTAR ROUTE] Route %s %s -> inline SEND BACK\n",
				method, path)

			altarMu.Lock()

			m := method
			p := path
			exprCopy := exprTokens
			parentSigils := sigils
			mux := srv.mux

			mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != m {
					http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
					return
				}

				// Clone sigils like SUMMON does
				child := make(sigilTable)
				for k, v := range parentSigils {
					child[k] = v
				}

				// Inject request details into SIGILs
				injectRequestSigils(child, r)

				val, err := evalStringExpr(prog, exprCopy, child)
				if err != nil {
					fmt.Fprintf(os.Stderr, "[SIC ALTAR] inline SEND BACK error: %v\n", err)
					http.Error(w, "internal error", http.StatusInternalServerError)
					return
				}

				// Same content-type logic as WORK handlers
				ct := chooseContentType("text/plain; charset=utf-8", p, child)
				w.Header().Set("Content-Type", ct)
				_, _ = w.Write([]byte(val + "\n"))
			})

			altarMu.Unlock()
			continue
		}

		// If we reach here, token after TO was neither WORK nor SEND
		return i, fmt.Errorf("ALTAR: expected WORK or SEND after TO at %s:%d:%d",
			tokens[i].File, tokens[i].Line, tokens[i].Column)
	}

	// ---------- BLOCK HERE: keep process alive ----------
	fmt.Fprintf(os.Stderr, "[SIC ALTAR] ALTAR is now holding the process open on %s.\n", addr)
	select {} // block forever; never return to MAIN
}

// SUMMON as a statement: ignore the returned value, keep side-effects.
// Also consume trailing '.' or newline so WEAVE doesn't see stray tokens.
func execSummonStmt(prog *Program, tokens []Token, i int, sigils sigilTable) (int, error) {
	_, consumed, err := evalSummonExpr(prog, tokens, i, sigils)
	if err != nil {
		return i + consumed, err
	}
	i += consumed

	// Consume any trailing junk up to DOT / NEWLINE / ENDWEAVE / ENDWORK
	for i < len(tokens) &&
		tokens[i].Type != TOK_DOT &&
		tokens[i].Type != TOK_NEWLINE &&
		tokens[i].Type != TOK_ENDWEAVE &&
		tokens[i].Type != TOK_ENDWORK {
		i++
	}
	if i < len(tokens) && tokens[i].Type == TOK_DOT {
		i++
	}
	return i, nil
}

// ---------------- Expression evaluation (strings + SUMMON) ----------------

// SUMMON expression:
//
//	SUMMON WORK GREETING WITH SIGIL "World"
func evalSummonExpr(prog *Program, tokens []Token, start int, sigils sigilTable) (string, int, error) {
	i := start // tokens[i] is TOK_SUMMON

	i++
	if i >= len(tokens) || tokens[i].Type != TOK_WORK {
		return "", 0, fmt.Errorf("SUMMON: expected WORK after SUMMON at %s:%d:%d",
			tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	i++

	if i >= len(tokens) || tokens[i].Type != TOK_IDENT {
		return "", 0, fmt.Errorf("SUMMON: expected WORK name after WORK at %s:%d:%d",
			tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	targetName := tokens[i].Lexeme
	i++

	argVal := ""

	// Optional: WITH SIGIL <arg>
	if i < len(tokens) && tokens[i].Type == TOK_WITH {
		i++
		if i < len(tokens) && tokens[i].Type == TOK_SIGIL {
			i++
		}

		if i >= len(tokens) {
			return "", 0, fmt.Errorf("SUMMON: missing argument after WITH for WORK %s", targetName)
		}

		switch tokens[i].Type {
		case TOK_STRING:
			argVal = tokens[i].Lexeme
			i++
		case TOK_IDENT:
			// treat as sigil name
			argVal, _ = getSigil(sigils, tokens[i].Lexeme)
			i++
		case TOK_UNUSED:
			// Sentinel: explicit "no argument" / ignored parameter
			argVal = ""
			i++
		default:
			return "", 0, fmt.Errorf("SUMMON: unsupported argument token %s at %s:%d:%d",
				tokens[i].Type, tokens[i].File, tokens[i].Line, tokens[i].Column)
		}
	}

	target := findWork(prog, targetName)
	if target == nil {
		return "", 0, fmt.Errorf("SUMMON: WORK %s not found", targetName)
	}

	// Build child environment: inherit sigils, override first parameter if present
	childSigils := make(sigilTable)
	for k, v := range sigils {
		childSigils[k] = v
	}
	if len(target.SigilParams) > 0 {
		childSigils[target.SigilParams[0]] = argVal
	}

	result, err := execWork(prog, target, childSigils, true)
	if err != nil {
		return "", 0, err
	}

	consumed := i - start
	return result, consumed, nil
}

func evalExpr(prog *Program, tokens []Token, i int, sigils sigilTable) (string, int, error) {
	start := i

	// ---- Parse left operand ----
	leftTok := tokens[i]
	var leftVal string

	switch leftTok.Type {
	case TOK_IDENT:
		v, ok := sigils[leftTok.Lexeme]
		if !ok {
			return "", 0, fmt.Errorf("unknown SIGIL %s at %s:%d:%d",
				leftTok.Lexeme, leftTok.File, leftTok.Line, leftTok.Column)
		}
		leftVal = v
		i++

	case TOK_STRING:
		leftVal = leftTok.Lexeme
		i++

	case TOK_NUM:
		leftVal = leftTok.Lexeme
		i++

	default:
		return "", 0, fmt.Errorf("unexpected %s in expr at %s:%d:%d",
			leftTok.Type, leftTok.File, leftTok.Line, leftTok.Column)
	}

	// ---- Check if there's an operator ----
	if i >= len(tokens) || tokens[i].Type != TOK_PLUS {
		// Single value expression
		return leftVal, i - start, nil
	}

	// We saw a +
	i++ // consume +

	// ---- Parse right operand ----
	if i >= len(tokens) {
		return "", 0, fmt.Errorf("expected right operand after +")
	}

	rightTok := tokens[i]
	var rightVal string

	switch rightTok.Type {
	case TOK_IDENT:
		v, ok := sigils[rightTok.Lexeme]
		if !ok {
			return "", 0, fmt.Errorf("unknown SIGIL %s", rightTok.Lexeme)
		}
		rightVal = v
		i++

	case TOK_STRING:
		rightVal = rightTok.Lexeme
		i++

	case TOK_NUM:
		rightVal = rightTok.Lexeme
		i++

	default:
		return "", 0, fmt.Errorf("unexpected %s after +", rightTok.Type)
	}

	// ---- Evaluate the expression ----

	// Try numeric addition first
	if lv, err1 := strconv.Atoi(leftVal); err1 == nil {
		if rv, err2 := strconv.Atoi(rightVal); err2 == nil {
			return fmt.Sprintf("%d", lv+rv), i - start, nil
		}
	}

	// Otherwise: string concatenation
	return leftVal + rightVal, i - start, nil
}
