package compiler

type TokenType string

const (
	// Meta / control
	TOK_ILLEGAL TokenType = "ILLEGAL"
	TOK_EOF     TokenType = "EOF"
	TOK_NEWLINE TokenType = "NEWLINE"
	TOK_DOT     TokenType = "."

	// Identifiers & literals
	TOK_IDENT  TokenType = "IDENT"
	TOK_STRING TokenType = "STRING"
	// Internal name for numeric literals; surface-level math is ARCWORK.
	TOK_NUM TokenType = "NUM"

	// Top-level declarations
	TOK_LANGUAGE TokenType = "LANGUAGE"
	TOK_SCROLL   TokenType = "SCROLL"
	TOK_MODE     TokenType = "MODE"
	TOK_PROFILE  TokenType = "PROFILE"
	TOK_USING    TokenType = "USING"

	// Works / sigils / text
	TOK_WORK    TokenType = "WORK"
	TOK_SIGIL   TokenType = "SIGIL"
	TOK_TEXT    TokenType = "TEXT"
	TOK_AS      TokenType = "AS"
	TOK_UNUSED  TokenType = "UNUSED"
	TOK_THUS    TokenType = "THUS"
	TOK_WE      TokenType = "WE"
	TOK_ANSWER  TokenType = "ANSWER"
	TOK_WITH    TokenType = "WITH"
	TOK_ENDWORK TokenType = "ENDWORK"

	// Speaking / binding / computation
	TOK_SAY      TokenType = "SAY"
	TOK_LET      TokenType = "LET"
	TOK_BE       TokenType = "BE"
	TOK_FROM     TokenType = "FROM"
	TOK_WEAVE    TokenType = "WEAVE"
	TOK_ENDWEAVE TokenType = "ENDWEAVE"
	TOK_AT       TokenType = "AT"
	TOK_LEVEL    TokenType = "LEVEL"

	// ARCWORK: kernel math / state updates
	TOK_ARCWORK TokenType = "ARCWORK"

	// ALTAR / HTTP-ish Canticle
	TOK_ALTAR    TokenType = "ALTAR"
	TOK_ENDALTAR TokenType = "ENDALTAR"
	TOK_PORT     TokenType = "PORT"
	TOK_ROUTE    TokenType = "ROUTE"
	TOK_GET      TokenType = "GET"
	TOK_POST     TokenType = "POST"
	TOK_PUT      TokenType = "PUT"
	TOK_DELETE   TokenType = "DELETE"
	TOK_HANDLER  TokenType = "HANDLER"
	TOK_SERVICE  TokenType = "SERVICE"

	TOK_SEND TokenType = "SEND" // SEND
	TOK_BACK TokenType = "BACK" // BACK (we already treat these as IDENTs sometimes, but ALTAR wants explicit tokens)

	// Chambers / blocks / control
	TOK_CHAMBER    TokenType = "CHAMBER"
	TOK_ENDCHAMBER TokenType = "ENDCHAMBER"

	TOK_ENTANGLE TokenType = "ENTANGLE"
	TOK_RELEASE  TokenType = "RELEASE"
	TOK_CORE     TokenType = "CORE"

	TOK_CHOIR    TokenType = "CHOIR"
	TOK_ENDCHOIR TokenType = "ENDCHOIR"

	TOK_IF   TokenType = "IF"
	TOK_ELSE TokenType = "ELSE"
	TOK_END  TokenType = "END"

	TOK_WHILE    TokenType = "WHILE"
	TOK_ENDWHILE TokenType = "ENDWHILE"

	// Ephemeral / omens / summons
	TOK_EPHEMERAL TokenType = "EPHEMERAL"
	TOK_RAISE     TokenType = "RAISE"
	TOK_OMEN      TokenType = "OMEN"
	TOK_ENDOMEN   TokenType = "ENDOMEN"
	TOK_SUMMON    TokenType = "SUMMON"
	TOK_YIELDS    TokenType = "YIELDS" // for SUMMON WORK X YIELDS ...

	// Misc placeholders – keep around for expansion
	TOK_WORKWORD TokenType = "WORKWORD"

	// Punctuation / operators
	TOK_COLON  TokenType = "COLON"  // :
	TOK_COMMA  TokenType = "COMMA"  // ,
	TOK_SLASH  TokenType = "SLASH"  // /
	TOK_LPAREN TokenType = "LPAREN" // (
	TOK_RPAREN TokenType = "RPAREN" // )
	TOK_LBRACE TokenType = "LBRACE" // {
	TOK_RBRACE TokenType = "RBRACE" // }
	TOK_EQUAL  TokenType = "EQUAL"  // =

	TOK_PLUS    TokenType = "PLUS"    // +
	TOK_MINUS   TokenType = "MINUS"   // -
	TOK_STAR    TokenType = "STAR"    // *
	TOK_PERCENT TokenType = "PERCENT" // %
	TOK_BANG    TokenType = "BANG"    // !
	TOK_LT      TokenType = "LT"      // <
	TOK_GT      TokenType = "GT"      // >
	TOK_LTE     TokenType = "LTE"     // <=
	TOK_GTE     TokenType = "GTE"     // >=
	TOK_EQ      TokenType = "EQ"      // ==
	TOK_NEQ     TokenType = "NEQ"     // !=

	TOK_AND TokenType = "AND" // AND
	TOK_OR  TokenType = "OR"  // OR
	TOK_NOT TokenType = "NOT" // NOT

	TOK_LOG TokenType = "LOG" // LOG keyword or symbol
)

// Token is the unified lexical unit used by lexer and parser.
type Token struct {
	Type   TokenType
	Lexeme string
	Line   int
	Column int
	File   string
}

func NewToken(t TokenType, lex string, file string, line int, col int) Token {
	return Token{
		Type:   t,
		Lexeme: lex,
		File:   file,
		Line:   line,
		Column: col,
	}
}
