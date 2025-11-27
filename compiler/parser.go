package compiler

import "fmt"

type Parser struct {
    l         *Lexer
    curToken  Token
    peekToken Token
    errors    []string
}

func NewParser(l *Lexer) *Parser {
    p := &Parser{
        l:      l,
        errors: []string{},
    }

    // Load first two tokens
    p.nextToken()
    p.nextToken()
    return p
}

func (p *Parser) nextToken() {
    p.curToken = p.peekToken
    p.peekToken = p.l.NextToken()
}

func (p *Parser) Errors() []string {
    return p.errors
}

// A minimal parsed program holding SIC declarations
type Program struct {
    Language string
    Scroll   string
    Mode     string
    Profile  string
    Work     string
}

func (p *Parser) ParseProgram() *Program {
    prog := &Program{}

    for p.curToken.Type != TOK_EOF {
        switch p.curToken.Type {

        case TOK_LANGUAGE:
            p.nextToken()
            if p.curToken.Type == TOK_STRING {
                prog.Language = p.curToken.Lexeme
            }
        case TOK_SCROLL:
            p.nextToken()
            if p.curToken.Type == TOK_IDENT {
                prog.Scroll = p.curToken.Lexeme
            }
        case TOK_MODE:
            p.nextToken()
            if p.curToken.Type == TOK_IDENT {
                prog.Mode = p.curToken.Lexeme
            }
        case TOK_PROFILE:
            p.nextToken()
            if p.curToken.Type == TOK_STRING {
                prog.Profile = p.curToken.Lexeme
            }
        case TOK_WORK:
            p.nextToken()
            if p.curToken.Type == TOK_IDENT {
                prog.Work = p.curToken.Lexeme
            }
        }

        p.nextToken()
    }

    return prog
}

func (p *Parser) PrintProgram(prg *Program) {
    fmt.Println("== SIC PROGRAM ==")
    fmt.Println("Language:", prg.Language)
    fmt.Println("Scroll:", prg.Scroll)
    fmt.Println("Mode:", prg.Mode)
    fmt.Println("Profile:", prg.Profile)
    fmt.Println("Work:", prg.Work)
}
