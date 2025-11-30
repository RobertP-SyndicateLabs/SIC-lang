package compiler

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

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

		case TOK_WHILE:
			// WHILE condition ... ENDWHILE.
			next, err := execWhile(prog, tokens, i, sigils)
			if err != nil {
				return "", err
			}
			i = next
			continue

		case TOK_ALTAR:
			// ALTAR ... ENDALTAR.
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

// ---------------- SEND BACK ----------------

// SEND BACK SIGIL name.
// SEND BACK <expr>.   (fallback form)
func execSendBack(prog *Program, tokens []Token, i int, sigils sigilTable) (string, int, error) {
	startTok := tokens[i] // "SEND"
	i++

	// Expect IDENT "BACK"
	if i >= len(tokens) || tokens[i].Type != TOK_IDENT || tokens[i].Lexeme != "BACK" {
		return "", i, fmt.Errorf("SEND BACK: expected BACK after SEND at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}
	i++

	// Special-case: SEND BACK SIGIL name.
	if i < len(tokens) && tokens[i].Type == TOK_SIGIL {
		i++
		if i >= len(tokens) || tokens[i].Type != TOK_IDENT {
			return "", i, fmt.Errorf("SEND BACK: expected SIGIL name after SIGIL at %s:%d:%d",
				tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
		}
		name := tokens[i].Lexeme
		val, _ := getSigil(sigils, name)
		i++
		// skip until DOT / NEWLINE / ENDWORK
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

	// Fallback: SEND BACK <expr>.
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

// ---------------- ALTAR / ROUTE Canticle ----------------
//
// ALTAR my_server AT PORT 15080:
//     ROUTE GET "/hello" WITH HANDLER HELLO.
// ENDALTAR.
//
// For now, ALTAR is a semantic stub: we parse and log the altar + routes,
// but we don't actually spin up an HTTP server (keeps Termux / mobile happy).

func execAltarBlock(prog *Program, tokens []Token, i int, sigils sigilTable) (int, error) {
	startTok := tokens[i] // TOK_ALTAR
	i++

	// Optional altar name (IDENT)
	altarName := "ALTAR"
	if i < len(tokens) && tokens[i].Type == TOK_IDENT {
		altarName = tokens[i].Lexeme
		i++
	}

	// Expect AT PORT <num>
	if i >= len(tokens) || tokens[i].Type != TOK_AT {
		return i, fmt.Errorf("ALTAR: expected AT after ALTAR at %s:%d:%d",
			startTok.File, startTok.Line, startTok.Column)
	}
	i++

	if i >= len(tokens) || tokens[i].Type != TOK_PORT {
		return i, fmt.Errorf("ALTAR: expected PORT after AT at %s:%d:%d",
			tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	i++

	if i >= len(tokens) || tokens[i].Type != TOK_NUM {
		return i, fmt.Errorf("ALTAR: expected port number after PORT at %s:%d:%d",
			tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	port := tokens[i].Lexeme
	i++

	// Expect COLON
	if i >= len(tokens) || tokens[i].Type != TOK_COLON {
		return i, fmt.Errorf("ALTAR: expected COLON after PORT %s at %s:%d:%d",
			port, tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
	}
	i++ // after COLON

	// Collect ROUTE lines until ENDALTAR. For now we just log them.
	type routeSpec struct {
		method  string
		path    string
		handler string
	}

	var routes []routeSpec

	for i < len(tokens) {
		tok := tokens[i]

		if tok.Type == TOK_NEWLINE {
			i++
			continue
		}

		if tok.Type == TOK_ENDALTAR {
			i++
			// Optional trailing DOT
			if i < len(tokens) && tokens[i].Type == TOK_DOT {
				i++
			}
			break
		}

		if tok.Type == TOK_ROUTE {
			// ROUTE GET "/path" WITH HANDLER NAME.
			j := i + 1

			if j >= len(tokens) {
				return j, fmt.Errorf("ROUTE: unexpected EOF after ROUTE at %s:%d:%d",
					tok.File, tok.Line, tok.Column)
			}

			methodTok := tokens[j]
			if methodTok.Type != TOK_GET &&
				methodTok.Type != TOK_POST &&
				methodTok.Type != TOK_PUT &&
				methodTok.Type != TOK_DELETE {
				return j, fmt.Errorf("ROUTE: expected HTTP method after ROUTE at %s:%d:%d",
					methodTok.File, methodTok.Line, methodTok.Column)
			}
			method := methodTok.Lexeme
			j++

			if j >= len(tokens) || tokens[j].Type != TOK_STRING {
				return j, fmt.Errorf("ROUTE: expected path string after method at %s:%d:%d",
					tokens[j-1].File, tokens[j-1].Line, tokens[j-1].Column)
			}
			path := tokens[j].Lexeme
			j++

			if j >= len(tokens) || tokens[j].Type != TOK_WITH {
				return j, fmt.Errorf("ROUTE: expected WITH after path at %s:%d:%d",
					tokens[j-1].File, tokens[j-1].Line, tokens[j-1].Column)
			}
			j++

			if j >= len(tokens) || tokens[j].Type != TOK_HANDLER {
				return j, fmt.Errorf("ROUTE: expected HANDLER after WITH at %s:%d:%d",
					tokens[j-1].File, tokens[j-1].Line, tokens[j-1].Column)
			}
			j++

			if j >= len(tokens) || tokens[j].Type != TOK_IDENT {
				return j, fmt.Errorf("ROUTE: expected handler name after HANDLER at %s:%d:%d",
					tokens[j-1].File, tokens[j-1].Line, tokens[j-1].Column)
			}
			handler := tokens[j].Lexeme
			j++

			// Skip to end of line / DOT
			for j < len(tokens) && tokens[j].Type != TOK_DOT && tokens[j].Type != TOK_NEWLINE {
				j++
			}
			if j < len(tokens) && tokens[j].Type == TOK_DOT {
				j++
			}

			routes = append(routes, routeSpec{
				method:  method,
				path:    path,
				handler: handler,
			})

			i = j
			continue
		}

		return i, fmt.Errorf("ALTAR: unexpected token %s at %s:%d:%d",
			tok.Type, tok.File, tok.Line, tok.Column)
	}

	// For now, just log what we *would* raise.
	fmt.Printf("[SIC ALTAR] ALTAR %s at :%s (stubbed; no HTTP server started).\n", altarName, port)
	for _, r := range routes {
		fmt.Printf("[SIC ALTAR] ROUTE %s %s -> handler %s\n", r.method, r.path, r.handler)
	}

	return i, nil
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

// Very small expression engine for now:
// - STRING literals
// - SIGIL name references (SIGIL name OR bare name if bound)
// - Concatenation with PLUS
// - Embedded SUMMON WORK ... WITH SIGIL ...
func evalStringExpr(prog *Program, tokens []Token, sigils sigilTable) (string, error) {
	if len(tokens) == 0 {
		return "", nil
	}

	out := ""
	i := 0
	expectValue := true

	for i < len(tokens) {
		tok := tokens[i]

		if expectValue {
			switch tok.Type {
			case TOK_STRING:
				out += tok.Lexeme
				i++

			case TOK_SIGIL:
				i++
				if i >= len(tokens) || tokens[i].Type != TOK_IDENT {
					return "", fmt.Errorf("expected SIGIL name after SIGIL at %s:%d:%d",
						tokens[i-1].File, tokens[i-1].Line, tokens[i-1].Column)
				}
				name := tokens[i].Lexeme
				val, _ := getSigil(sigils, name)
				out += val
				i++

			case TOK_IDENT:
				// Treat IDENT as a sigil reference if it exists in the table
				if val, ok := sigils[tok.Lexeme]; ok {
					out += val
					i++
				} else {
					return "", fmt.Errorf("unexpected IDENT in expr: %s at %s:%d:%d",
						tok.Lexeme, tok.File, tok.Line, tok.Column)
				}

			case TOK_SUMMON:
				val, consumed, err := evalSummonExpr(prog, tokens, i, sigils)
				if err != nil {
					return "", err
				}
				out += val
				i += consumed

			default:
				return "", fmt.Errorf("unexpected token in expr: %s at %s:%d:%d",
					tok.Type, tok.File, tok.Line, tok.Column)
			}
			expectValue = false
		} else {
			// Expect PLUS
			if tok.Type != TOK_PLUS {
				return "", fmt.Errorf("expected '+' in expression, got %s at %s:%d:%d",
					tok.Type, tok.File, tok.Line, tok.Column)
			}
			i++
			expectValue = true
		}
	}

	if expectValue {
		return "", fmt.Errorf("incomplete expression, trailing '+'")
	}

	return out, nil
}

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
