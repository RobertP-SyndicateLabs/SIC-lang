# SIC Language Semantics

**Version:** v0.3 — Expression Canticle  
**Status:** Stable, deterministic, fully specified

This document defines the formal execution semantics of **SIC (Structured Intent Canticle)**.
It describes how SIC scrolls are interpreted, how state behaves, and what guarantees the
runtime provides.

This is not an implementation guide.
This is the contract between the language and its users.



## 1. Core Execution Model

SIC executes code through **WORKs**.

A WORK is:
- The only executable unit
- Entered explicitly
- Executed deterministically
- Scoped in both state and failure

There is no implicit execution.
There is no hidden control flow.

Execution always proceeds forward, token by token, within a WORK body.



## 2. WORK Semantics

A WORK:

- Has a name
- May declare SIGIL parameters
- Executes sequentially unless an orchestration construct is used
- May return a value via `THUS WE ANSWER` or `SEND BACK`

If a WORK completes without returning a value:
- It returns an empty result
- This is not an error

WORKs may be marked **EPHEMERAL**, in which case all EPHEMERAL sigils created within them
are scrubbed upon exit, regardless of how execution terminates.



## 3. Sigils (State)

### 3.1 SIGIL Definition

SIGILs are named state values.
All SIGIL values are text at rest and may be interpreted as numbers or booleans by expressions.

SIGILs are:
- Explicit
- Mutable only via `LET`
- Scoped by execution context

There is no global mutable state outside of SIGILs.



### 3.2 Ephemeral Sigils

An EPHEMERAL sigil:

- Exists only for the duration of the current WORK (or EPHEMERAL block)
- Is automatically removed on any exit path:
  - Normal completion
  - `SEND BACK`
  - `THUS`
  - OMEN failure

EPHEMERAL behavior is enforced by the runtime and cannot be bypassed.



## 4. Sigil Visibility and Invisibility

SIGILs exist in two visibility classes:

### 4.1 Visible Sigils

Visible sigils:
- Are addressable by user code
- May be read, written, and printed
- May participate in expressions
- May be passed into WORKs

This is the default.



### 4.2 Invisible Sigils

Invisible sigils:
- Are runtime-reserved
- Are not addressable by user expressions
- Do not appear in output
- Do not leak across scope boundaries unless explicitly allowed

Invisible sigils may:
- Influence execution
- Control orchestration behavior
- Affect ALTAR responses
- Carry runtime metadata

Invisibility is **semantic**, not cosmetic.
To user code, invisible sigils are treated as nonexistent.



## 5. Expressions

Expressions are evaluated deterministically.

Supported features include:
- Arithmetic
- Boolean logic
- Comparisons
- String concatenation
- Parentheses
- `SUMMON` as an expression

Expressions:
- Have no side effects
- May read visible sigils
- May not mutate state directly



## 6. Control Flow

### 6.1 IF / WHILE

Control flow is explicit and deterministic.

- IF blocks evaluate conditions once
- WHILE loops reevaluate conditions on each iteration
- There is no implicit break or continue



### 6.2 Failure and OMEN

Failures are structured.

An OMEN:
- Is explicitly raised
- Has a name
- May be handled by an OMEN block

If an OMEN is unhandled:
- Execution terminates
- The error propagates upward

OMEN handling does not suppress cleanup.
EPHEMERAL sigils are always scrubbed.



## 7. SUMMON

`SUMMON` invokes a WORK.

SUMMON semantics:
- Clone visible sigils into a child environment
- Bind declared parameters
- Execute the target WORK
- Return its result (if any)

SUMMON does not share mutable state.
Each invocation is isolated unless explicitly designed otherwise.



## 8. WEAVE

WEAVE defines **sequential orchestration**.

Inside a WEAVE:
- Only SUMMON statements are allowed
- Each SUMMON executes in order
- Failure halts the WEAVE immediately

WEAVE introduces no concurrency.



## 9. CHOIR

CHOIR defines **parallel orchestration**.

Inside a CHOIR:
- Only SUMMON statements are allowed
- Each SUMMON executes concurrently
- All SUMMONs receive an identical snapshot of visible sigils

### CHOIR Guarantees

- No nondeterministic shared state mutation
- No execution order dependence
- Failure propagates after all active SUMMONs complete

CHOIR guarantees that concurrency does not alter observable program behavior.



## 10. ALTAR (HTTP Services)

ALTAR defines HTTP service bindings.

An ALTAR:
- Binds to a single address
- Registers routes deterministically
- Starts a server if one is not already running

### 10.1 ALTAR Lifetime

ALTAR does **not** own process lifetime.

- ALTAR activation returns control to the calling WORK
- The server runs alongside execution
- Multiple ALTAR blocks may coexist if bound to the same address



### 10.2 ROUTE Semantics

Each ROUTE:
- Binds an HTTP method and path
- Maps to either:
  - A WORK
  - An inline `SEND BACK` expression

Duplicate routes are rejected.

Each request:
- Receives a fresh sigil environment
- Includes injected request sigils
- Cannot mutate shared runtime state



## 11. Determinism Guarantees

SIC guarantees:

- Deterministic execution for identical inputs
- Explicit failure paths
- No hidden state
- No implicit concurrency effects
- No observable race conditions

Parallel execution does not compromise predictability.



## 12. Completion Semantics

If a WORK is executed with capture enabled:
- The first `THUS WE ANSWER` or `SEND BACK` returns immediately

If no answer is produced:
- The result is empty
- This is not an error

Top-level WORKs may complete without returning a value.



## 13. Semantic Stability

This document defines the complete semantics of SIC v0.3.

Future versions may:
- Extend semantics
- Introduce new constructs
- Add dialects (e.g. SIC_D)

They will not retroactively change the meaning of existing constructs.



SIC is a language of intention.
What is written is what executes.
What executes is what was written.
