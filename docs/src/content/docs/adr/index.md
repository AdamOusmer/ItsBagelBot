---
title: Architecture Decision Records
description: The decision log for ItsBagelBot. Why the system looks the way it does.
---

Architecture Decision Records (ADRs) capture the **reasoning** behind architectural choices: the context that motivated
a decision, the option that was chosen, the consequences that follow, and the alternatives that were weighed.

ADRs are **immutable** once accepted. If a later decision changes course, we don't edit the original. We write a new
ADR that supersedes it and update the older one's status to point forward.

## The log

ADRs are listed in the sidebar in numerical order. Each filename follows the pattern `NNNN-kebab-case-title.md`.

## Writing a new ADR

From the `docs/` directory:

```sh
bun run adr:new "Short title of the decision"
# or: npm run adr:new "Short title of the decision"
```

The wrapper points `adr-tools` at the project template (`docs/.adr/template.md`) and writes the new file into this
folder with the next sequential number.

To supersede or link to an existing record:

```sh
bun run adr:new -- -s 3 "Replaces decision 3"
bun run adr:new -- -l "3:Amends:Amended by" "Amends decision 3"
adr list
```

## What deserves an ADR?

Write an ADR when a choice is:

- **Architecturally significant:** It shapes how other components are built or constrains future options.
- **Costly to reverse:** Flipping the decision later would require coordinated change across services, infrastructure,
  or data.
- **Non-obvious:** A future maintainer reading the code would reasonably ask *"why this way?"*

Day-to-day refactors, bug fixes, and naming preferences don't need ADRs. Commit messages and PR descriptions are the
right home for those.

## Anatomy of a record

Each ADR contains:

- **Status:** Proposed, Accepted, Deprecated, or Superseded by `[ADR-NNNN]`.
- **Context:** The forces in play: constraints, requirements, prior decisions, incidents that prompted it.
- **Decision:** The choice, stated in the active voice ("We will…").
- **Consequences:** What becomes easier, harder, or riskier as a result. Be honest about the trade-offs.
- **Alternatives considered:** Options that were weighed and why they lost.

See [`adr help`](https://github.com/npryce/adr-tools) for the full command reference.
