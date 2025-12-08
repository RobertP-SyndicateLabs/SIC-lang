SIC-lang — The Ritual Orchestration Language

v0.3 — Expression Canticle

A language of intention, discipline, and deterministic orchestration.




What is SIC?

SIC is a human-readable orchestration language designed to unify how humans—and AI systems—express:

behavior

state

parallel execution

error handling

service interactions

structured workflows


SIC is not “just another syntax.”
It is a ceremonial programming model that treats computation as intention, action, and consequence.

It reads like a scroll.
It executes like a workflow engine.
It behaves like a disciplined runtime.

SIC is built on four pillars:

1. Intention — explicit behavior via WORKS


2. Responsibility — deterministic failure, recovery, scoping


3. Orchestration — parallel, sequential, distributed execution


4. Clarity — no hidden state, no ambiguity, no magic



SIC is currently implemented in its CHANT dialect:
a structured execution language with strict semantics and human-legible control flow.




Why SIC Exists

Modern development fragments intent across:

Bash scripts

Python automations

YAML pipelines

Kubernetes operators

Workflow engines

HTTP services

configuration files

state machines


This leads to brittle systems, unclear behavior, and human error.

SIC unifies all of it.

SIC provides a single, explicit, deterministic language for expressing:

workflows

automation

distributed service calls

stateful systems

orchestration logic

parallel tasks

error behavior

endpoint routing


No more YAML → Go → Bash → Python → JSON → Terraform → back to YAML.

Just SIC.



Key SIC Concepts

WORK — Units of intention

WORK GREET WITH SIGIL name AS TEXT:
    SAY: "Hello, " + name + ".".
ENDWORK.

SIGIL — Named state

SIGIL mood BE "joyful".

EPHEMERAL SIGIL — Auto-scrubbed scoped state

EPHEMERAL SIGIL temp BE "secret".

SUMMON — Call a WORK with bound state

SUMMON GREET WITH name AS "Ada".

SEND BACK — Returning values from a WORK

SEND BACK "Done.".

IF / WHILE — Deterministic control flow

WHILE count < 3:
    SAY: count.
    LET count BE count + 1.
ENDWHILE.

OMEN / FALLS_TO_RUIN — Structured failure handling

OMEN "network_down":
    RAISE OMEN "network_down".
FALLS_TO_RUIN:
    SAY: "Recovered.".
ENDOMEN.

WEAVE / CHOIR — Sequential or parallel orchestration

WEAVE:
    SING TASK_A.
    SING TASK_B.
ENDWEAVE.

CHAMBER / ENTANGLE / RELEASE — Scoped ownership discipline

Think “Rust-like borrow checking,” but ritualistic.

ALTAR — HTTP service endpoints

ALTAR AT :8080:
    ROUTE GET "/hello" TO WORK HELLO.
ENDALTAR.

(Full SEND BACK → HTTP response integration arriving in v0.4)




Current Status — v0.3 Expression Canticle

✔ Fully implemented:

Parser + lexer + runtime

WORK execution model

SIGIL state system

LET mutation

EPHEMERAL sigils with auto-clean

IF / WHILE

SUMMON

SEND BACK return semantics

OMEN / FALLS / FALLS_TO_RUIN

WEAVE orchestration

CHOIR (sequential baseline)

CHAMBER scoping

ENTANGLE / RELEASE (ownership discipline)

Expression engine with:

arithmetic

boolean logic

comparisons

nested expressions

SUMMON as expression


ALTAR: HTTP server with routing (v0.3)

17 example scrolls demonstrating the system


In progress:

ALTAR → HTTP-response SEND BACK support

CHOIR worker pool (true parallel execution)

Richer diagnostics

Typed sigils visualizer


Coming soon (v0.4+):

Remote SUMMON (cross-process workflows)

Persistent CHAMBERs (stateful storage)

Scheduler: EVERY N SECONDS:

SIC_D dialect (general-purpose canonical layer)

SIC_VM (bytecode execution engine)

Cluster orchestration model

SIC → Go/Python transpiler



Example: ALTAR service (live HTTP endpoint)

LANGUAGE "SIC 1.0".
SCROLL altar_demo
MODE CHANT.

WORK HELLO WITH SIGIL UNUSED AS TEXT:
    SEND BACK "Hello from SIC!".
ENDWORK.

WORK MAIN WITH SIGIL UNUSED AS TEXT:
    SAY: "Raising ALTAR.".
    ALTAR AT :15080:
        ROUTE GET "/hello" TO WORK HELLO.
    ENDALTAR.

    SAY: "ALTAR active.".

    SIGIL forever BE "yes".
    WHILE forever IS "yes":
        SLEEP 1000.
    ENDWHILE.
ENDWORK.

Run:

go build -o sic ./cli
./sic run examples/altar_demo.sic

Then:

curl http://localhost:15080/hello



Repository Structure

SIC-lang/
├── cli/               # CLI entry point
├── compiler/
│   ├── lexer.go       # tokenization
│   ├── parser.go      # AST + WorkDecl builder
│   ├── runtime.go     # execution engine
│   └── tokens.go
├── examples/          # full working SIC scrolls
├── scrolls/           # design scrolls & philosophy
├── go.mod
├── LICENSE
└── README.md



Building & Running

Build:

go build -o sic ./cli

Run a SIC scroll:

./sic run examples/hello_plus.sic



Philosophy

SIC is a language of responsibility.

Where most languages obscure intention with syntax, mutation, and ambiguity, SIC makes intention explicit.
It treats behavior as ceremony.
It treats state as something to be honored.
It treats failure as something to be handled with dignity.

Its aesthetic is ritual.
Its purpose is clarity.
Its goal is to unify how humans command machines.



Contributing

SIC is under active development.
Feedback, issues, and scroll contributions are welcome.

Code contributions will open formally once:

SIC_D dialect structure stabilizes

The analyzer subsystem begins

ALTAR completes its full service semantics


Meanwhile, please:

Open issues

Propose features

Discuss SIC’s growth

Sponsor development



Supporting SIC

If you value the mission and want SIC to reach its full potential:

https://github.com/sponsors/RobertP-SyndicateLabs

Your support accelerates:

the SIC virtual machine

distributed SUMMON

persistent CHAMBERs

the analyzer

the official SIC_D dialect

documentation and onboarding





License

Apache 2.0 — allowing open experimentation, commercial adoption, and research use.



SIC is a language built not just to run — but to endure.

If you’re ready, proceed to the scrolls.
If you’re brave, read the CHANT.
If you’re foolish, summon a CHOIR.

And if you’re wise —

SIC will orchestrate your systems.
