Excellent — this is the right moment to do this.
Below is a full, drop-in replacement for SEMANTICS.md, adjusted to:

Reflect v0.4.0 reality

Stay stable and authoritative

Avoid over-promising future behavior

Cleanly defer deeper detail to SEMANTICS_v0.4.md


This version is intentionally high-level, normative, and durable.
You can paste this in as-is.




SIC Language Semantics

Version: v0.4.0 — Orchestration Canticle
Status: Stable, deterministic, normative

This document defines the core execution semantics of SIC (Structured Intent Canticle).

It specifies:

how SIC scrolls execute,

how state behaves,

how failure propagates,

and what guarantees the runtime provides.


This is not an implementation guide.
This is the semantic contract between the SIC language and its users.

Low-level, version-specific, or experimental behavior is documented separately in SEMANTICS_v0.4.md.




1. Core Execution Model

SIC executes exclusively through WORKs.

A WORK is:

the only executable unit,

entered explicitly,

executed deterministically,

scoped in both state and failure.


There is:

no implicit execution,

no hidden control flow,

no ambient mutation.


Execution proceeds forward, statement by statement, within a WORK body.




2. WORK Semantics

A WORK:

has a name,

may declare SIGIL parameters,

executes sequentially unless an orchestration construct is used,

may return a value via SEND BACK or THUS WE ANSWER.


If a WORK completes without producing a value:

it returns an empty result,

this is not an error.


2.1 EPHEMERAL WORKs

A WORK may be declared EPHEMERAL.

For EPHEMERAL WORKs:

all EPHEMERAL sigils created within the WORK

are scrubbed on any exit path:

normal completion,

early return,

OMEN failure,

runtime error.



EPHEMERAL cleanup is mandatory and enforced by the runtime.




3. Sigils (State)

3.1 SIGIL Definition

SIGILs are named state values.

Properties:

all values are text at rest,

values may be interpreted as numbers or booleans by expressions,

mutation is allowed only via LET.


There is no global mutable state outside SIGILs.




3.2 Ephemeral Sigils

An EPHEMERAL sigil:

exists only within its declared scope,

cannot outlive the enclosing WORK or EPHEMERAL block,

is removed regardless of how execution terminates.


This behavior is guaranteed and cannot be bypassed.




4. Sigil Visibility

SIGILs exist in two visibility classes.

4.1 Visible Sigils

Visible sigils:

are addressable by user code,

may be read, written, and printed,

participate in expressions,

may be passed into WORKs.


This is the default.




4.2 Invisible Sigils

Invisible sigils:

are runtime-reserved,

are not addressable by user expressions,

do not appear in output,

do not leak across scope boundaries unless explicitly permitted.


Invisible sigils may influence:

execution behavior,

orchestration control,

ALTAR request handling,

runtime metadata.


Invisibility is semantic, not cosmetic.
To user code, invisible sigils are treated as nonexistent.




5. Expressions

Expressions are deterministic and side-effect free.

Supported features include:

arithmetic,

boolean logic,

comparisons,

string concatenation,

parentheses,

SUMMON as an expression.


Expressions:

may read visible sigils,

may not mutate state directly,

may not raise OMENs.





6. Control Flow

6.1 IF and WHILE

Control flow is explicit.

IF evaluates its condition once,

WHILE reevaluates its condition on each iteration,

there is no implicit break or continue.





6.2 Failure and OMEN

Failure is structured and intentional.

An OMEN:

is explicitly raised,

has a symbolic name,

may be handled by an OMEN block.


If an OMEN is unhandled:

execution terminates,

the error propagates upward.


Cleanup is never suppressed: EPHEMERAL sigils are always scrubbed.




7. SUMMON

SUMMON invokes a WORK.

SUMMON semantics:

clone visible sigils into a child environment,

bind declared parameters,

execute the target WORK,

return its result (if any).


SUMMON does not share mutable state. Each invocation is isolated unless explicitly designed otherwise.




8. WEAVE

WEAVE defines sequential orchestration.

Inside a WEAVE:

only SUMMON statements are allowed,

SUMMONs execute strictly in order,

failure halts the WEAVE immediately.


WEAVE introduces no concurrency.




9. CHOIR

CHOIR defines orchestrated isolation.

Inside a CHOIR:

only SUMMON statements are allowed,

each SUMMON receives an identical snapshot of visible sigils,

SUMMONs execute in an isolated manner.


CHOIR Guarantees

no shared mutable state,

no sigil leakage between SUMMONs,

deterministic observable behavior.


> Note:
In v0.4.0, CHOIR execution is sequential with isolation.
True parallel execution is a planned extension and will not alter these guarantees.




10. Ownership and Scoped Discipline

10.1 CHAMBER

A CHAMBER defines an isolated ownership scope.

Inside a CHAMBER:

sigils may be entangled,

ownership rules are enforced by the runtime,

state cannot leak unless explicitly released.





10.2 ENTANGLE and RELEASE

ENTANGLE:

binds a sigil to the current scope,

establishes ownership responsibility.


RELEASE:

explicitly relinquishes ownership.


If a CHAMBER exits with unreleased entanglements:

execution fails with a runtime error,

cleanup still occurs.


This discipline is enforced and non-optional.




11. ALTAR (HTTP Services)

ALTAR defines HTTP service bindings.

An ALTAR:

binds to a single address,

registers routes deterministically,

starts a server if one is not already running.





11.1 ALTAR Lifetime

ALTAR does not control process lifetime.

activation returns control to the calling WORK,

the server runs alongside execution,

repeated ALTAR invocations at the same address operate on the same service instance.





11.2 ROUTE Semantics

Each ROUTE:

binds an HTTP method and path,

maps to either:

a WORK, or

an inline SEND BACK expression.



Duplicate routes are rejected.

Each request:

executes in a fresh sigil environment,

receives injected request sigils,

cannot mutate shared runtime state.





11.3 ALTAR Sealing

An ALTAR may be sealed at first bind.

If sealed:

further modification requires a matching SEAL,

unsealed or mismatched attempts fail deterministically,

SEAL and SEALED are valid only in the ALTAR header.


SEAL usage inside the ALTAR body is a runtime error.




12. Determinism Guarantees

SIC guarantees:

deterministic execution for identical inputs,

explicit failure paths,

no hidden state,

no implicit concurrency effects,

no observable race conditions.


Parallelism (where introduced) must preserve these guarantees.




13. Completion Semantics

If a WORK is executed with capture enabled:

the first SEND BACK or THUS WE ANSWER terminates execution immediately.


If no answer is produced:

the result is empty,

this is not an error.


Top-level WORKs may complete without returning a value.




14. Semantic Stability

This document defines the stable core semantics of SIC v0.4.0.

Future versions may:

extend semantics,

introduce new constructs,

add dialects (e.g. SIC_D).


They will not retroactively alter the meaning of constructs defined here.




SIC is a language of intention.
What is written is what executes.
What executes is what was written.
