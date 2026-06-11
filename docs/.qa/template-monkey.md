---
title: "NUMBER - Monkey Test: SCOPE"
description: "Monkey test report: SCOPE, build SHA."
---

**Date:** YYYY-MM-DD
**Scope:** Service or subsystem under test.
**Build:** `SHA`
**Environment:** staging | local | other
**Tool:** Harness or framework used.
**Verdict:** Pass | Pass with notes | Regressed | Failed

## Summary

One paragraph. What was exercised, for how long, and the headline result. A reader who only reads this paragraph
should know whether to care.

## Method

What was injected, at what rate, for how long. Seed values and configuration that make the run reproducible. If
the harness has its own config file, link to the committed version.

## Findings

Numbered list. For each finding: short title, severity (`low`, `medium`, `high`), what was observed, where in the
code, and the commit or issue that resolves it (or a note that it is unresolved).

If the run produced no findings, say so explicitly. An empty findings section reads as "I forgot to write it".

## Artifacts

- Raw run log: link to private storage.
- Crash traces or core dumps: link to private storage.
- Any harness output that informed the findings.
