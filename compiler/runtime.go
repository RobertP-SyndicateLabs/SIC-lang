package compiler

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---- ALTAR runtime ----

type altarServer struct {
	addr    string
	mux     *http.ServeMux
	started bool

	// routeKey = method + " " + path
	registered map[string]bool
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

const sicInvisibleMetaPrefix = "__SIC_META_INVISIBLE__"

// isInvisibleSigil reports whether `name` is marked invisible in this environment.
func isInvisibleSigil(sigils sigilTable, name string) bool {
	if sigils == nil || name == "" {
		return false
	}
	_, ok := sigils[sicInvisibleMetaPrefix+name]
	return ok
}

// markInvisibleSigil marks `name` as invisible in this environment.
func markInvisibleSigil(sigils sigilTable, name string) {
	if sigils == nil || name == "" {
		return
	}
	sigils[sicInvisibleMetaPrefix+name] = "1"
}

// unmarkInvisibleSigil removes invisibility from `name` (optional, but handy).
func unmarkInvisibleSigil(sigils sigilTable, name string) {
	if sigils == nil || name == "" {
		return
	}
	delete(sigils, sicInvisibleMetaPrefix+name)
}

// setSigilInvisible sets a sigil value and marks it invisible.
func setSigilInvisible(sigils sigilTable, name, v string) {
	setSigil(sigils, name, v)
	markInvisibleSigil(sigils, name)
}

// cloneVisibleSigils copies only visible sigils from src->dst.
// It also skips all internal meta keys.
func cloneVisibleSigils(dst, src sigilTable) {
	for k, v := range src {
		// skip meta keys entirely
		if strings.HasPrefix(k, sicInvisibleMetaPrefix) {
			continue
		}
		// skip invisibles by default
		if isInvisibleSigil(src, k) {
			continue
		}
		dst[k] = v
	}
}

const sicRedacted = "[REDACTED]"

func redactIfTainted(val string, tainted bool) string {
	if tainted {
		return sicRedacted
	}
	return val
}

// exprSingleSigilRef returns (sigilName, true) if exprTokens represent
// a *direct* sigil reference with no operators.
//
// Supported forms:
//   - SIGIL <name>         (TOK_SIGIL or TOK_SIGIL + TOK_IDENT)
//   - $<name>              (TOK_DOLLAR + TOK_IDENT)
//   - <name>               (TOK_IDENT)  // your parsePrimary treats ident as sigil lookup
func exprSingleSigilRef(exprTokens []Token) (string, bool) {
	if len(exprTokens) == 0 {
		return "", false
	}

	// Trim surrounding NEWLINEs (shouldn't be present, but be defensive)
	start := 0
	for start < len(exprTokens) && exprTokens[start].Type == TOK_NEWLINE {
		start++
	}
	end := len(exprTokens)
	for end > start && exprTokens[end-1].Type == TOK_NEWLINE {
		end--
	}
	toks := exprTokens[start:end]
	if len(toks) == 0 {
		return "", false
	}

	// SIGIL <ident>
	if len(toks) == 2 && (toks[0].Type == TOK_SIGIL || toks[0].Type == TOK_SIGIL) && toks[1].Type == TOK_IDENT {
		return toks[1].Lexeme, true
	}

	// $ <ident>
	if len(toks) == 2 && toks[0].Type == TOK_DOLLAR && toks[1].Type == TOK_IDENT {
		return toks[1].Lexeme, true
	}

	// bare IDENT (your runtime interprets as sigil lookup)
	if len(toks) == 1 && toks[0].Type == TOK_IDENT {
		return toks[0].Lexeme, true
	}

	return "", false
}

func redactIfInvisible(sigils sigilTable, name, val string) string {
	if name != "" && isInvisibleSigil(sigils, name) {
		return sicRedacted
	}
	return val
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
	kind    exprKind
	s       string
	i       int64
	f       float64
	b       bool
	tainted bool // true if this value depends on an INVISIBLE sigil
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

// Utility: copy taint onto a produced value
func withTaint(v exprValue, t bool) exprValue {
	v.tainted = t
	return v
}

// Utility: combine taint from two operands onto an output
func combineTaint(out, a, b exprValue) exprValue {
	out.tainted = a.tainted || b.tainted
	return out
}

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

func isWord(tok Token, w string) bool {
	return tok.Type == TOK_IDENT && strings.EqualFold(tok.Lexeme, w)
}

func lexemeIs(tok Token, w string) bool {
	// Matches keyword tokens that still carry lexeme, and IDENT fallback.
	return strings.EqualFold(tok.Lexeme, w)
}

// normalizeExprTokens rewrites legacy/statement-style tokens into expression-style tokens.
// This lets your parseOr()/parsePrimary() expression engine evaluate things like:
//
//	SIGIL name EQUALS "World"   ->   name == "World"
//
// It also avoids "unexpected SIGIL/FOR/SECONDS" showing up inside expressions.
func normalizeExprTokens(tokens []Token) []Token {
	if len(tokens) == 0 {
		return tokens
	}

	out := make([]Token, 0, len(tokens))

	for i := 0; i < len(tokens); i++ {
		t := tokens[i]

		// Legacy: "SIGIL <ident>" used as a value reference in IF/WHILE conditions.
		// Lexer emits TOK_SIGIL for the keyword "SIGIL".
		if t.Type == TOK_SIGIL && strings.EqualFold(t.Lexeme, "SIGIL") {
			// If it's followed by an IDENT, drop the SIGIL keyword and keep the name.
			if i+1 < len(tokens) && tokens[i+1].Type == TOK_IDENT {
				out = append(out, tokens[i+1])
				i++ // consumed the IDENT too
				continue
			}
			// Otherwise keep it (expression parser may still handle it, but usually this is invalid)
			out = append(out, t)
			continue
		}

		// Legacy comparator keyword: EQUALS -> ==
		if t.Type == TOK_IDENT && strings.EqualFold(t.Lexeme, "EQUALS") {
			t.Type = TOK_EQ
			t.Lexeme = "=="
			out = append(out, t)
			continue
		}

		// Some scripts use ENDIF/ENDWHILE forms as IDENT tokens; those should never
		// be evaluated as expression operands.
		if t.Type == TOK_FOR || t.Type == TOK_SECONDS {
			// These should not exist in a normal expression; drop them if they leak in.
			continue
		}

		out = append(out, t)
	}

	return out
}

func evalBoolExpr(prog *Program, tokens []Token, sigils sigilTable) (bool, error) {
	if len(tokens) == 0 {
		return false, nil
	}

	tokens = normalizeExprTokens(tokens)

	idx := 0
	v, err := parseOr(prog, tokens, &idx, sigils)
	if err != nil {
		return false, err
	}

	return v.asBool(), nil
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

	// Normalize legacy tokens ("SIGIL name", "EQUALS") into expression tokens.
	tokens = normalizeExprTokens(tokens)

	i := 0
	val, err := parseOr(prog, tokens, &i, sigils)
	if err != nil {
		return "", err
	}
	return val.String(), nil
}

func evalStringExprTainted(prog *Program, tokens []Token, sigils sigilTable) (string, bool, error) {
	if len(tokens) == 0 {
		return "", false, nil
	}

	end := len(tokens)
	for idx, tok := range tokens {
		switch tok.Type {
		case TOK_DOT, TOK_COLON, TOK_NEWLINE, TOK_ENDWEAVE, TOK_ENDWORK, TOK_FROM:
			end = idx
			goto sliced
		}
	}
sliced:
	tokens = tokens[:end]
	if len(tokens) == 0 {
		return "", false, nil
	}

	tokens = normalizeExprTokens(tokens)

	i := 0
	val, err := parseOr(prog, tokens, &i, sigils)
	if err != nil {
		return "", false, err
	}

	return val.String(), val.tainted, nil
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
		left = combineTaint(makeBool(left.asBool() || right.asBool()), left, right)
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
		left = combineTaint(makeBool(left.asBool() && right.asBool()), left, right)
	}
	return left, nil
}

func parseEquality(prog *Program, tokens []Token, i *int, sigils sigilTable) (exprValue, error) {
	left, err := parseComparison(prog, tokens, i, sigils)
	if err != nil {
		return exprValue{}, err
	}
	for *i < len(tokens) && (tokens[*i].Type == TOK_EQ || tokens[*i].Type == TOK_NEQ) {
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

		var out exprValue
		if op == TOK_EQ {
			out = makeBool(eq)
		} else {
			out = makeBool(!eq)
		}
		left = combineTaint(out, left, right)
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

		left = combineTaint(makeBool(res), left, right)
	}
	return left, nil
}

func parseTerm(prog *Program, tokens []Token, i *int, sigils sigilTable) (exprValue, error) {
	left, err := parseFactor(prog, tokens, i, sigils)
	if err != nil {
		return exprValue{}, err
	}
	for *i < len(tokens) && (tokens[*i].Type == TOK_PLUS || tokens[*i].Type == TOK_MINUS) {
		op := tokens[*i].Type
		*i++
		right, err := parseFactor(prog, tokens, i, sigils)
		if err != nil {
			return exprValue{}, err
		}

		lf, okL := left.asFloat()
		rf, okR := right.asFloat()

		if okL && okR {
			var out exprValue
			switch op {
			case TOK_PLUS:
				out = makeFloat(lf + rf)
			case TOK_MINUS:
				out = makeFloat(lf - rf)
			}
			left = combineTaint(out, left, right)
		} else {
			if op == TOK_PLUS {
				out := makeText(left.String() + right.String())
				left = combineTaint(out, left, right)
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
			return exprValue{}, fmt.Errorf("non-numeric value in arithmetic expression")
		}

		var out exprValue
		switch op {
		case TOK_STAR:
			out = makeFloat(lf * rf)
		case TOK_SLASH:
			if rf == 0 {
				return exprValue{}, fmt.Errorf("division by zero")
			}
			out = makeFloat(lf / rf)
		case TOK_PERCENT:
			li := int64(lf)
			ri := int64(rf)
			if ri == 0 {
				return exprValue{}, fmt.Errorf("modulo by zero")
			}
			out = makeInt(li % ri)
		}

		left = combineTaint(out, left, right)
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
		return withTaint(makeFloat(-lf), val.tainted), nil
	}

	if tok.Type == TOK_NOT {
		*i++
		val, err := parseUnary(prog, tokens, i, sigils)
		if err != nil {
			return exprValue{}, err
		}
		return withTaint(makeBool(!val.asBool()), val.tainted), nil
	}

	return parsePrimary(prog, tokens, i, sigils)
}

func parsePrimary(prog *Program, tokens []Token, i *int, sigils sigilTable) (exprValue, error) {
	if *i >= len(tokens) {
		return exprValue{}, fmt.Errorf("unexpected end of expression")
	}

	coerce := func(val string) exprValue {
		s := strings.TrimSpace(val)
		if strings.EqualFold(s, "true") {
			return makeBool(true)
		}
		if strings.EqualFold(s, "false") {
			return makeBool(false)
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return makeFloat(f)
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return makeInt(n)
		}
		return makeText(val)
	}

	tok := tokens[*i]

	// Skip glue words inside expressions
	if tok.Type == TOK_FOR || tok.Type == TOK_SECONDS || tok.Type == TOK_SECONDS ||
		(tok.Type == TOK_IDENT && (strings.EqualFold(tok.Lexeme, "BY") ||
			strings.EqualFold(tok.Lexeme, "FOR") ||
			strings.EqualFold(tok.Lexeme, "SECONDS"))) {
		*i++
		return parsePrimary(prog, tokens, i, sigils)
	}

	switch tok.Type {

	case TOK_STRING:
		*i++
		return makeText(tok.Lexeme), nil

	case TOK_TIME_NOW:
		*i++
		return makeInt(time.Now().Unix()), nil

	// "SIGIL name" legacy form
	case TOK_SIGIL:
		*i++
		if *i >= len(tokens) || tokens[*i].Type != TOK_IDENT {
			return exprValue{}, fmt.Errorf("expected SIGIL name after SIGIL at %s:%d:%d",
				tok.File, tok.Line, tok.Column)
		}
		nameTok := tokens[*i]
		name := nameTok.Lexeme
		*i++

		val, ok := sigils[name]
		if !ok {
			return exprValue{}, fmt.Errorf("unknown SIGIL %s at %s:%d:%d",
				name, nameTok.File, nameTok.Line, nameTok.Column)
		}

		v := coerce(val)
		if isInvisibleSigil(sigils, name) {
			v = withTaint(v, true)
		}
		return v, nil

	// $NAME
	case TOK_DOLLAR:
		*i++
		if *i >= len(tokens) || tokens[*i].Type != TOK_IDENT {
			return exprValue{}, fmt.Errorf("expected SIGIL name after $ at %s:%d:%d",
				tok.File, tok.Line, tok.Column)
		}
		nameTok := tokens[*i]
		name := nameTok.Lexeme
		*i++

		if strings.EqualFold(name, "TIME_NOW") {
			return makeInt(time.Now().Unix()), nil
		}

		val, ok := sigils[name]
		if !ok {
			return exprValue{}, fmt.Errorf("unknown SIGIL %s at %s:%d:%d",
				name, nameTok.File, nameTok.Line, nameTok.Column)
		}

		v := coerce(val)
		if isInvisibleSigil(sigils, name) {
			v = withTaint(v, true)
		}
		return v, nil

	case TOK_NUM:
		*i++
		lex := strings.TrimSpace(tok.Lexeme)
		if strings.ContainsAny(lex, ".eE") {
			f, err := strconv.ParseFloat(lex, 64)
			if err != nil {
				return exprValue{}, fmt.Errorf("invalid float literal %q", tok.Lexeme)
			}
			return makeFloat(f), nil
		}
		n, err := strconv.ParseInt(lex, 10, 64)
		if err != nil {
			return exprValue{}, fmt.Errorf("invalid int literal %q", tok.Lexeme)
		}
		return makeInt(n), nil

	// Bare IDENT => sigil lookup
	case TOK_IDENT:
		if strings.EqualFold(tok.Lexeme, "TIME_NOW") {
			*i++
			return makeInt(time.Now().Unix()), nil
		}

		val, ok := sigils[tok.Lexeme]
		if !ok {
			return exprValue{}, fmt.Errorf("unknown SIGIL %s at %s:%d:%d",
				tok.Lexeme, tok.File, tok.Line, tok.Column)
		}
		*i++

		v := coerce(val)
		if isInvisibleSigil(sigils, tok.Lexeme) {
			v = withTaint(v, true)
		}
		return v, nil

	case TOK_LPAREN:
		*i++
		inner, err := parseOr(prog, tokens, i, sigils)
		if err != nil {
			return exprValue{}, err
		}
		if *i >= len(tokens) || tokens[*i].Type != TOK_RPAREN {
			return exprValue{}, fmt.Errorf("expected ')' in expression at %s:%d:%d",
				tok.File, tok.Line, tok.Column)
		}
		*i++
		return inner, nil

	case TOK_SUMMON:
		start := *i
		val, consumed, err := evalSummonExpr(prog, tokens, start, sigils)
		if err != nil {
			return exprValue{}, err
		}
		*i = start + consumed
		// SUMMON result is treated as text. (If you later want taint to flow
		// through SUMMON, you’ll need work-level tainting semantics.)
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
			// Also scrub invisibility metadata if present
			delete(sigils, sicInvisibleMetaPrefix+name)
		}
	}()

	for i < len(tokens) {
		tok := tokens[i]

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
				// Mark this sigil as ephemeral for scrubbing at WORK exit.
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

		case TOK_SLEEP:
			next, err := execSleep(prog, tokens, i, sigils)
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

			case "SLEEP":
				next, err := execSleep(prog, tokens, i, sigils)
				if err != nil {
					return "", err
				}
				i = next
				continue
			}

			// other idents fall through

		case TOK_IF:
			// IF OMEN ... IS PRESENT THEN: (OMEN-aware IF)
			if i+1 < len(tokens) && tokens[i+1].Type == TOK_OMEN {
				next, err := execIfOmen(prog, tokens, i, sigils)
				if err != nil {
					return "", err
				}
				i = next
				continue
			}

			// Normal IF ...
			next, err := execIf(prog, tokens, i, sigils)
			if err != nil {
				return "", err
			}
			i = next
			continue

		case TOK_CHAMBER:
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

func execSleep(prog *Program, tokens []Token, i int, sigils sigilTable) (int, error) {
	startTok := tokens[i] // TOK_SLEEP or TOK_IDENT("SLEEP")
	i++                   // after SLEEP

	// Optional FOR (either keyword token or IDENT)
	if i < len(tokens) && (tokens[i].Type == TOK_FOR ||
		(tokens[i].Type == TOK_IDENT && strings.EqualFold(tokens[i].Lexeme, "FOR"))) {
		i++
	}

	// Duration expression until SECONDS / DOT / NEWLINE / ENDWORK
	exprStart := i
	for i < len(tokens) &&
		tokens[i].Type != TOK_DOT &&
		tokens[i].Type != TOK_NEWLINE &&
		tokens[i].Type != TOK_ENDWORK &&
		tokens[i].Type != TOK_SECONDS &&
		!(tokens[i].Type == TOK_IDENT && strings.EqualFold(tokens[i].Lexeme, "SECONDS")) {
		i++
	}

	if exprStart == i {
		return i, fmt.Errorf("SLEEP: expected duration at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}

	// Evaluate duration expression
	exprTokens := normalizeExprTokens(tokens[exprStart:i])
	idx := 0
	v, err := parseOr(prog, exprTokens, &idx, sigils)
	if err != nil {
		return i, err
	}

	secs, ok := v.asFloat()
	if !ok {
		return i, fmt.Errorf("SLEEP: duration must be numeric at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}
	if secs < 0 {
		return i, fmt.Errorf("SLEEP: duration must be >= 0 at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}

	// Optional SECONDS token or IDENT("SECONDS")
	if i < len(tokens) && (tokens[i].Type == TOK_SECONDS ||
		(tokens[i].Type == TOK_IDENT && strings.EqualFold(tokens[i].Lexeme, "SECONDS"))) {
		i++
	}

	// Optional DOT
	if i < len(tokens) && tokens[i].Type == TOK_DOT {
		i++
	}

	time.Sleep(time.Duration(secs * float64(time.Second)))
	return i, nil
}

// ---------------- SAY ----------------

// SAY: <expr>.
func execSay(prog *Program, tokens []Token, i int, sigils sigilTable) (int, error) {
	i++ // after SAY

	if i >= len(tokens) || tokens[i].Type != TOK_COLON {
		return i, fmt.Errorf("SAY: expected COLON after SAY at %s:%d:%d",
			tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	i++

	exprStart := i
	for i < len(tokens) &&
		tokens[i].Type != TOK_DOT &&
		tokens[i].Type != TOK_NEWLINE &&
		tokens[i].Type != TOK_ENDWORK {
		i++
	}
	exprEnd := i

	msg, tainted, err := evalStringExprTainted(prog, tokens[exprStart:exprEnd], sigils)
	if err != nil {
		return i, err
	}

	fmt.Println("[SIC SAY]", redactIfTainted(msg, tainted))

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

// parseSigilTarget parses the "target sigil name" in a few ergonomic forms:
//
//	LET SIGIL NAME BE ...
//	LET NAME BE ...
//	LET $NAME BE ...        (if '$' is tokenized as TOK_SIGIL)
//
// It returns (name, newIndex, error).
func parseSigilTarget(tokens []Token, i int) (string, int, error) {
	if i >= len(tokens) {
		return "", i, fmt.Errorf("expected SIGIL name")
	}

	// Optional keyword SIGIL:
	// - lexer emits TOK_SIGIL for keyword "SIGIL"
	// - tolerate IDENT "SIGIL" too, just in case
	if tokens[i].Type == TOK_SIGIL ||
		(tokens[i].Type == TOK_IDENT && strings.EqualFold(tokens[i].Lexeme, "SIGIL")) {
		i++
		if i >= len(tokens) {
			return "", i, fmt.Errorf("expected name after SIGIL")
		}
	}

	// Optional '$' marker
	if i < len(tokens) && tokens[i].Type == TOK_DOLLAR {
		i++
		if i >= len(tokens) {
			return "", i, fmt.Errorf("expected name after $")
		}
	}

	// Name must be IDENT (also allow "$NAME" glued into IDENT if you want)
	if i >= len(tokens) || tokens[i].Type != TOK_IDENT {
		return "", i, fmt.Errorf("expected SIGIL name")
	}

	name := tokens[i].Lexeme
	if strings.HasPrefix(name, "$") {
		name = strings.TrimPrefix(name, "$")
	}
	if name == "" {
		return "", i, fmt.Errorf("empty SIGIL name")
	}

	return name, i + 1, nil
}

// ---------------- LET SIGIL ----------------
//
// Accepts all of:
//
//	LET SIGIL name BE <expr>.
//	LET name BE <expr>.
//	LET $name BE <expr>.          (tolerated; treated same as name)
//
// Also supports:
//
//	LET EPHEMERAL SIGIL name BE <expr>.
//	LET EPHEMERAL name BE <expr>.
func execLet(prog *Program, tokens []Token, i int, sigils sigilTable, ephemeral map[string]bool) (int, error) {
	startTok := tokens[i] // TOK_LET
	i++

	// Optional EPHEMERAL
	isEphemeral := false
	if i < len(tokens) && tokens[i].Type == TOK_EPHEMERAL {
		isEphemeral = true
		i++
	}

	// Optional INVISIBLE (either dedicated token if you add one later,
	// or IDENT "INVISIBLE" for now)
	isInvisible := false
	if i < len(tokens) {
		if tokens[i].Type == TOK_IDENT && strings.EqualFold(tokens[i].Lexeme, "INVISIBLE") {
			isInvisible = true
			i++
		}
		// If you later add TOK_INVISIBLE, you can also accept it here:
		// if tokens[i].Type == TOK_INVISIBLE { isInvisible = true; i++ }
	}

	// Parse target name:
	//   LET [EPHEMERAL] [INVISIBLE] SIGIL X BE ...
	//   LET [EPHEMERAL] [INVISIBLE] X BE ...              (allowed)
	//   LET [EPHEMERAL] [INVISIBLE] $X BE ...             (tolerated)
	name, next, err := parseSigilTarget(tokens, i)
	if err != nil {
		return i, fmt.Errorf("LET: %v at %s:%d:%d",
			err, startTok.File, startTok.Line, startTok.Column)
	}
	i = next

	// Expect BE (TOK_BE or IDENT "BE")
	if i >= len(tokens) || !(tokens[i].Type == TOK_BE ||
		(tokens[i].Type == TOK_IDENT && strings.EqualFold(tokens[i].Lexeme, "BE"))) {
		return i, fmt.Errorf("LET: expected BE after SIGIL %s at %s:%d:%d",
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
		return i, err
	}

	// Assign sigil with visibility semantics
	if isInvisible {
		setSigilInvisible(sigils, name, val) // sets value + marks invisible
	} else {
		setSigil(sigils, name, val)
		// Optional: if overwriting a previously invisible sigil, you might
		// choose to keep it invisible or clear invisibility. Current choice:
		// leave invisibility as-is unless explicitly set invisible.
		// If you want overwrite to become visible, uncomment:
		// unmarkInvisibleSigil(sigils, name)
	}

	// Mark EPHEMERAL cleanup
	if isEphemeral && ephemeral != nil {
		ephemeral[name] = true
		// Also scrub invisibility meta for that sigil on cleanup if you ever
		// unify cleanup later. (Current cleanup deletes only sigils[name].)
		// We'll address this properly when we wire EPHEMERAL + INVISIBLE fully.
	}

	// Optional trailing DOT
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
	i++ // after EPHEMERAL

	// Optional LET: "EPHEMERAL LET SIGIL ..." is tolerated.
	if i < len(tokens) && tokens[i].Type == TOK_LET {
		i++
	}

	// Parse sigil target name:
	//   EPHEMERAL SIGIL X BE ...
	//   EPHEMERAL X BE ...          (allowed)
	//   EPHEMERAL $X BE ...         (if lexer supports)
	name, next, err := parseSigilTarget(tokens, i)
	if err != nil {
		return i, "", fmt.Errorf("EPHEMERAL: %v", err)
	}
	i = next

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

	for i < len(tokens) && tokens[i].Type == TOK_NEWLINE {
		i++
	}

	if i >= len(tokens) || !strings.EqualFold(tokens[i].Lexeme, "BACK") {
		return "", i, fmt.Errorf(
			"SEND BACK: expected BACK after SEND at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column,
		)
	}
	i++

	if i < len(tokens) && tokens[i].Type == TOK_COLON {
		i++
	}

	// Special-case: SEND BACK SIGIL name.
	if i < len(tokens) && (tokens[i].Type == TOK_SIGIL || tokens[i].Type == TOK_SIGIL) {
		i++

		if i >= len(tokens) || tokens[i].Type != TOK_IDENT {
			return "", i, fmt.Errorf(
				"SEND BACK: expected SIGIL name after SIGIL at %s:%d:%d",
				tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column,
			)
		}
		name := tokens[i].Lexeme
		val, _ := getSigil(sigils, name)
		i++

		for i < len(tokens) &&
			tokens[i].Type != TOK_DOT &&
			tokens[i].Type != TOK_NEWLINE &&
			tokens[i].Type != TOK_ENDWORK {
			i++
		}
		if i < len(tokens) && tokens[i].Type == TOK_DOT {
			i++
		}

		// Redact if invisible
		if isInvisibleSigil(sigils, name) {
			return sicRedacted, i, nil
		}
		return val, i, nil
	}

	// General: SEND BACK <expr>.
	exprStart := i
	for i < len(tokens) &&
		tokens[i].Type != TOK_DOT &&
		tokens[i].Type != TOK_NEWLINE &&
		tokens[i].Type != TOK_ENDWORK {
		i++
	}
	exprEnd := i

	val, tainted, err := evalStringExprTainted(prog, tokens[exprStart:exprEnd], sigils)
	if err != nil {
		return "", i, err
	}

	if i < len(tokens) && tokens[i].Type == TOK_DOT {
		i++
	}

	return redactIfTainted(val, tainted), i, nil
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

	// ---- Parse condition as tokens up to THEN / COLON ----
	condStart := i
	for i < len(tokens) &&
		tokens[i].Type != TOK_COLON &&
		!(tokens[i].Type == TOK_IDENT && strings.EqualFold(tokens[i].Lexeme, "THEN")) {
		i++
	}
	condTokens := tokens[condStart:i]
	if len(condTokens) == 0 {
		return i, fmt.Errorf("IF: expected condition after IF at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}

	// Optional THEN
	if i < len(tokens) && tokens[i].Type == TOK_IDENT && strings.EqualFold(tokens[i].Lexeme, "THEN") {
		i++
	}

	// Expect COLON
	if i >= len(tokens) || tokens[i].Type != TOK_COLON {
		return i, fmt.Errorf("IF: expected COLON after condition at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}
	i++ // after COLON

	// Skip NEWLINEs
	for i < len(tokens) && tokens[i].Type == TOK_NEWLINE {
		i++
	}

	thenStart := i
	elseStart := -1
	endPos := -1

	// We consider both:
	//   END.      (TOK_END + optional DOT)
	//   ENDIF.    (IDENT "ENDIF" + optional DOT)
	depth := 1
	for j := i; j < len(tokens); j++ {
		t := tokens[j]

		// Nested IF
		if t.Type == TOK_IF {
			depth++
			continue
		}

		// ELSE only at current depth
		if t.Type == TOK_ELSE && depth == 1 {
			elseStart = j
			continue
		}

		// END closes an IF if depth==1, otherwise reduces nesting.
		if t.Type == TOK_END {
			if depth == 1 {
				endPos = j
				break
			}
			depth--
			continue
		}

		// ENDIF (often lexed as IDENT)
		if t.Type == TOK_IDENT && strings.EqualFold(t.Lexeme, "ENDIF") {
			if depth == 1 {
				endPos = j
				break
			}
			depth--
			continue
		}
	}

	if endPos == -1 {
		// Match your existing wording style
		return i, fmt.Errorf("IF: unmatched ENDIF for IF at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}

	// Evaluate condition (boolean expression)
	cond, err := evalBoolExpr(prog, condTokens, sigils)
	if err != nil {
		return i, err
	}

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

	// Resume after END/ENDIF and optional DOT
	k := endPos + 1
	if k < len(tokens) && tokens[k].Type == TOK_DOT {
		k++
	}
	return k, nil
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

	// Condition tokens until COLON
	condStart := i
	for i < len(tokens) && tokens[i].Type != TOK_COLON {
		i++
	}
	if i >= len(tokens) || tokens[i].Type != TOK_COLON {
		return i, fmt.Errorf("WHILE: expected COLON after condition at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}
	condTokens := tokens[condStart:i]
	i++ // after COLON

	// Skip NEWLINEs before body
	for i < len(tokens) && tokens[i].Type == TOK_NEWLINE {
		i++
	}

	// Find matching ENDWHILE (token or IDENT)
	bodyStart := i
	endPos := -1
	depth := 1
	for j := i; j < len(tokens); j++ {
		t := tokens[j]

		if t.Type == TOK_WHILE {
			depth++
			continue
		}
		if t.Type == TOK_ENDWHILE || (t.Type == TOK_IDENT && strings.EqualFold(t.Lexeme, "ENDWHILE")) {
			depth--
			if depth == 0 {
				endPos = j
				break
			}
		}
	}
	if endPos == -1 {
		return i, fmt.Errorf("WHILE: unmatched ENDWHILE for WHILE at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}

	// Safety cap
	const maxWhileIterations = 100000
	iterations := 0

	for {
		if iterations >= maxWhileIterations {
			return endPos + 1, fmt.Errorf("WHILE: exceeded %d iterations", maxWhileIterations)
		}
		iterations++

		ok, err := evalBoolExpr(prog, condTokens, sigils)
		if err != nil {
			return endPos + 1, err
		}
		if !ok {
			break
		}

		if err := execBlock(prog, tokens[bodyStart:endPos], sigils); err != nil {
			return endPos + 1, err
		}
	}

	// Resume just after ENDWHILE (and optional '.')
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
	startTok := tokens[i] // TOK_RAISE
	i++                   // after RAISE

	// Skip NEWLINEs
	for i < len(tokens) && tokens[i].Type == TOK_NEWLINE {
		i++
	}

	if i >= len(tokens) {
		return i, fmt.Errorf("RAISE: expected target after RAISE at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}

	// Optional leading '$' before name (tolerated)
	if i < len(tokens) && (tokens[i].Type == TOK_DOLLAR || tokens[i].Type == TOK_SIGIL) {
		i++
	}

	// Name must be IDENT
	if i >= len(tokens) || tokens[i].Type != TOK_IDENT {
		return i, fmt.Errorf("RAISE: expected SIGIL name after RAISE at %s:%d:%d",
			tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	name := tokens[i].Lexeme
	i++

	// Amount expression until DOT/NEWLINE/ENDARCWORK
	exprStart := i
	for i < len(tokens) &&
		tokens[i].Type != TOK_DOT &&
		tokens[i].Type != TOK_NEWLINE &&
		!(tokens[i].Type == TOK_IDENT && lexemeIs(tokens[i], "ENDARCWORK")) {
		i++
	}
	if exprStart == i {
		return i, fmt.Errorf("RAISE: expected amount after SIGIL %s at %s:%d:%d",
			name, startTok.File, startTok.Line, startTok.Column)
	}

	// Evaluate amount using existing expression parser (normalized)
	amtTokens := normalizeExprTokens(tokens[exprStart:i])
	idx := 0
	amtVal, err := parseOr(nil, amtTokens, &idx, sigils)
	if err != nil {
		return i, err
	}

	amt, ok := amtVal.asFloat()
	if !ok {
		return i, fmt.Errorf("RAISE: amount must be numeric for SIGIL %s at %s:%d:%d",
			name, startTok.File, startTok.Line, startTok.Column)
	}

	// Get current value (default 0)
	curStr, _ := getSigil(sigils, name)
	cur := 0.0
	if strings.TrimSpace(curStr) != "" {
		if f, perr := strconv.ParseFloat(strings.TrimSpace(curStr), 64); perr == nil {
			cur = f
		}
	}

	newVal := cur + amt

	// Preserve integer-ness when possible
	if float64(int64(newVal)) == newVal {
		setSigil(sigils, name, fmt.Sprintf("%d", int64(newVal)))
	} else {
		setSigil(sigils, name, fmt.Sprintf("%g", newVal))
	}

	// Optional DOT
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
			return j, fmt.Errorf("CHOIR: only SUMMON statements are allowed inside CHOIR, got %s at %s:%d:%d",
				tok.Type, tok.File, tok.Line, tok.Column)
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

		for j < endPos && tokens[j].Type == TOK_NEWLINE {
			j++
		}
	}

	// No SUMMONs? Skip CHOIR.
	if len(starts) == 0 {
		k := endPos + 1
		if k < len(tokens) && tokens[k].Type == TOK_DOT {
			k++
		}
		return k, nil
	}

	// Run summons in parallel, but with isolated sigils per summon.
	errs := make([]error, len(starts))
	var wg sync.WaitGroup
	wg.Add(len(starts))

	for idx, startIdx := range starts {
		idx := idx
		startIdx := startIdx

		go func() {
			defer wg.Done()

			// Clone sigils for this summon (prevents races)
			child := make(sigilTable)
			for k, v := range sigils {
				child[k] = v
			}

			// Execute the summon statement using the cloned sigils
			_, err := execSummonStmt(prog, tokens, startIdx, child)
			errs[idx] = err
		}()
	}

	wg.Wait()

	// Deterministic error: first in declaration order
	for _, err := range errs {
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

	// Query params: Q_<KEY> (first value)
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

	// Body (best-effort): set REQUEST_BODY and rewind so others can still read it
	child["REQUEST_BODY"] = ""
	if r.Body != nil {
		bodyBytes, err := io.ReadAll(r.Body)
		if err == nil {
			child["REQUEST_BODY"] = string(bodyBytes)
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes)) // rewind
		}
	}
}

// chooseContentType picks the HTTP Content-Type for an ALTAR response.
// Priority:
//  1. SIGIL response_content_type (if set by the WORK or inline route)
//  2. ".json" suffix on the route path -> application/json
//  3. fallback to the provided defaultCT.
func chooseContentType(defaultCT, path string, sigils sigilTable) string {
	if sigils != nil {
		if ct, ok := sigils["response_content_type"]; ok && strings.TrimSpace(ct) != "" {
			return strings.TrimSpace(ct)
		}
	}
	if strings.HasSuffix(path, ".json") {
		return "application/json; charset=utf-8"
	}
	return defaultCT
}

func getResponseStatus(sigils sigilTable) int {
	if sigils == nil {
		return http.StatusOK
	}
	raw := strings.TrimSpace(sigils["response_status"])
	if raw == "" {
		return http.StatusOK
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 100 || n > 599 {
		return http.StatusOK
	}
	return n
}

func applyResponseHeaders(w http.ResponseWriter, sigils sigilTable) {
	if w == nil || sigils == nil {
		return
	}
	for k, v := range sigils {
		if !strings.HasPrefix(k, "response_header_") {
			continue
		}
		hname := strings.TrimPrefix(k, "response_header_")
		hname = strings.ReplaceAll(hname, "_", "-")
		hname = strings.TrimSpace(hname)
		if hname == "" {
			continue
		}
		w.Header().Set(hname, v)
	}
}

func pickResponseBody(defaultBody string, sigils sigilTable) string {
	if sigils == nil {
		return defaultBody
	}
	if b := strings.TrimSpace(sigils["response_body"]); b != "" {
		return b
	}
	return defaultBody
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
			addr:       addr,
			mux:        http.NewServeMux(),
			registered: make(map[string]bool),
		}
	} else if globalAltar.addr != addr {
		prev := globalAltar.addr
		altarMu.Unlock()
		return i, fmt.Errorf("ALTAR: server already bound to %s, cannot bind to %s", prev, addr)
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
			// IMPORTANT: ALTAR does not own process lifetime.
			return i, nil
		}

		if tok.Type != TOK_ROUTE {
			return i, fmt.Errorf("ALTAR: expected ROUTE or ENDALTAR, got %s at %s:%d:%d",
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
			return i, fmt.Errorf("ALTAR: missing path after method %s at %s:%d:%d",
				method, tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
		}
		path := tokens[i].Lexeme
		i++

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
				return i, fmt.Errorf("ALTAR: expected WORK name after WORK at %s:%d:%d",
					tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
			}
			handlerName := tokens[i].Lexeme
			i++

			// Optional DOT
			if i < len(tokens) && tokens[i].Type == TOK_DOT {
				i++
			}

			fmt.Printf("[SIC ALTAR ROUTE] Route %s %s -> WORK %s\n", method, path, handlerName)

			altarMu.Lock()

			routeKey := method + " " + path
			if srv.registered[routeKey] {
				altarMu.Unlock()
				return i, fmt.Errorf("ALTAR: duplicate route %s", routeKey)
			}
			srv.registered[routeKey] = true

			m := method
			p := path
			h := handlerName
			parent := sigils
			mux := srv.mux

			mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != m {
					http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
					return
				}

				work := findWork(prog, h)
				if work == nil {
					http.Error(w, "handler not found", http.StatusNotFound)
					return
				}

				// Clone sigils (request-local state)
				child := make(sigilTable)
				cloneVisibleSigils(child, parent)

				// Inject request details into SIGILs
				injectRequestSigils(child, r)

				// Capture the ritual's answer as HTTP body
				body, err := execWork(prog, work, child, true)
				if err != nil {
					http.Error(w, "internal error", http.StatusInternalServerError)
					return
				}
				if body == "" {
					body = "OK"
				}

				// Allow overrides via sigils
				body = pickResponseBody(body, child)

				// Apply headers + content type
				applyResponseHeaders(w, child)
				ct := chooseContentType("text/plain; charset=utf-8", p, child)
				w.Header().Set("Content-Type", ct)

				// Status
				status := getResponseStatus(child)
				w.WriteHeader(status)

				_, _ = w.Write([]byte(body + "\n"))
			})

			altarMu.Unlock()
			continue
		}

		// ----------------------------------------------------
		// 2) ROUTE ... TO SEND BACK <expr>.
		// ----------------------------------------------------
		if tokens[i].Type == TOK_SEND {
			i++ // SEND

			// Allow blank lines between SEND and BACK/expression
			for i < len(tokens) && tokens[i].Type == TOK_NEWLINE {
				i++
			}

			// Optional BACK keyword (by lexeme)
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

			fmt.Printf("[SIC ALTAR ROUTE] Route %s %s -> inline SEND BACK\n", method, path)

			altarMu.Lock()

			routeKey := method + " " + path
			if srv.registered[routeKey] {
				altarMu.Unlock()
				return i, fmt.Errorf("ALTAR: duplicate route %s", routeKey)
			}
			srv.registered[routeKey] = true

			m := method
			p := path
			exprCopy := exprTokens
			parent := sigils
			mux := srv.mux

			mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != m {
					http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
					return
				}

				// Clone sigils (request-local state)
				child := make(sigilTable)
				cloneVisibleSigils(child, parent)

				// Inject request details into SIGILs
				injectRequestSigils(child, r)

				val, err := evalStringExpr(prog, exprCopy, child)
				if err != nil {
					http.Error(w, "internal error", http.StatusInternalServerError)
					return
				}
				if val == "" {
					val = "OK"
				}

				// Allow overrides via sigils
				val = pickResponseBody(val, child)

				applyResponseHeaders(w, child)

				ct := chooseContentType("text/plain; charset=utf-8", p, child)
				w.Header().Set("Content-Type", ct)

				status := getResponseStatus(child)
				w.WriteHeader(status)

				_, _ = w.Write([]byte(val + "\n"))
			})

			altarMu.Unlock()
			continue
		}

		// If token after TO was neither WORK nor SEND
		return i, fmt.Errorf("ALTAR: expected WORK or SEND after TO at %s:%d:%d",
			tokens[i].File, tokens[i].Line, tokens[i].Column)
	}

	return i, fmt.Errorf("ALTAR: missing ENDALTAR for block starting at %s:%d:%d",
		startTok.File, startTok.Line, startTok.Column)
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
	argFromSigil := ""       // if the arg was IDENT, track which sigil name
	argWasInvisible := false // if argFromSigil was invisible, propagate invisibility to callee param

	// Optional: WITH SIGIL <arg>
	if i < len(tokens) && tokens[i].Type == TOK_WITH {
		i++
		if i < len(tokens) && tokens[i].Type == TOK_SIGIL {
			i++
		}

		if i >= len(tokens) {
			return "", 0, fmt.Errorf("SUMMON: missing argument after WITH")
		}

		switch tokens[i].Type {
		case TOK_STRING:
			argVal = tokens[i].Lexeme
			i++

		case TOK_IDENT:
			// Treat as sigil name (explicit reference = intentional)
			argFromSigil = tokens[i].Lexeme
			argVal, _ = getSigil(sigils, argFromSigil)
			argWasInvisible = isInvisibleSigil(sigils, argFromSigil)
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

	// Build child environment:
	// - inherit only VISIBLE sigils by default
	// - set the first param to argVal if present
	childSigils := make(sigilTable)

	// Copy only visible sigils (and skip all meta keys)
	cloneVisibleSigils(childSigils, sigils)

	// If the callee expects a first parameter, bind it.
	if len(target.SigilParams) > 0 {
		param := target.SigilParams[0]
		childSigils[param] = argVal

		// If the caller explicitly referenced an invisible sigil as the argument,
		// that is an intentional reveal/copy into the callee param — keep it invisible there too.
		if argFromSigil != "" && argWasInvisible {
			markInvisibleSigil(childSigils, param)
		}
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
