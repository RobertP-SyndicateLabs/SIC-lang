package compiler

import (
	"io"
	"os"
	"strings"
	"testing"
)

func captureRunFileOutput(t *testing.T, path string) (string, error) {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = w

	runErr := RunFile(path)

	_ = w.Close()
	os.Stdout = oldStdout

	out, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("read stdout: %v", readErr)
	}
	return string(out), runErr
}

func TestScribeAndLogDispatch(t *testing.T) {
	out, err := captureRunFileOutput(t, "../examples/scribe_demo.sic")
	if err != nil {
		t.Fatalf("RunFile returned error: %v", err)
	}

	for _, want := range []string{
		"[SIC SAY] Entering SCRIBE demo.",
		"[SIC SCRIBE] In the annals of Empire of Languages, SIC awakens.",
		"[SIC SCRIBE] Raw sigil dump: SIC / Empire of Languages.",
		"SCRIBE demo complete.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q; got:\n%s", want, out)
		}
	}
}
