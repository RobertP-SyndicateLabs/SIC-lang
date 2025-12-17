# SIC 1.0 — Security & Capability Model

SIC treats state and authority as explicit, inspectable constructs.
A SIC program should make it *obvious* what information is available, where it flows, and what boundaries exist.

This document defines SIC’s current security model and its planned capability primitives.


## Core Principles

1. **Explicit State**
   - All runtime state is carried through SIGILs.
   - No hidden ambient globals (beyond intentional runtime internals).

2. **Deterministic Boundaries**
   - Crossing a boundary (SUMMON, CHOIR task, ALTAR request) must define what is inherited.

3. **Fail Closed**
   - Missing/forbidden access should not degrade silently.
   - Preferred: structured OMEN paths instead of implicit fallbacks.

4. **Non-Exfiltration Defaults**
   - Sensitive values should not leak through output, logs, returns, or HTTP responses unless explicitly allowed.


## SIGIL Classification

SIC recognizes multiple “classes” of sigils:

### Visible SIGIL (default)
- Normal state, inherited by default into child execution environments (unless restricted).

### INVISIBLE SIGIL
- Exists in the environment but is **not inherited** across boundaries that clone sigils.
- Any attempt to emit it externally must be redacted or rejected per output rules.

### EPHEMERAL SIGIL
- A sigil that is **scrubbed on scope exit** (WORK exit, including error/OMEN paths).
- Used for secrets, temporary tokens, transient request data, and short-lived control state.

### INVISIBLE + EPHEMERAL
- Allowed and encouraged for secrets:
  - Not inheritable
  - Not exfiltratable
  - Not persistent


## Inheritance & Boundary Rules

### WORK execution (same environment)
- A WORK executes against a sigil table.
- EPHEMERAL sigils created in the WORK are scrubbed on exit.

### SUMMON boundary
When SUMMON executes a WORK, it creates a child environment.

**Default rule:**
- Child inherits **visible** sigils only.
- Child does **not** inherit INVISIBLE sigils.
- Internal meta-keys are never copied.

This makes SUMMON a **capability boundary**: visibility governs authority.

### CHOIR boundary
CHOIR runs multiple SUMMON statements concurrently.

**Default rule:**
- Each task must run in its own child environment.
- Each child inherits **only visible** sigils.
- INVISIBLE sigils must not appear inside CHOIR tasks unless explicitly passed in a sealed way (future).

This prevents accidental secret leakage into parallel workers.

### WEAVE
WEAVE is sequential orchestration; it still uses SUMMON semantics per summon statement.


## Output & Exfiltration Rules

SIC recognizes “exfiltration channels”:

- `SAY:` output
- `SEND BACK` return values
- ALTAR HTTP response bodies
- ALTAR headers/status (if derived from sigils)
- Logging / tracing hooks (future)

### Redaction Policy

If an output expression is *tainted* by an INVISIBLE sigil, output must not reveal the raw value.

**Current behavior:**
- Redact: return `[REDACTED]`.

**Allowed future modes (configurable):**
- Redact (default)
- Error (fail fast)
- Replace-with-empty


## ALTAR Boundary Model

ALTAR creates a long-lived HTTP boundary that invokes WORK handlers.

### Request injection
ALTAR may inject request properties into sigils (REQUEST_METHOD, REQUEST_PATH, Q_* keys, REQUEST_BODY, etc.).

**Guidelines:**
- Request sigils should be treated as untrusted input.
- If a request contains secret-like values, prefer EPHEMERAL + INVISIBLE storage.

### Response control
ALTAR responses may be influenced by response sigils such as:
- `response_content_type`
- `response_status`
- `response_header_*` (if implemented)

**Security rule:**
- Any response output derived from INVISIBLE sigils must be redacted or rejected.


## Planned Capability Primitives

### INVISIBLE Blocks
A block-level construct to mark all sigils created inside as invisible by default.

Example:
```sic
INVISIBLE:
    LET SIGIL SECRET BE "doom".
ENDINVISIBLE.
