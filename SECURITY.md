# Security Policy

SIC is a deterministic orchestration language.
Security is treated as a property of explicit behavior, not obscurity.

This document describes how security issues are handled.


## Supported Versions

SIC is under active development.

Security fixes are applied only to the latest released version.

| Version | Supported |
|--------|-----------|
| v0.3.x | ✅ Yes |
| < v0.3 | ❌ No |

Users are encouraged to upgrade promptly when releases occur.


## Reporting a Vulnerability

If you discover a security vulnerability in SIC, please report it responsibly.

**Do not open a public issue for security-sensitive reports.**

Instead:

- Email: wespaoletti@gmail.com

Include:

- A clear description of the issue
- Steps to reproduce (if applicable)
- Affected components (compiler, runtime, ALTAR, etc.)
- Potential impact

You will receive an acknowledgment within **72 hours**.


## Disclosure Process

1. Report is received and acknowledged
2. Issue is investigated
3. Fix is developed and validated
4. Release is prepared
5. Public disclosure occurs with the fix

SIC favors responsible disclosure over rapid publication.


## Scope

Security reports may include:

- Memory safety issues
- Runtime isolation failures
- ALTAR (HTTP) attack surfaces
- State leakage across CHAMBER or EPHEMERAL scopes
- Denial-of-service vectors
- Unexpected nondeterministic behavior

Reports that are purely theoretical without a clear exploit path
may be deprioritized.


## Philosophy

SIC treats security as a consequence of:

- Explicit control flow
- Deterministic execution
- Scoped state ownership
- Clear failure semantics

Security is not an afterthought.
It is part of the ritual.

Thank you for helping keep SIC disciplined and safe.
