package compiler

import (
	"errors"
	"strings"
	"testing"
)

func TestBindJSONStoresCompactText(t *testing.T) {
	out, err := captureRunFileOutput(t, writeTempScroll(t, `  LET SIGIL RAW BE "{\"service\": \"SIC\", \"ok\": true}".
  BIND JSON FROM RAW AS SIGIL cfg.
  SAY: cfg.`))
	if err != nil {
		t.Fatalf("RunFile returned error: %v", err)
	}
	if !strings.Contains(out, `[SIC SAY] {"ok":true,"service":"SIC"}`) {
		t.Fatalf("expected compact JSON output; got:\n%s", out)
	}
}

func TestBindJSONAcceptsTargetWithoutSigilKeyword(t *testing.T) {
	out, err := captureRunFileOutput(t, writeTempScroll(t, `  LET SIGIL RAW BE "[1, 2, 3]".
  BIND JSON FROM RAW AS data.
  SAY: data.`))
	if err != nil {
		t.Fatalf("RunFile returned error: %v", err)
	}
	if !strings.Contains(out, `[SIC SAY] [1,2,3]`) {
		t.Fatalf("expected compact JSON output; got:\n%s", out)
	}
}

func TestWriteJSONWritesCompactText(t *testing.T) {
	out, err := captureRunFileOutput(t, writeTempScroll(t, `  LET SIGIL RAW BE "{\"z\": 1, \"a\": 2}".
  BIND JSON FROM RAW AS SIGIL cfg.
  WRITE JSON cfg AS TEXT INTO SIGIL out.
  SAY: out.`))
	if err != nil {
		t.Fatalf("RunFile returned error: %v", err)
	}
	if !strings.Contains(out, `[SIC SAY] {"a":2,"z":1}`) {
		t.Fatalf("expected compact JSON output; got:\n%s", out)
	}
}

func TestBindJSONInvalidRaisesStableOmenName(t *testing.T) {
	err := RunFile(writeTempScroll(t, `  LET SIGIL RAW BE "{ not real json }".
  BIND JSON FROM RAW AS SIGIL cfg.
  SAY: cfg.`))
	if err == nil {
		t.Fatal("RunFile succeeded, want invalid_json OMEN")
	}
	var omen *omenError
	if !errors.As(err, &omen) {
		t.Fatalf("error = %T %v, want omenError", err, err)
	}
	if omen.name != "invalid_json" {
		t.Fatalf("omen name = %q, want invalid_json", omen.name)
	}
}

func TestMirrorsDemoRuns(t *testing.T) {
	out, err := captureRunFileOutput(t, "../examples/mirrors_demo.sic")
	if err != nil {
		t.Fatalf("RunFile returned error: %v", err)
	}
	for _, want := range []string{
		`[SIC SAY] {"active":true,"service":"SIC"}`,
		`[SIC SAY] {"active":true,"service":"SIC"}`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q; got:\n%s", want, out)
		}
	}
}
