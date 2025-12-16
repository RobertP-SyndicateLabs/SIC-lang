SIC v0.3 Runtime Semantics

Expression Canticle — Official Semantics

This document defines the normative runtime semantics for SIC v0.3 with a focus on ALTAR (service orchestration) and CHOIR (parallel execution). It is intended to remove ambiguity, lock behavior, and serve as a reference for contributors, implementers, and users.



1. Foundational Principles

SIC execution is governed by four non-negotiable principles:

1. Determinism — Given the same scroll and inputs, observable behavior must not vary.


2. Explicitness — All state, control flow, and side effects must be declared.


3. Isolation — Concurrent execution must not create implicit shared state.


4. Responsibility — Failure is structured, surfaced, and handled deliberately.



ALTAR and CHOIR are extensions of these principles — not exceptions to them.



2. Execution Model Overview

At runtime, SIC operates as a single orchestrating process that may:

Execute WORKs sequentially

Execute WORKs concurrently (CHOIR)

Expose WORKs as HTTP endpoints (ALTAR)


All execution ultimately flows through execWork, which:

Receives a sigil table (state)

Executes statements in order

May spawn controlled concurrent execution




3. Sigils and State Semantics

3.1 Sigil Tables

A sigil table is a key → value mapping representing all visible state for a WORK invocation.

Properties:

Sigils are lexically scoped

Sigils are copied, not shared, across concurrency boundaries

Mutation (LET) only affects the local table


3.2 Isolation Rule (Hard Guarantee)

> No concurrent execution may mutate shared sigil state.



This rule applies universally:

CHOIR

ALTAR request handlers

SUMMON


The runtime enforces this by cloning sigil tables before execution.



4. ALTAR Semantics (HTTP Orchestration)

4.1 Purpose

ALTAR binds SIC WORKs and expressions to HTTP routes, turning a scroll into a live service.

ALTAR is:

Declarative

Deterministic

Process-scoped


It is not a general web framework.



4.2 ALTAR Lifecycle

ALTAR AT :15080:
    ROUTE GET "/hello" TO WORK HELLO.
ENDALTAR.

Semantics:

1. ALTAR initializes (or reuses) a singleton HTTP server


2. Routes are registered during scroll execution


3. Server begins listening asynchronously


4. Control returns to the WORK after ENDALTAR



> ALTAR does not block execution.



Process lifetime is controlled by the scroll (e.g. via WHILE, SLEEP).



4.3 Singleton Rule

Only one ALTAR server may exist per process

All ALTAR blocks must bind to the same address

Attempting to bind a different address is a runtime error


This prevents ambiguous network behavior.



4.4 ROUTE Semantics

Two routing forms are supported:

4.4.1 ROUTE → WORK

ROUTE GET "/hello" TO WORK HELLO.

For each request:

1. Parent sigils are cloned


2. Request sigils are injected


3. WORK is executed


4. SEND BACK result becomes HTTP body



If the WORK produces no body, "OK" is returned.



4.4.2 ROUTE → SEND BACK

ROUTE GET "/info" TO SEND BACK "hello".

The expression is evaluated per request with request-local sigils.



4.5 Request Sigil Injection

For every ALTAR request, the following SIGILs are injected (TEXT):

SIGIL	Meaning

REQUEST_METHOD	HTTP method
REQUEST_PATH	URL path
REQUEST_QUERY	Raw query string
REQUEST_BODY	Request body
Q_<NAME>	Query parameters


These sigils are read-only by convention.



4.6 Response Semantics

Content-Type resolution order:

1. response_content_type SIGIL (if set)


2. .json path suffix → application/json


3. Fallback (text/plain; charset=utf-8)



Status codes are currently implicit (200 / 404).



5. CHOIR Semantics (Parallel Execution)

5.1 Purpose

CHOIR executes multiple SUMMON statements concurrently while preserving determinism and isolation.

CHOIR:
    SUMMON WORK A.
    SUMMON WORK B.
ENDCHOIR.



5.2 Structural Rules

Inside CHOIR:

Only SUMMON statements are permitted

Order of execution is undefined

Completion is synchronized


Violations are runtime errors.



5.3 Isolation Semantics

Each SUMMON inside CHOIR receives:

A cloned sigil table

No shared mutable state


Parent sigils remain unchanged after CHOIR completes.

This is a hard guarantee.



5.4 Error Semantics

All SUMMONs execute

Errors are collected

First error (if any) is surfaced


No partial cancellation occurs.



6. Ephemeral Execution (Forward-Compatible)

6.1 EPHEMERAL Sigils (Existing)

EPHEMERAL SIGILs are:

Automatically scoped

Auto-scrubbed after execution




6.2 Proposed: EPHEMERAL ALTAR (Future)

Concept:

EPHEMERAL ALTAR AT :15080:
    ROUTE GET "/once" TO WORK TASK.
ENDALTAR.

Semantics:

ALTAR exists for the lifetime of the enclosing WORK

Automatically unregistered on exit

Useful for testing, callbacks, and transient services




6.3 Proposed: EPHEMERAL CHOIR (Future)

Concept:

EPHEMERAL CHOIR:
    SUMMON WORK TEMP_A.
    SUMMON WORK TEMP_B.
ENDCHOIR.

Semantics:

Same isolation guarantees

Additional guarantee: no external side effects permitted


This would formalize pure parallel computation.



7. Guarantees Summary

SIC v0.3 guarantees:

No hidden shared state

Deterministic orchestration

Explicit concurrency

Structured failure

Inspectable execution


These semantics are considered stable for v0.3.x.



8. Closing

SIC treats orchestration as a first-class responsibility.

ALTAR governs interaction with the world. CHOIR governs parallel intention.

Neither compromises clarity. Neither compromises determinism.

This is by design.
