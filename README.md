## Supporting SIC

If you value the direction and development of SIC, you can support the project through GitHub Sponsors:

https://github.com/sponsors/RobertP-SyndicateLabs

Your support accelerates development and helps move SIC toward a complete, production-grade ecosystem.
# ===============================================================

SIC-lang

The SIC Reference Interpreter — v0.1 (Active Development)

SIC is a disciplined, intention-driven language system designed to unify how humans express behavior, state, orchestration, and service interaction to machines.
This repository contains the official reference interpreter and runtime for SIC’s CHANT dialect.

SIC is not a framework, and not “another syntax.”
It is a structured method of describing what systems must do — explicitly, deterministically, and responsibly.




Purpose

Modern development fragments intent across many languages and tools.
SIC eliminates that fragmentation.

SIC provides:

WORKS — explicit units of executable intention

SIGILS — named state

EPHEMERAL SIGILS — protected, scoped state

IF / WHILE — deterministic flow

OMENS — structured failure semantics

FALLS_TO_RUIN — recovery and alternative paths

WEAVE — orchestrated sequential and parallel operations

SUMMON — calling WORKS with bound state

ALTARS — service endpoints and dispatch logic

SCRIBE — structured, deterministic output

SIC_D — general-purpose dialect (in design)


SIC is a unifying language system intended to bring clarity, intention, and responsibility back to software construction.




Repository Contents

.
├── cli/            # SIC command-line entry point
├── compiler/
│   ├── lexer.go    # SIC tokenizer
│   ├── parser.go   # SIC CHANT parser + Program/WorkDecl model
│   ├── runtime.go  # SIC interpreter and evaluator
│   └── tokens.go   # token definitions
├── examples/       # Complete demo scrolls
│   ├── hello_plus.sic
│   ├── summon_demo.sic
│   ├── while_demo.sic
│   ├── omen_demo.sic
│   ├── altar_demo.sic
│   ├── ephemeral_demo.sic
│   └── more...
├── go.mod
├── LICENSE
└── README.md       # (You are reading this)

Everything in compiler/ forms the CHANT-level implementation of SIC v1.0 concepts.




Building the SIC Interpreter

Prerequisites:

Go 1.21+


Build:

go build -o sic ./cli

Run a SIC scroll:

./sic run ./examples/hello_plus.sic




Current Language Features (Implemented)

✔ WORKS

WORK MAIN WITH SIGIL name AS TEXT:
    SAY: "Hello, " + name + "!".
ENDWORK.

✔ SIGILS / LET

LET SIGIL count BE 0.

✔ EPHEMERAL SIGILS

EPHEMERAL SIGIL temp BE "secret".

✔ SUMMON

SUMMON GREETING WITH name AS "World".

✔ IF / ELSE

IF SIGIL count EQUALS 3 THEN:
    SAY: "Matched.".
END IF.

✔ WHILE

WHILE SIGIL count LESS_THAN 3:
    SAY: count.
    LET SIGIL count BE count + 1.
ENDWHILE.

✔ OMENS (try/catch semantics)

OMEN "network_failure":
    RAISE OMEN "network_failure".
FALLS_TO_RUIN:
    SAY: "Recovered".
ENDOMEN.

✔ WEAVE

Sequential multi-work weaving with correct flow semantics.

✔ ALTAR (stubbed HTTP service)

Parses and registers HTTP routes; full service engine coming soon.




Examples

Run any example:

./sic run ./examples/while_demo.sic

Output:

[SIC SAY] Entering WHILE demo.
[SIC SAY] Loop turn 0.
[SIC SAY] Loop turn 1.
[SIC SAY] Loop turn 2.
WHILE demo complete.




Development Status

SIC-lang is under active, rapid development.

Completed:

CHANT interpreter

Lexer, parser, runtime

OMENS, SUMMON, WEAVE, EPHEMERAL, WHILE

ALTAR (parses & logs routes)


In Progress:

SIC_D dialect (general-purpose canonical layer)

Full evaluator and analyzer

ARCWORK parallel execution model

Formal CANON type system


Planned:

Service runtime

Multi-threaded weaving

Cross-language bridges

Deterministic serializer

SIC → other languages transpilers





Philosophy (Summary)

SIC is a language of responsibility.
It is designed to express intention clearly, deterministically, and without ambiguity.

Its purpose is not replacement for its own sake —
but simplification, unification, and mastery of system behavior without fragmentation.

Full philosophy scrolls are available in:

SIC-scrolls/




Contributing

SIC is open for feedback and discussion.
Code contributions will open after the analyzer and SIC_D scaffolding is complete.

For now:

open issues

share feedback

follow development on LinkedIn

or become a sponsor



License

The reference interpreter is released under Apache 2.0.



Support the Project

If SIC’s mission resonates with you, you can support ongoing development through GitHub Sponsors:

https://github.com/sponsors/RobertP-SyndicateLabs
