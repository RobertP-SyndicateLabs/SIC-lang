SIC-lang — The Ritual Orchestration Language

[![License](https://img.shields.io/badge/license-Apache%202.0-blue)](LICENSE)
**CI:** pending

**Version:** v0.4.0  
**License:** Apache 2.0  
**Status:** Active Development  


## Build From Source

```sh
CGO_ENABLED=0 go build -o sic ./cli
```

Run the example smoke suite with:

```sh
scripts/run_examples.sh
```

A language of intention, discipline, and deterministic orchestration.


## What is SIC?

**SIC** is a human-readable orchestration language designed to unify how humans — and AI systems — express:

- behavior
- state
- parallel execution
- error handling
- service interactions
- structured workflows

SIC is not “just another syntax.”

It is a **ceremonial programming model** that treats computation as **intention, action, and consequence**.

It reads like a scroll.  
It executes like a workflow engine.  
It behaves like a disciplined runtime.


## The Four Pillars of SIC

SIC is built on four foundational principles:

### 🜁 Intention  
Explicit behavior via **WORKs** — nothing implicit, nothing hidden.

### 🜂 Responsibility  
Deterministic failure, recovery, and scoping.  
Failure is named. Recovery is structured.

### 🜃 Orchestration  
Sequential, parallel, and service-level execution as first-class language concepts.

### 🜄 Clarity  
No hidden state. No magic propagation. No ambiguous side effects.


## Why SIC Exists

Modern systems fragment intent across:

- Bash scripts
- Python automations
- YAML pipelines
- Kubernetes manifests
- Workflow engines
- HTTP services
- configuration files
- state machines

This leads to brittle systems and unclear behavior.

**SIC unifies all of it.**

SIC provides a single, explicit, deterministic language for expressing:

- workflows
- automation
- distributed service calls
- stateful systems
- orchestration logic
- parallel tasks
- failure behavior
- HTTP endpoint routing

No YAML → Go → Bash → Python → JSON → Terraform → back to YAML.

Just **SIC**.


## Who SIC Is For

SIC is designed for:

- Engineers building orchestration-heavy systems
- Teams managing workflows, automation, and services
- Developers tired of YAML-driven complexity
- Researchers exploring deterministic execution models
- AI systems that must express intent safely and explicitly


## Core Concepts

### WORK — Units of Intention

```sic
WORK GREET WITH SIGIL name AS TEXT:
    SAY: "Hello, " + name + ".".
ENDWORK.



SIGIL — Named State

SIGIL mood BE "joyful".



EPHEMERAL SIGIL — Auto-Scrubbed Scoped State

EPHEMERAL SIGIL secret BE "hidden".

Automatically removed on all exit paths (normal or failure).



INVISIBLE SIGIL — Non-Propagating State

Invisible sigils do not propagate to:

SUMMONed WORKs

CHOIR tasks


Unless explicitly passed.

This is how secrets stay secret.



SUMMON — Call a WORK

SUMMON WORK GREET WITH name AS "Ada".

SUMMON can also be used as an expression.



SEND BACK — Return Values

SEND BACK "Done.".



IF / WHILE — Deterministic Control Flow

WHILE count < 3:
    SAY: count.
    LET count BE count + 1.
ENDWHILE.



OMEN / FALLS_TO_RUIN — Structured Failure Handling

OMEN "network_down":
    RAISE OMEN "network_down".
FALLS_TO_RUIN:
    SAY: "Recovered gracefully.".
ENDOMEN.



WEAVE / CHOIR — Orchestration

WEAVE: sequential orchestration

CHOIR: parallel multi-task orchestration using isolated sigil snapshots and deterministic source-order error reporting




CHAMBER / ENTANGLE / RELEASE — Ownership Discipline

Scoped state ownership with runtime enforcement.

Think Rust-like borrow discipline, but ritualized.



SEALED WORK (v0.4.0)

A SEALED WORK requires a matching SEAL token to execute.

WORK SEALED VAULT SEAL "vault_key":
    SEND BACK "TREASURE".
ENDWORK.

Invocation:

SUMMON WORK VAULT SEAL "vault_key".

Without the correct seal, execution raises:

OMEN "sealed_work"



ALTAR — HTTP Services

ALTAR raises an HTTP server and binds routes to WORKs or inline responses.

Canonical Route Syntax

Unquoted paths are canonical:

ROUTE GET /hello TO SEND BACK "Hello".



Example: ALTAR Service

LANGUAGE "SIC 1.0".
SCROLL altar_demo
MODE CHANT.

WORK MAIN WITH SIGIL UNUSED AS TEXT:
    SAY: "Raising ALTAR.".

    ALTAR AT :15080:
        ROUTE GET /hello TO SEND BACK "Hello from SIC!".
        ROUTE GET /secure TO SEND BACK "Secure route active".
    ENDALTAR.

    SAY: "ALTAR active.".

    SIGIL forever BE "yes".
    WHILE forever IS "yes":
        SLEEP 1000.
    ENDWHILE.
ENDWORK.

Run:

CGO_ENABLED=0 go build -o sic ./cli
./sic run examples/altar_demo.sic

Then:

curl http://localhost:15080/hello



SEALED ALTAR (v0.4.0)

An ALTAR can be sealed on first bind.

Once sealed, all future modifications require the correct seal

Attempts without the seal or with the wrong seal raise:


OMEN "sealed_altar"

SEAL is only allowed in the ALTAR header, never in the body.

Negative tests enforce this strictly.



Current Status — v0.4.0

✔ Fully Implemented

Lexer, parser, runtime

WORK execution model

SIGIL state system

LET mutation

EPHEMERAL sigils (auto-scrubbed)

INVISIBLE sigils

IF / WHILE

SUMMON (statement + expression)

SEND BACK semantics

OMEN / FALLS / FALLS_TO_RUIN

WEAVE orchestration

CHOIR worker pool with isolated snapshots and deterministic source-order errors

CHAMBER scoping

ENTANGLE / RELEASE enforcement

Expression engine:

arithmetic

boolean logic

comparisons

nested expressions


ALTAR HTTP server

ALTAR inline SEND BACK → HTTP response

SEALED WORK

SEALED ALTAR

Positive + negative example suite


In Progress

Richer diagnostics

Typed sigil visualization


Coming in v0.5+

Remote SUMMON (cross-process workflows)

Persistent CHAMBERs

Scheduler primitives (EVERY N SECONDS:)

SIC_D dialect (canonical IR layer)

SIC_VM (bytecode engine)

Cluster orchestration model

SIC → Go / Python transpilers




Repository Structure

SIC-lang/
├── cli/              # CLI entry point
├── compiler/
│   ├── lexer.go
│   ├── parser.go
│   ├── runtime.go
│   └── tokens.go
├── examples/         # Fully working SIC scrolls (incl. negative tests)
├── scrolls/          # Design scrolls & philosophy
├── go.mod
├── LICENSE
└── README.md



Building & Running

Build

CGO_ENABLED=0 go build -o sic ./cli

Run a Scroll

./sic run examples/hello_plus.sic



Philosophy

SIC is a language of responsibility.

Where most languages obscure intent with syntax, mutation, and ambiguity, SIC makes intention explicit.

It treats:

behavior as ceremony

state as something to be honored

failure as something to be handled with dignity


Its aesthetic is ritual.
Its purpose is clarity.
Its goal is to unify how humans command machines.



Contributing

SIC is under active development.

Feedback, issues, and scroll contributions are welcome.

Formal code contributions will open once:

SIC_D stabilizes

the analyzer subsystem begins

ALTAR completes its extended service semantics


Until then:

Open issues

Propose features

Discuss SIC’s evolution




Supporting SIC

If you believe in SIC’s mission and want to accelerate its growth:

https://github.com/sponsors/RobertP-SyndicateLabs

Your support advances:

the SIC virtual machine

distributed SUMMON

persistent CHAMBERs

the analyzer

the official SIC_D dialect

documentation and onboarding



License

Apache 2.0 — open for experimentation, research, and commercial use.



SIC is a language built not just to run — but to endure.

If you’re ready, proceed to the scrolls.
If you’re brave, read the CHANT.
If you’re foolish, summon a CHOIR.

And if you’re wise —

SIC will orchestrate your systems.

## Language Semantics

The official runtime semantics for SIC v0.4 are defined here:

📜 [SIC v0.4 Runtime Semantics](docs/semantics/v0.4-runtime-semantics.md)
