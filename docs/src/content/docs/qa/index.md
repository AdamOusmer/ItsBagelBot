---
title: QA reports
description: Monkey tests and AI-assisted reviews. Dated, immutable evidence of how ItsBagelBot behaved and read at a point in time.
---

QA reports are **evidence**, not documentation. Each report captures how a service behaved, or how the code read,
at a specific commit. They are not how-tos and they do not get rewritten when a bug they surfaced is fixed.

Two streams live in this section:

- **[Monkey tests →](/qa/monkey/).** Randomized and property-style runs against a running service. Used to find
  crashes, deadlocks, lifecycle bugs, and unexpected state under load.
- **[AI code reviews →](/qa/ai-review/).** Model-assisted reads of a subsystem or PR. Used as a second pair of eyes
  on intent, structure, and security posture. The model's verdict is recorded as-is, with human triage in a
  separate section so the two can be told apart.

Security audits are deliberately **not** published here. A sanitized status summary belongs on the project status
page, the raw audit reports stay in a private repository. Publishing full audits, even after remediation, exposes
attack surface and dependency weakness patterns that are still useful to an attacker.

## How to read a report

Every report opens with a **verdict block**: date, scope, build SHA, environment, tool or model, and a one-line
verdict (`Pass`, `Pass with notes`, `Regressed`, `Failed`). Skim that first. The full method, findings, and artifact
links follow.

Reports are **immutable** once published. If a follow-up run produces a different result, it gets its own report
with the next sequential number. The older report is left as a record of what was true on its date, the same way
[ADRs](/adr/) are left in place when superseded.

## Numbering

Filenames follow `NNNN-YYYY-MM-DD-<short-scope>.md`. Numbers restart per subgroup so the two streams stay
independent, the same way two ADR logs would.

## Authoring a new report

Templates live alongside the ADR template:

- `docs/.qa/template-monkey.md`
- `docs/.qa/template-ai-review.md`

Copy the appropriate template into the matching subgroup directory, increment the number, and fill the verdict
block first. The rest of the report should be writable from the run's raw artifacts alone, with no memory of the
session required.

## Artifacts

Logs, traces, and raw model output do **not** live in the docs repo. They are linked from each report and stored
off-site (private bucket, internal Grafana). This keeps the docs build small and avoids accidentally publishing
hostnames, tokens, or stack frames that name internal paths.
