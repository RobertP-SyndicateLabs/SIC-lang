package compiler

import "testing"

func TestLexerRecognizesCoreKeywords(t *testing.T) {
	lx := NewLexer(`SCRIBE: "x".
LOG: "y".
CHOIR:
ENDCHOIR.
`, "test.sic")

	want := []TokenType{TOK_LOG, TOK_COLON, TOK_STRING, TOK_DOT, TOK_LOG, TOK_COLON, TOK_STRING, TOK_DOT, TOK_CHOIR, TOK_COLON, TOK_ENDCHOIR, TOK_DOT, TOK_EOF}
	for i, typ := range want {
		got := lx.NextToken()
		for got.Type == TOK_NEWLINE {
			got = lx.NextToken()
		}
		if got.Type != typ {
			t.Fatalf("token %d: got %s (%q), want %s", i, got.Type, got.Lexeme, typ)
		}
	}
}

func TestParserFindsWorkAndHeaderFields(t *testing.T) {
	src := `LANGUAGE "SIC 1.0".
SCROLL STRONG parser_test
MODE CHANT.
PROFILE "CIVIL"

WORK SEALED VAULT WITH SIGIL UNUSED AS TEXT SEAL "vault_key":
    SEND BACK "ok".
ENDWORK
`
	p := NewParser(NewLexer(src, "parser_test.sic"))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parser errors: %v", errs)
	}
	if prog.Language != "SIC 1.0" || prog.Scroll != "parser_test" || prog.Mode != "CHANT" || prog.Profile != "CIVIL" {
		t.Fatalf("unexpected headers: %#v", prog)
	}
	if len(prog.Works) != 1 {
		t.Fatalf("got %d works, want 1", len(prog.Works))
	}
	w := prog.Works[0]
	if w.Name != "VAULT" || !w.Sealed || w.SealToken != "vault_key" {
		t.Fatalf("unexpected work metadata: %#v", w)
	}
	if len(w.SigilParams) != 1 || w.SigilParams[0] != "UNUSED" {
		t.Fatalf("unexpected sigil params: %#v", w.SigilParams)
	}
}

func TestRunFileSayDemo(t *testing.T) {
	out, err := captureRunFileOutput(t, "../tests/test_say.sic")
	if err != nil {
		t.Fatalf("RunFile returned error: %v", err)
	}
	if out != "[SIC SAY] Test OK\n" {
		t.Fatalf("unexpected output: %q", out)
	}
}
