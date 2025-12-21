package compiler

import (
	"fmt"
	"strings"
)

// ===== AST TYPES =====

type Program struct {
	Language string
	Scroll   string
	Mode     string
	Profile  string
	Works    []*WorkDecl
}

// WorkDecl represents a WORK block.
//
// Example headers:
//
//	WORK MAIN WITH SIGIL UNUSED AS TEXT:
//	WORK GREETING WITH SIGIL name AS TEXT:
//
// We currently only care about:
//   - Name
//   - Any SIGIL parameter names in the header (e.g. "name")
type WorkDecl struct {
	Name        string
	Start       Token
	Body        []Token
	SigilParams []string // names of SIGIL parameters in header, in order
	Ephemeral   bool     // true if declared as WORK EPHEMERAL
	Sealed      bool
	SealToken   string
}

// ===== PARSER CORE =====

type Parser struct {
	l         *Lexer
	curToken  Token
	peekToken Token
	errors    []string
}

func NewParser(l *Lexer) *Parser {
	p := &Parser{l: l}
	// prime cur/peek
	p.nextToken()
	p.nextToken()
	return p
}

func (p *Parser) Errors() []string {
	return p.errors
}

func (p *Parser) addError(msg string, args ...interface{}) {
	p.errors = append(p.errors, fmt.Sprintf(msg, args...))
}

func (p *Parser) nextToken() {
	p.curToken = p.peekToken
	p.peekToken = p.l.NextToken()
}

func (p *Parser) skipNewlines() {
	for p.curToken.Type == TOK_NEWLINE {
		p.nextToken()
	}
}

// ===== TOP-LEVEL PARSE =====

func (p *Parser) ParseProgram() *Program {
	prog := &Program{}

	for p.curToken.Type != TOK_EOF {
		switch p.curToken.Type {
		case TOK_NEWLINE:
			p.nextToken()
			continue

		case TOK_LANGUAGE:
			p.parseLanguage(prog)

		case TOK_SCROLL:
			p.parseScroll(prog)

		case TOK_MODE:
			p.parseMode(prog)

		case TOK_PROFILE:
			p.parseProfile(prog)

		case TOK_WORK:
			w := p.parseWork()
			if w != nil {
				prog.Works = append(prog.Works, w)
			}

		default:
			// Unknown / not-yet-handled token at top level:
			// just advance to avoid infinite loop.
		}

		p.nextToken()
	}

	return prog
}

// ===== HEADER HELPERS =====

// LANGUAGE "SIC 1.0".
func (p *Parser) parseLanguage(prog *Program) {
	// curToken is TOK_LANGUAGE
	p.nextToken()
	if p.curToken.Type == TOK_STRING {
		prog.Language = p.curToken.Lexeme
	} else {
		p.addError("expected STRING after LANGUAGE, got %s", p.curToken.Type)
	}
	// optional trailing DOT is ignored by parser; lexer already emitted it.
}

// SCROLL STRONG hello_scroll
// SCROLL hello_scroll
func (p *Parser) parseScroll(prog *Program) {
	// curToken is TOK_SCROLL
	p.nextToken() // move to possible strength or name

	if p.curToken.Type != TOK_IDENT {
		p.addError("expected IDENT after SCROLL, got %s", p.curToken.Type)
		return
	}

	// If we see SCROLL STRONG/WEAK foo, treat the *second* IDENT as the name.
	if p.peekToken.Type == TOK_IDENT {
		// strength (ignored for now)
		p.nextToken() // now at scroll name
		prog.Scroll = p.curToken.Lexeme
	} else {
		// SCROLL foo
		prog.Scroll = p.curToken.Lexeme
	}
}

// MODE CHANT.
func (p *Parser) parseMode(prog *Program) {
	p.nextToken()
	if p.curToken.Type == TOK_IDENT {
		prog.Mode = p.curToken.Lexeme
	} else {
		p.addError("expected IDENT after MODE, got %s", p.curToken.Type)
	}
}

// PROFILE "CIVIL"
func (p *Parser) parseProfile(prog *Program) {
	p.nextToken()
	if p.curToken.Type == TOK_STRING || p.curToken.Type == TOK_IDENT {
		prog.Profile = p.curToken.Lexeme
	} else {
		p.addError("expected STRING/IDENT after PROFILE, got %s", p.curToken.Type)
	}
}

// ===== WORK PARSING =====
//
// Handles both:
//
//   WORK MAIN WITH SIGIL UNUSED AS TEXT:
//   WORK GREETING WITH SIGIL name AS TEXT:
//
// We scan the header until the COLON. Any "SIGIL <ident>" pair in the header
// is recorded as a sigil parameter name, *except* that we don't special-case
// "UNUSED" here (it just becomes a param name, which is harmless for MAIN).

// Accept anything that can act as a SIGIL parameter name in a WORK header
func isSigilNameToken(t Token) bool {
	switch t.Type {
	case TOK_IDENT:
		return true
	case TOK_STRING:
		return true
	case TOK_UNUSED:
		return true
	default:
		return false
	}
}

func (p *Parser) parseWork() *WorkDecl {
	w := &WorkDecl{
		Start: p.curToken,
	}

	// Move to the token after WORK.
	p.nextToken()

	// Optional modifiers in any order:
	//   WORK [EPHEMERAL] [SEALED] Name ...
	//   WORK [SEALED] [EPHEMERAL] Name ...
	for {
		switch p.curToken.Type {
		case TOK_EPHEMERAL:
			w.Ephemeral = true
			p.nextToken()
			continue

		case TOK_SEALED:
			w.Sealed = true
			p.nextToken()
			continue
		}
		break
	}

	// Expect the work name.
	if p.curToken.Type != TOK_IDENT {
		p.addError("expected IDENT after WORK, got %s", p.curToken.Type)
		return nil
	}
	w.Name = p.curToken.Lexeme

	// Scan header until COLON, capturing:
	// - SIGIL params
	// - optional SEAL <token>
	for {
		p.nextToken()

		switch p.curToken.Type {
		case TOK_COLON:
			goto bodyStart

		case TOK_EOF:
			p.addError("unexpected EOF in WORK header for %s", w.Name)
			return nil

		case TOK_ENDWORK:
			p.addError("unexpected ENDWORK in WORK header for %s", w.Name)
			return nil

		case TOK_SIGIL:
			p.nextToken()
			if !isSigilNameToken(p.curToken) {
				p.addError("expected SIGIL name after SIGIL in WORK header for %s, got %s",
					w.Name, p.curToken.Type)
				return nil
			}
			w.SigilParams = append(w.SigilParams, p.curToken.Lexeme)

		case TOK_SEAL:
			// Header seal token: SEAL "vault_key"  or  SEAL someIdent
			p.nextToken()
			if !isSigilNameToken(p.curToken) {
				p.addError("expected SEAL token after SEAL in WORK header for %s, got %s",
					w.Name, p.curToken.Type)
				return nil
			}
			w.SealToken = p.curToken.Lexeme

		default:
			// Ignore other header tokens (WITH, AS, TEXT, etc.)
		}
	}

bodyStart:
	// If declared SEALED, require SEAL token in header
	if w.Sealed && strings.TrimSpace(w.SealToken) == "" {
		p.addError("WORK %s declared SEALED but no SEAL token provided in header", w.Name)
		return nil
	}

	// Move to first body token.
	p.nextToken()

	// Collect body tokens until ENDWORK or EOF.
	for p.curToken.Type != TOK_EOF && p.curToken.Type != TOK_ENDWORK {
		w.Body = append(w.Body, p.curToken)
		p.nextToken()
	}

	return w
}
