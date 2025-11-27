package compiler

import "fmt"

// -------- AST TYPES --------

type Program struct {
	Language string
	Scroll   string
	Mode     string
	Profile  string
	Works    []*WorkDecl
}

type WorkDecl struct {
	Name  string
	Start Token   // where WORK was seen
	Body  []Token // raw tokens between WORK and ENDWORK (for now)
}

// -------- PARSER CORE --------

type Parser struct {
	l         *Lexer
	curToken  Token
	peekToken Token
	errors    []string
}

func NewParser(l *Lexer) *Parser {
	p := &Parser{l: l}
	// Load two tokens so cur/peek are valid.
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

// -------- TOP-LEVEL PARSE --------

func (p *Parser) ParseProgram() *Program {
	prog := &Program{}

	for p.curToken.Type != TOK_EOF {
		switch p.curToken.Type {
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
			// Just advance over things we don't care about yet (DOT, NEWLINE, etc.).
		}
		p.nextToken()
	}

	return prog
}

// -------- HEADER HELPERS --------

func (p *Parser) parseLanguage(prog *Program) {
	// LANGUAGE "SIC 1.0".
	p.nextToken()
	if p.curToken.Type == TOK_STRING {
		prog.Language = p.curToken.Lexeme
	} else {
		p.addError("expected STRING after LANGUAGE, got %s", p.curToken.Type)
	}
}

func (p *Parser) parseScroll(prog *Program) {
	// SCROLL STRONG hello_scroll
	p.nextToken() // maybe STRONG / SOFT (ignore for now)
	if p.curToken.Type == TOK_IDENT {
		// could be mode (STRONG) OR scroll name if no strength keyword exists
		// look ahead one to see if there's another IDENT
		if p.peekToken.Type == TOK_IDENT {
			// STRONG hello_scroll -> take second ident as name
			p.nextToken()
			prog.Scroll = p.curToken.Lexeme
		} else {
			// SCROLL hello_scroll
			prog.Scroll = p.curToken.Lexeme
		}
	} else {
		p.addError("expected IDENT after SCROLL, got %s", p.curToken.Type)
	}
}

func (p *Parser) parseMode(prog *Program) {
	// MODE CHANT
	p.nextToken()
	if p.curToken.Type == TOK_IDENT {
		prog.Mode = p.curToken.Lexeme
	} else {
		p.addError("expected IDENT after MODE, got %s", p.curToken.Type)
	}
}

func (p *Parser) parseProfile(prog *Program) {
	// PROFILE "CIVIL"
	p.nextToken()
	if p.curToken.Type == TOK_STRING || p.curToken.Type == TOK_IDENT {
		prog.Profile = p.curToken.Lexeme
	} else {
		p.addError("expected STRING/IDENT after PROFILE, got %s", p.curToken.Type)
	}
}

// -------- WORK PARSING --------

func (p *Parser) parseWork() *WorkDecl {
	// WORK MAIN WITH SIGIL UNUSED AS TEXT:
	w := &WorkDecl{
		Start: p.curToken,
	}

	// Next token should be the work name.
	p.nextToken()
	if p.curToken.Type != TOK_IDENT {
		p.addError("expected IDENT after WORK, got %s", p.curToken.Type)
		return nil
	}
	w.Name = p.curToken.Lexeme

	// Now collect everything until ENDWORK into Body.
	for {
		p.nextToken()
		if p.curToken.Type == TOK_EOF || p.curToken.Type == TOK_ENDWORK {
			break
		}
		w.Body = append(w.Body, p.curToken)
	}

	return w
}
