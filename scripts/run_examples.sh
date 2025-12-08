#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SIC="$ROOT/sic"
EX="$ROOT/examples"

examples=(
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
  "entangle_demo.sic"
  "choir_demo.sic"
  "send_back_demo.sic"
  "expr_demo.sic"
  "altar_demo.sic"
)

fail=0
for f in "${examples[@]}"; do
  echo "===== $f ====="
  if "$SIC" run "$EX/$f"; then
    echo "[OK] $f"
  else
    echo "[FAIL] $f"
    fail=1
  fi
  echo
done

exit "$fail"
