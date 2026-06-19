#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SIC="${SIC:-$ROOT/sic}"
EX="$ROOT/examples"
TIMEOUT_SECONDS="${SIC_EXAMPLE_TIMEOUT:-10}"

cd "$ROOT"

echo "[build] CGO_ENABLED=0 go build -o sic ./cli"
CGO_ENABLED=0 go build -o "$SIC" ./cli

examples=(
  "hello.sic"
  "hello_plus.sic"
  "if_demo.sic"
  "summon_demo.sic"
  "yields_lex_test.sic"
  "arcwork_demo.sic"
  "weave_demo.sic"
  "omen_demo.sic"
  "falls_demo.sic"
  "scribe_demo.sic"
  "while_demo.sic"
  "ephemeral_demo.sic"
  "chamber_demo.sic"
  "choir_demo.sic"
  "send_back_demo.sic"
  "expr_demo.sic"
  "invisibility_demo.sic"
  "sealed_demo.sic"
  "choir_isolation_demo.sic"
  "choir_invisibility_demo.sic"
  "choir_sealed_demo.sic"
  "choir_sealed_negative_demo.sic"
)

expected_fail=(
  "entangle_demo.sic"
)

skip=(
  "altar_demo.sic"
  "altar_inline_demo.sic"
  "altar_json_demo.sic"
  "altar_mirrors_demo.sic"
  "altar_response_demo.sic"
  "sealed_altar_demo.sic"
  "sealed_altar_negative_demo.sic"
  "threads_demo.sic"
  "heartbeat_demo.sic"
  "rate_limit_demo.sic"
)

fail=0
for f in "${examples[@]}"; do
  echo "===== $f ====="
  if command -v timeout >/dev/null 2>&1; then
    if timeout "$TIMEOUT_SECONDS" "$SIC" run "$EX/$f"; then
      echo "[OK] $f"
    else
      echo "[FAIL] $f"
      fail=1
    fi
  else
    if "$SIC" run "$EX/$f"; then
      echo "[OK] $f"
    else
      echo "[FAIL] $f"
      fail=1
    fi
  fi
  echo
done

for f in "${expected_fail[@]}"; do
  echo "===== $f (expected fail) ====="
  if command -v timeout >/dev/null 2>&1; then
    if timeout "$TIMEOUT_SECONDS" "$SIC" run "$EX/$f"; then
      echo "[UNEXPECTED PASS] $f"
      fail=1
    else
      echo "[EXPECTED FAIL] $f"
    fi
  else
    if "$SIC" run "$EX/$f"; then
      echo "[UNEXPECTED PASS] $f"
      fail=1
    else
      echo "[EXPECTED FAIL] $f"
    fi
  fi
  echo
done

for f in "${skip[@]}"; do
  echo "[SKIP] $f (long-running ALTAR/service or sleep demo)"
done

exit "$fail"
