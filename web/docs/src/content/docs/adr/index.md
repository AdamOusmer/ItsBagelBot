---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Architecture Decision Records
description: The architecture decision log.
docType: reference
---

Decisions retain their original dates and statuses. These summaries describe the choices and trade-offs in light of the current code. Superseded decisions remain in the log. The former hosting and observability provider records were retired during reorganization; they remain available in Git history.

| Decision | Date | Status |
| --- | --- | --- |
| [0001 - Rewrite as services](/adr/0001-rewrite-as-services/) | 2025-12-25 | Superseded in part by [ADR 0005](/adr/0005-elixir-twitch-ingress/); the rewrite and service boundaries remain. |
| [0002 - Go for services](/adr/0002-go-for-services/) | 2026-01-04 | Accepted; refined by [ADR 0005](/adr/0005-elixir-twitch-ingress/). |
| [0003 - NATS for service communication](/adr/0003-nats-service-communication/) | 2026-05-23 | Accepted. |
| [0004 - MySQL for durable state](/adr/0004-mysql-durable-state/) | 2026-05-23 | Accepted. |
| [0005 - Elixir for Twitch ingress](/adr/0005-elixir-twitch-ingress/) | 2026-05-23 | Accepted; supersedes [ADR 0001](/adr/0001-rewrite-as-services/) in part. |
| [0006 - Data service ownership](/adr/0006-data-service-ownership/) | 2026-06-09 | Accepted. |
| [0007 - Cache and write-behind](/adr/0007-cache-and-write-behind/) | 2026-06-09 | Accepted. |
| [0008 - Shared read projection](/adr/0008-shared-read-projection/) | 2026-06-09 | Accepted. |
| [0009 - Transactions owns giveaway awards](/adr/0009-transactions-own-giveaway-awards/) | 2026-09-15 | Accepted and implemented; fulfillment paths have separate gates. |
| [0010 - Kit owns the public variable inventory](/adr/0010-kit-variable-inventory/) | 2026-09-18 | Accepted and implemented. |

To add a decision, run `bun run adr:new "Short title"` from `web/docs/`, then add its translation in `fr/adr/` with the same number and filename. Reserve ADRs for choices that are costly to reverse, non-obvious and involve a real trade-off.
