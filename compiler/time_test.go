package compiler

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func evalSingleTokenString(t *testing.T, tt TokenType) string {
	t.Helper()
	v, err := evalStringExpr(nil, []Token{{Type: tt, Lexeme: string(tt)}}, make(sigilTable))
	if err != nil {
		t.Fatalf("evalStringExpr(%s) returned error: %v", tt, err)
	}
	return v
}

func TestTimeTickIncrementsFromResetCounter(t *testing.T) {
	atomic.StoreUint64(&sicTimeTick, 0)
	first := evalSingleTokenString(t, TOK_TIME_TICK)
	second := evalSingleTokenString(t, TOK_TIME_TICK)
	if first != "1" || second != "2" {
		t.Fatalf("TIME_TICK got %q then %q, want 1 then 2", first, second)
	}
}

func TestTimeUnixMSPositiveIntegerLikeValue(t *testing.T) {
	got := evalSingleTokenString(t, TOK_TIME_UNIX_MS)
	n, err := strconv.ParseInt(got, 10, 64)
	if err != nil {
		t.Fatalf("TIME_UNIX_MS was not integer-like: %q", got)
	}
	if n <= 0 {
		t.Fatalf("TIME_UNIX_MS = %d, want positive", n)
	}
}

func TestTimeRFC3339Parses(t *testing.T) {
	got := evalSingleTokenString(t, TOK_TIME_RFC3339)
	if _, err := time.Parse(time.RFC3339, got); err != nil {
		t.Fatalf("TIME_RFC3339 did not parse: %q: %v", got, err)
	}
}

func writeTempScroll(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "time_sleep_test.sic")
	src := "LANGUAGE \"SIC 1.0\".\nSCROLL time_sleep_test\nMODE CHANT.\n\nWORK MAIN WITH SIGIL UNUSED AS TEXT:\n" + body + "\nENDWORK.\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("write temp scroll: %v", err)
	}
	return path
}

func TestSleepZeroSecondsSucceeds(t *testing.T) {
	path := writeTempScroll(t, "  SLEEP FOR 0 SECONDS.")
	if err := RunFile(path); err != nil {
		t.Fatalf("RunFile returned error: %v", err)
	}
}

func TestSleepNumericExpressionSucceeds(t *testing.T) {
	path := writeTempScroll(t, "  SLEEP FOR 1 + 1 - 2 SECONDS.")
	if err := RunFile(path); err != nil {
		t.Fatalf("RunFile returned error: %v", err)
	}
}

func TestSleepNegativeDurationErrors(t *testing.T) {
	path := writeTempScroll(t, "  SLEEP FOR -1 SECONDS.")
	err := RunFile(path)
	if err == nil {
		t.Fatal("RunFile succeeded, want negative SLEEP duration error")
	}
	if !strings.Contains(err.Error(), "duration must be >= 0") {
		t.Fatalf("unexpected error: %v", err)
	}
}
