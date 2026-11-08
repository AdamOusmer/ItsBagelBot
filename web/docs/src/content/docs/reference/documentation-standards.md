---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "Documentation standards"
description: "Page types, writing conventions, diagrams, and review requirements for engineering documentation."
docType: reference
---

These conventions apply to the engineering site. Each page serves one reader need, identifies the relevant code boundary, and distinguishes implemented behavior from a proposal. The code and its contracts remain authoritative for runtime behavior.

## Page types

Use [Diátaxis](https://diataxis.fr/start-here/) to separate learning, task completion, lookup, and understanding. Decision records and dated evidence have additional requirements.

| `docType` | Reader need | Required content |
| --- | --- | --- |
| `tutorial` | Learn through a controlled exercise | Starting state, steps, observable results, cleanup |
| `how-to` | Complete a specific task | Goal, prerequisites, procedure, verification, relevant troubleshooting |
| `reference` | Look up a contract or component | Scope, exact names and fields, defaults where applicable, examples, limitations, source |
| `explanation` | Understand the architecture | Scope, components, relationships, invariants, trade-offs, failure behavior, source |
| `decision` | Understand a consequential choice | Original date, status, context, decision, consequences, references |
| `record` | Assess a completed test or review | Date, source commit, method, environment, result, evidence, limitations |

Choose the type before writing. A service page is a reference; a procedure for changing that service is a how-to. Link between them. Do not label a proposed run as a completed record or a collection of commands as a tutorial.

## Writing and organization

The [Google developer style guide](https://developers.google.com/style/highlights) informs the writing conventions: active voice, sentence-case headings, descriptive links, and precise code formatting. Keep the project's direct voice. State the result or responsibility first, name the actor, and explain the condition before the action it changes.

For procedures, follow [Microsoft's instruction guidance](https://learn.microsoft.com/en-us/style-guide/procedures-instructions/writing-step-by-step-instructions): identify where the task starts, number actions in execution order, and provide an observable completion condition. Group alternative environments separately so a reader can follow one path.

- Use the page title as the only level-one heading; Astro renders it from frontmatter. Start body sections at level two and do not skip heading levels.
- Use short paragraphs for explanation, tables for parallel fields or responsibilities, and numbered lists for procedures.
- Put identifiers, paths, subjects, variables, and JSON fields in code formatting. Preserve the implementation's spelling and case.
- Define unfamiliar project terms through the [shared glossary](https://github.com/AdamOusmer/ItsBagelBot/blob/main/CONTEXT.md). Avoid giving one term several meanings across pages.
- Use concrete examples with synthetic identifiers. State the command's working directory and external prerequisites. Show expected outcomes without inventing successful output.
- Link to the owning source and related guidance. Keep a contract definition in one place and use links from its consumers.

## Service references

For an implemented service, cover responsibilities, interfaces, state and dependencies, failure behavior, code locations, and validation. Use only sections that carry useful information. A scaffold or image needs a clear implementation status and its actual boundary, not fabricated interfaces.

Document the difference between accepted work, durable state, acknowledged delivery, and a completed external effect. State retries, expiry, ordering, deduplication, and stale-read behavior only to the extent supported by the named implementation. A focused unit test does not establish fleet-wide or production behavior.

## Architecture diagrams

Choose one level of detail per diagram. [C4's diagram guidance](https://c4model.com/diagrams) distinguishes system context, containers, and components; use the level that answers the page's question. A service interaction diagram and a statement of deployment topology serve different purposes.

Use Mermaid flowcharts for relationships, sequence diagrams for time and messages, and state diagrams for transitions. Label arrows with the operation or data they carry. Keep identifiers stable, quote labels containing punctuation, and explain omitted paths in the surrounding text. Add a text explanation so understanding does not depend on color or the diagram alone.

Render diagrams with `bun run check:diagrams`. Inspect the affected page at desktop and mobile widths when its layout or diagram changes.

## Decisions and history

Keep sequential ADR IDs and original decision dates. Use a new decision when an accepted trade-off changes; mark the old decision as superseded and link both records. Record alternatives only when the rationale is evidenced. Do not infer a historical motive from present code.

Preserve published routes with redirects when pages move. If an anchor must change, update inbound links in the same change. French pages use the same relative filename and `docType`; identifiers and commands keep their original spelling. Other configured locales use the documented English fallback.

## Authoring and review

Start from `web/docs/templates/` or use `bun run adr:new "Decision title"`. Add a new page to `src/lib/spaces.ts` and create its French counterpart. Keep reusable templates outside the published content collection.

Before review, run these commands from `web/docs/`:

```sh
bun test src
bun run check:content
bunx playwright install chromium
bun run check:diagrams
bun run build
```

Run `python3 scripts/translations.py check --strict fr` from the repository root. Chromium installation is needed only when the browser is absent. The documentation CI job repeats the automated checks for relevant changes.

Review the changed claims against the owner implementation, not just another document. The automated checks cover metadata, navigation, local links and anchors, source targets, translation parity, numbering, and diagram rendering. They cannot establish that a technical explanation is true or that a translation is faithful. Keep that judgment in code review.
