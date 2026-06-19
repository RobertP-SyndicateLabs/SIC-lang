package compiler

import (
	"strings"
	"testing"
)

func evalHandsSource(t *testing.T, src string, sigils sigilTable) string {
	t.Helper()
	lx := NewLexer(src, "hands_expr.sic")
	tokens := make([]Token, 0)
	for {
		tok := lx.NextToken()
		if tok.Type == TOK_EOF {
			break
		}
		tokens = append(tokens, tok)
	}
	got, err := evalStringExpr(nil, tokens, sigils)
	if err != nil {
		t.Fatalf("evalStringExpr(%q) returned error: %v", src, err)
	}
	return got
}

func TestHandsTextFunctions(t *testing.T) {
	sigils := sigilTable{"Name": "  Sic Lang  "}
	cases := map[string]string{
		`LOWER("SIC")`:       "sic",
		`UPPER("sic")`:       "SIC",
		`TRIM(Name)`:          "Sic Lang",
		`LENGTH("SIC")`:      "3",
		`LENGTH("Sigil")`:    "5",
		`LOWER(TRIM(Name))`:   "sic lang",
		`UPPER(TRIM(Name))`:   "SIC LANG",
	}
	for src, want := range cases {
		if got := evalHandsSource(t, src, sigils); got != want {
			t.Fatalf("%s = %q, want %q", src, got, want)
		}
	}
}

func TestHandsPredicateFunctions(t *testing.T) {
	cases := map[string]string{
		`CONTAINS("SIC language", "lang")`:       "true",
		`CONTAINS("SIC language", "missing")`:    "false",
		`STARTS_WITH("SIC language", "SIC")`:     "true",
		`STARTS_WITH("SIC language", "language")`: "false",
		`ENDS_WITH("SIC language", "language")`:   "true",
		`ENDS_WITH("SIC language", "SIC")`:        "false",
	}
	for src, want := range cases {
		if got := evalHandsSource(t, src, make(sigilTable)); got != want {
			t.Fatalf("%s = %q, want %q", src, got, want)
		}
	}
}

func TestHandsDemoRuns(t *testing.T) {
	out, err := captureRunFileOutput(t, "../examples/hands_demo.sic")
	if err != nil {
		t.Fatalf("RunFile returned error: %v", err)
	}
	for _, want := range []string{
		"[SIC SAY] sic lang",
		"[SIC SAY] SIC LANG",
		"[SIC SAY] 8",
		"[SIC SAY] true",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q; got:\n%s", want, out)
		}
	}
}
