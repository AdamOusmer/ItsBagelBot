---
title: AI code reviews
description: Model-assisted reads of a subsystem or PR. Records the model's verdict alongside human triage.
---

Each entry below is a dated review of a specific commit or PR. The model is given the same brief every time:
flag correctness bugs, security issues, and contracts that are easy to misuse. Its raw output is preserved in
the report, human triage is added in a separate section so a future reader can tell what came from the model and
what came from the team.

## Reports

| # | Date | Scope | Commit | Model | Verdict |
|---|------|-------|--------|-------|---------|

*No reviews published yet.*

## Why we keep these

AI reviews are noisy. They flag real bugs, they flag false positives, and occasionally they invent things that
are not in the diff. Publishing the raw verdict alongside the human triage makes it possible to look back and
ask "did the model actually help here?" with a real answer rather than a vibe.

## What is in scope

- New microservices and significant subsystems on their first merge.
- ADR-significant changes. A change that has its own ADR gets a review once the implementation lands.
- Security-relevant surfaces. Auth, token storage, anything that handles broadcaster credentials.

## What is not in scope

- Day-to-day refactors, renames, and bug fixes. The model would just generate noise.
- Generated code (mocks, protobufs, OpenAPI stubs). The contract belongs to the schema, not the output.
