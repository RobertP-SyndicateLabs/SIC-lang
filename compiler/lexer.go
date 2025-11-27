package compiler

import (
    "fmt"
    "unicode"
    "unicode/utf8"
)

/*
   SIC Lexer v0.9

   - Supports:
     * Keywords (LANGUAGE, SCROLL, WORK, MODE, PROFILE, USING, ALTAR, ROUTE, GET, POST, PUT, DELETE, WITH, HANDLER, SIGIL, AS, TEXT, EPHEMERAL, CHAMBER, ENDCHAMBER, THUS, WE, ANSWER, ENDWORK, ENDALTAR, IF, ELSE, END, RAISE, OMEN, SUMMON, SERVICE, LOG, PORT)
     * Identifiers
     * String literals: "like this"
     * Numbers (simple integers)
     * Punctuation: . : , / ( ) { } = + - * > < !
     * Comments: // to end of line
     * Newline tracking

   - API:
     * NewLexer(source, filename) *Lexer
     * (*Lexer).NextToken() Token
*/

func (t Token) String() string {
    return fmt.Sprintf("%s(%q) at %s:%d:%d", t.Type, t.Lexeme, t.File, t.Line, t.Column)
}

type Lexer struct {
    src      string
    filename string

    pos      int  // byte index into src
    line     int
    column   int

    ch       rune // current rune
    width    int  // width in bytes of ch
    done     bool
}

func NewLexer(src, filename string) *Lexer {
    l := &Lexer{
        src:      src,
        filename: filename,
        line:     1,
        column:   0,
    }
    l.readRune()
    return l
}

func (l *Lexer) readRune() {
    if l.pos >= len(l.src) {
        l.ch = 0
        l.width = 0
        l.done = true
        return
    }

    r, w := utf8.DecodeRuneInString(l.src[l.pos:])
    l.ch = r
    l.width = w
    l.pos += w
    if r == '\n' {
        l.line++
        l.column = 0
    } else {
        l.column++
    }
}

func (l *Lexer) peekRune() rune {
    if l.pos >= len(l.src) {
        return 0
    }
    r, _ := utf8.DecodeRuneInString(l.src[l.pos:])
    return r
}

func (l *Lexer) makeToken(tt TokenType, lexeme string, line, col int) Token {
    return Token{
        Type:   tt,
        Lexeme: lexeme,
        Line:   line,
        Column: col,
        File:   l.filename,
    }
}

// NextToken returns the next token from the input.
func (l *Lexer) NextToken() Token {
    // Skip whitespace but keep NEWLINE as its own token
    for {
        if l.done {
            return l.makeToken(TOK_EOF, "", l.line, l.column)
        }

        if l.ch == ' ' || l.ch == '\t' || l.ch == '\r' {
            l.readRune()
            continue
        }

        // Comments: // to end of line
        if l.ch == '/' && l.peekRune() == '/' {
            l.skipLineComment()
            continue
        }

        break
    }

    // Newline token
    if l.ch == '\n' {
        line, col := l.line, l.column
        l.readRune()
        return l.makeToken(TOK_NEWLINE, "\n", line, col)
    }

    line, col := l.line, l.column

    // EOF
    if l.done || l.ch == 0 {
        return l.makeToken(TOK_EOF, "", line, col)
    }

    // Strings
    if l.ch == '"' {
        return l.lexString()
    }

    // Identifiers / keywords
    if isLetter(l.ch) {
        return l.lexIdentOrKeyword()
    }

    // Numbers (simple ints)
    if unicode.IsDigit(l.ch) {
        return l.lexNumber()
    }

    // Single-character tokens / operators
    ch := l.ch
    l.readRune()

    switch ch {
    case '.':
        return l.makeToken(TOK_DOT, ".", line, col)
    case ':':
        return l.makeToken(TOK_COLON, ":", line, col)
    case ',':
        return l.makeToken(TOK_COMMA, ",", line, col)
    case '/':
        return l.makeToken(TOK_SLASH, "/", line, col)
    case '(':
        return l.makeToken(TOK_LPAREN, "(", line, col)
    case ')':
        return l.makeToken(TOK_RPAREN, ")", line, col)
    case '{':
        return l.makeToken(TOK_LBRACE, "{", line, col)
    case '}':
        return l.makeToken(TOK_RBRACE, "}", line, col)
    case '=':
        return l.makeToken(TOK_EQUAL, "=", line, col)
    case '+':
        return l.makeToken(TOK_PLUS, "+", line, col)
    case '-':
        return l.makeToken(TOK_MINUS, "-", line, col)
    case '*':
        return l.makeToken(TOK_STAR, "*", line, col)
    case '!':
        return l.makeToken(TOK_BANG, "!", line, col)
    case '<':
        return l.makeToken(TOK_LT, "<", line, col)
    case '>':
        return l.makeToken(TOK_GT, ">", line, col)
    default:
        // Anything else is illegal for now
        return l.makeToken(TOK_ILLEGAL, string(ch), line, col)
    }
}

func (l *Lexer) skipLineComment() {
    // We are at first '/', peek confirmed second '/'
    for !l.done && l.ch != '\n' {
        l.readRune()
    }
}

func (l *Lexer) lexString() Token {
    // We are at the opening quote "
    line, col := l.line, l.column
    l.readRune() // consume opening quote

    startPos := l.pos - l.width
    var out []rune

    for !l.done && l.ch != '"' && l.ch != '\n' {
        if l.ch == '\\' {
            // Handle a few simple escapes
            l.readRune()
            if l.done {
                break
            }
            switch l.ch {
            case 'n':
                out = append(out, '\n')
            case 't':
                out = append(out, '\t')
            case '"':
                out = append(out, '"')
            case '\\':
                out = append(out, '\\')
            default:
                // Unknown escape, keep literal
                out = append(out, '\\', l.ch)
            }
            l.readRune()
            continue
        }

        out = append(out, l.ch)
        l.readRune()
    }

    if l.ch == '"' {
        // consume closing quote
        l.readRune()
        return l.makeToken(TOK_STRING, string(out), line, col)
    }

    // Unterminated string
    return l.makeToken(TOK_ILLEGAL, l.src[startPos:l.pos], line, col)
}

func (l *Lexer) lexNumber() Token {
    line, col := l.line, l.column
    start := l.pos - l.width

    for !l.done && unicode.IsDigit(l.ch) {
        l.readRune()
    }

    lex := l.src[start : l.pos-l.width]
    return l.makeToken(TOK_NUMBER, lex, line, col)
}

func (l *Lexer) lexIdentOrKeyword() Token {
    line, col := l.line, l.column
    start := l.pos - l.width

    for !l.done && (isLetter(l.ch) || unicode.IsDigit(l.ch) || l.ch == '_') {
        l.readRune()
    }

    lex := l.src[start : l.pos-l.width]
    upper := toUpperASCII(lex)

    if tt, ok := keywords[upper]; ok {
        return l.makeToken(tt, upper, line, col)
    }

    return l.makeToken(TOK_IDENT, lex, line, col)
}

func isLetter(r rune) bool {
    return unicode.IsLetter(r) || r == '_' // allow _ in identifiers
}

func toUpperASCII(s string) string {
    // SIC keywords are ASCII uppercase; CHANT often uses uppercase.
    // We normalize identifiers to case-sensitive, but compare uppercased for keywords.
    out := make([]byte, len(s))
    for i := 0; i < len(s); i++ {
        c := s[i]
        if 'a' <= c && c <= 'z' {
            out[i] = c - 32
        } else {
            out[i] = c
        }
    }
    return string(out)
}

var keywords = map[string]TokenType{
    "LANGUAGE":    TOK_LANGUAGE,
    "SCROLL":      TOK_SCROLL,
    "WORK":        TOK_WORK,
    "MODE":        TOK_MODE,
    "PROFILE":     TOK_PROFILE,
    "USING":       TOK_USING,

    "ALTAR":       TOK_ALTAR,
    "ROUTE":       TOK_ROUTE,
    "PORT":        TOK_PORT,
    "GET":         TOK_GET,
    "POST":        TOK_POST,
    "PUT":         TOK_PUT,
    "DELETE":      TOK_DELETE,
    "HANDLER":     TOK_HANDLER,

    "SIGIL":       TOK_SIGIL,
    "AS":          TOK_AS,
    "TEXT":        TOK_TEXT,
    "EPHEMERAL":   TOK_EPHEMERAL,
    "CHAMBER":     TOK_CHAMBER,
    "ENDCHAMBER":  TOK_ENDCHAMBER,

    "THUS":        TOK_THUS,
    "WE":          TOK_WE,
    "ANSWER":      TOK_ANSWER,
    "ENDWORK":     TOK_ENDWORK,
    "ENDALTAR":    TOK_ENDALTAR,

    "IF":          TOK_IF,
    "ELSE":        TOK_ELSE,
    "END":         TOK_END,
    "RAISE":       TOK_RAISE,
    "OMEN":        TOK_OMEN,

    "SUMMON":      TOK_SUMMON,
    "SERVICE":     TOK_SERVICE,
    "LOG":         TOK_LOG,
}
