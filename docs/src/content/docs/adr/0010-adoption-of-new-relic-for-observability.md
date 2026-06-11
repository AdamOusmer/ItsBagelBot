---
title: "0010 - Adoption of New Relic for Observability"
description: "Architecture decision record: Adoption of New Relic as the observability
platform, with per-message transactions and distributed tracing over the bus"
---

**Date:** 2026-06-09

## Status

Accepted

## Context

[ADR 0001](/adr/0001-rewriting-to-microservices/) named the cost honestly: debugging across services and the need
for an observability stack become part of the daily price of microservices. That bill is now due. A settings change
travels through a repository, a write-behind batcher, a database transaction, a JetStream event, and a projector
before it lands in Valkey; when that chain misbehaves, log files on five services are not an answer.

The constraints are the usual ones. The team is one person, the fleet runs on free and low-cost capacity
([ADR 0004](/adr/0004-adoption-of-oracle-cloud/)), and the services are deliberately small, so the observability
stack cannot be allowed to dwarf the system it watches. Self-hosting a metrics, traces, and logs pipeline
(Prometheus, Tempo, Loki, Grafana, and their storage) is a real operational project on its own, and it would eat
into the same ARM node the workloads share.

Two system-specific requirements stand out:

- Work here is message-driven, not request-driven. The unit of tracing is "one event consumed", and a trace must
  survive the hop through NATS from the publishing service to the consuming one.
- Monitoring must be optional at runtime. Local development and tests run without credentials and without a
  collector, and the code cannot be littered with nil checks to make that work.

## Decision

We adopt **New Relic** as the observability platform, on its free tier (100 GB of ingest per month, one full user),
wired through a single shared package (`pkg/monitor`).

- **One bootstrap per service.** `monitor.New` reads the license key from the environment. Without a key it returns
  a nil application, and the Go agent treats nil receivers as no-ops by design, so development and CI run
  unmonitored with zero branches in calling code. Configuration beyond the app name comes from `NEW_RELIC_*`
  variables, never from code.
- **A transaction per consumed message.** The bus consumer opens an APM transaction for every message, notices the
  error on failure before nacking, and exposes the transaction through the message context.
- **Distributed tracing across the bus.** Publishers inject trace headers into the Watermill message metadata, and
  consumers accept them, so one trace follows a module toggle from the flush transaction through JetStream into the
  projector's Valkey write.
- **Instrumented edges.** The MySQL pool opens through the `nrmysql` wrapper, so every ent query reports as a
  datastore segment of whatever transaction rides in the context. The projector reports each Valkey command as a
  datastore segment (under the Redis product, which is wire-accurate). Batch flushes run as their own background
  transactions, since they execute detached from any request. Logs forward through the `nrzap` core, so log lines
  carry trace context.

## Consequences

- Traces, metrics, logs, and errors land in one place, with the event hop stitched into one trace. The "why is this
  toggle not in Valkey" question becomes a trace lookup instead of five log files.
- We take a vendor dependency. It is confined: `pkg/monitor`, the bus, and the database provider import the agent;
  repositories only carry a transaction reference for background work. Moving to OpenTelemetry later means
  replacing those seams, not rewriting services.
- The free tier is a budget. 100 GB of ingest sounds large until log forwarding meets a hot chat channel; sampling
  and the agent's log limits need watching, and the zap sampling configured in production already caps the worst of
  it.
- The agent adds a background goroutine and harvest cycles per service. Measured against the services' footprint
  this is small, but it is not zero, and it is part of why monitoring stays disableable by environment.
- Telemetry leaves the infrastructure and lands on a third party. We already do not log user content as a matter of
  posture ([ADR 0003](/adr/0003-adoption-of-nats-as-communication-bridge/)), and tokens never appear in events or
  logs by construction, so what ships is operational metadata.

## Alternatives considered

- **OpenTelemetry with a self-hosted Grafana stack.** The standards-correct answer and the best escape hatch, but
  it turns one person into the operator of a telemetry pipeline (collector, Tempo, Loki, Prometheus, Grafana, plus
  their storage and upgrades) on hardware the workloads need. Revisit when the fleet or the team grows.
- **OpenTelemetry SDK exporting to a hosted backend.** Keeps the instrumentation vendor-neutral, but the Go OTel
  SDK is heavier to wire by hand (providers, processors, exporters per signal), and the hosted backends worth using
  are not freer than New Relic's tier. The pragmatic trade went to the integrated agent; the seams keep the exit
  open.
- **Prometheus and Grafana, metrics only.** Lightweight and self-contained, but metrics without traces answer "is
  it slow" and not "where", and the cross-service event hop is precisely the part that needs tracing here.
- **Nothing yet.** Free in money, expensive the first night something silently stops projecting. The whole point of
  paying the microservices tax knowingly was not to skip this line item.
