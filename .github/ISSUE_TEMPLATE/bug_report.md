---
name: Bug report
about: Create a report to help us improve
title: ''
labels: ''
assignees: ''

---

---
name: Bug report
about: Report unexpected behavior in SIC
title: "[BUG] "
labels: bug
assignees: ""
---

## Description

Describe the unexpected behavior clearly and concisely.

What did SIC do that it should not have done?
Or what did it fail to do?

---

## Reproduction

Provide the smallest possible SIC scroll or steps that reproduce the issue.

```sic
LANGUAGE "SIC 1.0".
SCROLL example
MODE CHANT.

WORK MAIN:
    // minimal reproduction here
ENDWORK.
---

Expected Behavior

What should have happened instead?


---

Actual Behavior

What actually happened?

Include error messages or output if applicable.


---

Environment

SIC version:

OS / platform:

How SIC was built or installed:



---

Additional Context

Any additional information that may help diagnose the issue:

Logs

Stack traces

Related features (ALTAR, CHAMBER, EPHEMERAL, etc.)



---

Severity (optional)

[ ] Cosmetic

[ ] Incorrect behavior

[ ] Runtime crash

[ ] State safety / determinism concern

[ ] Security-related (email security contact instead)
