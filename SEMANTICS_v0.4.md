SIC v0.4 Runtime Semantics

Extended and Operational Specification

Language: SIC (Structured Intent Canticle)
Version: v0.4.0
Scope: Runtime-level, operational semantics
Status: Canonical for v0.4 implementations

This document extends SEMANTICS.md by defining the precise operational behavior of SIC v0.4 constructs.

Where SEMANTICS.md defines what must be true,
this document defines how the runtime enforces it.




1. Execution Context Model

Every executing WORK operates within an Execution Context consisting of:

A visible sigil table

An invisible sigil table

An ephemeral sigil registry

A failure boundary

An ownership ledger (for CHAMBER)


Execution contexts form a tree, not a graph.

Child contexts:

inherit visible sigils by value,

inherit invisible sigils by rule,

never share mutable state unless explicitly entangled.




2. Sigil Inheritance Rules

2.1 Visible Sigils

When a WORK is entered:

visible sigils are cloned into the child context,

mutations affect only the child,

parent state is never mutated implicitly.


This applies to:

SUMMON

ALTAR route execution

WEAVE and CHOIR orchestration units




2.2 Invisible Sigils

Invisible sigils follow selective inheritance rules.

By default:

invisible sigils do not propagate across WORK boundaries.


Exceptions:

runtime-reserved invisible sigils (e.g. request metadata)

explicitly whitelisted orchestration sigils


User code:

cannot read,

cannot write,

cannot enumerate invisible sigils.




3. Expression Evaluation Order

Expressions are evaluated:

left to right,

eagerly,

without side effects.


SUMMON inside expressions:

executes fully before expression continuation,

may fail with OMEN,

returns a value or empty string.


Expression evaluation cannot:

mutate sigils,

raise SIGIL visibility,

alter execution order.




4. OMEN Propagation Semantics

When an OMEN is raised:

1. Execution halts immediately

2. Control transfers to the nearest enclosing OMEN handler

3. All EPHEMERAL sigils in the active scope are scrubbed

4. Ownership checks are enforced before handler entry



If no handler exists:

the OMEN propagates upward

execution terminates at top-level


OMEN handling:

does not rewind execution

does not restore prior state

does not suppress cleanup




5. WEAVE Execution Semantics

Inside a WEAVE:

only SUMMON statements are valid

each SUMMON executes in a fresh child context

execution is strictly sequential


Failure behavior:

the first failing SUMMON halts the WEAVE

no subsequent SUMMONs execute

failure propagates immediately




6. CHOIR Execution Semantics (v0.4)

CHOIR provides isolated orchestration, not shared concurrency.

In v0.4:

SUMMONs execute sequentially

each receives the same sigil snapshot

no SUMMON can observe another’s mutations


This design guarantees:

deterministic behavior

future parallelization safety

absence of data races


> Forward Compatibility Rule:
Any future parallel execution must preserve v0.4 observable behavior.




7. CHAMBER Ownership Semantics

CHAMBER establishes a resource ownership boundary.

7.1 ENTANGLE

ENTANGLE:

registers a sigil as owned by the CHAMBER

marks the sigil as requiring explicit release


7.2 RELEASE

RELEASE:

removes ownership responsibility

permits scope exit without error


7.3 Leak Detection

If a CHAMBER exits while:

one or more entangled sigils remain unreleased


Then:

execution fails immediately

a runtime error is raised

EPHEMERAL cleanup still occurs


Leak detection is strict and non-configurable.




8. ALTAR Runtime Model

ALTARs operate as singleton services per address.

the first ALTAR binds the address

subsequent ALTAR blocks reuse the same server

route registration mutates server state deterministically


ALTAR execution:

does not block the calling WORK

does not terminate automatically

persists for process lifetime




9. ALTAR Request Execution

Each incoming HTTP request:

1. Creates a fresh execution context

2. Injects request-specific invisible sigils

3. Executes the mapped WORK or inline SEND BACK

4. Produces an HTTP response

5. Destroys the context



Request contexts:

cannot mutate global runtime state

cannot access other requests

cannot observe server internals




10. ALTAR Sealing Semantics

ALTAR sealing rules are strict.

10.1 Sealing on First Bind

If an ALTAR is sealed on first bind:

a seal value is permanently associated with the server


10.2 Modification Rules

If an ALTAR is sealed:

any modification attempt must supply a matching SEAL

mismatches raise a deterministic OMEN


10.3 Header-Only Enforcement

SEAL / SEALED:

are valid only in the ALTAR header

are illegal inside the ALTAR body


If encountered in the body:

execution fails immediately

no routes after the violation are processed




11. SEND BACK Semantics

SEND BACK:

terminates the current WORK immediately

returns a value to the caller

triggers EPHEMERAL cleanup

finalizes HTTP responses when used in ALTAR contexts


Only the first SEND BACK is honored.




12. Determinism Contract

SIC v0.4 guarantees:

identical input → identical observable behavior

no hidden shared mutation

no timing-based semantics

no implicit concurrency effects


Any future extensions must uphold this contract.




13. Compatibility and Evolution

This document defines v0.4 operational semantics.

Future versions may:

add constructs,

extend orchestration,

introduce new dialects,

introduce alternative execution engines.


They must not:

reinterpret existing constructs,

weaken determinism,

invalidate existing scrolls.




Final Note

SIC is not designed to be clever.

It is designed to be correct, explicit, and trustworthy.

Execution is ceremony.
State is responsibility.
Failure is acknowledged — not ignored.
