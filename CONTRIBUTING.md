# Contributing to SIC

SIC is a language of intention, discipline, and responsibility.

Contributions are welcome — but they are expected to respect the design,
philosophy, and long-term integrity of the system.

This document explains how to participate constructively.


## Guiding Principles

SIC is not an experiment in cleverness.
It is an experiment in **clarity**.

When contributing, prioritize:

- Explicit behavior over convenience
- Deterministic semantics over flexibility
- Readability over brevity
- Stability over novelty

If a change makes SIC harder to reason about, it is not a good change —
even if it is powerful.


## Ways to Contribute

You may contribute by:

- Reporting bugs with clear reproduction steps
- Improving documentation or examples
- Proposing language features via issues (not PRs)
- Submitting focused pull requests
- Reviewing issues and design discussions
- Improving tooling around SIC

Large or conceptual changes **must begin as an issue**.


## Issues First

Before writing code:

1. Open an issue describing:
   - The problem
   - Why it matters
   - How it fits SIC’s philosophy
2. Wait for design discussion or approval
3. Only then begin implementation

Pull requests that introduce new semantics without prior discussion
may be closed without review.


## Pull Request Guidelines

All pull requests must:

- Be small, focused, and intentional
- Include tests or example scrolls when applicable
- Avoid unrelated refactors
- Preserve backward compatibility unless explicitly approved
- Follow existing naming and structural conventions

One idea per pull request.


## Language Changes

Changes to the SIC language itself are held to a higher standard.

Language changes must:
- Preserve determinism
- Avoid implicit behavior
- Avoid hidden state
- Be explainable in one paragraph of plain English
- Be representable cleanly in the SIC_D canonical model

If a feature cannot be explained simply, it does not belong in SIC.


## Runtime & Compiler Changes

When modifying the compiler or runtime:

- Do not introduce side effects without explicit control flow
- Do not weaken scoping, ownership, or EPHEMERAL guarantees
- Prefer explicit errors over silent behavior
- Keep execution order obvious

Tests and example scrolls are strongly encouraged.


## Style & Tone

SIC has a distinct voice.

- Use clear, declarative language
- Avoid sarcasm, hostility, or dismissiveness
- Treat disagreements as design discussions, not debates
- Assume good faith

This project follows the Contributor Covenant Code of Conduct.


## Sponsorship

SIC is free and open.

Sponsorship is not required to contribute,
but it directly supports the time and focus required
to evolve SIC responsibly.

See: `SIC Sponsorship Philosophy`


## Final Note

SIC is designed to endure.

Every contribution becomes part of a language
intended to outlive trends, frameworks, and fashions.

If you contribute — do so with care.

Thank you for helping make SIC real.
