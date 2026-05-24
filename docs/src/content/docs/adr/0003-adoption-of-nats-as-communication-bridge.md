---
title: "0003 - Adoption of NATS as Communication Bridge"
description: "Architecture decision record: Adoption of NATS (core, JetStream, and KV) as 
the communication bridge between services for events, RPC, and shard coordination"
---

**Date:** 2026-05-23

## Status

Accepted

## Context

Following our [decision to rewrite to microservices](/adr/0001-rewriting-to-microservices/) and our
[adoption of Go as the primary service language](/adr/0002-adoption-of-go-as-primary-service-language/), services now
need a way to talk to each other without coupling themselves through direct HTTP calls. A synchronous mesh would put
every service on the critical path of every other, and the v1's failure modes already showed how fragile that becomes
when one component stalls.

The value of this system is real-time. A chat message, a follow, a subscription, a redemption, all matter in the
seconds after they happen and lose almost all of their value shortly after. We are not building an event-sourced
analytics platform, and we explicitly do not log or collect user data beyond what is required to react to an event in
flight. That posture rules out long-term event logs and makes "replay months of history" a non-goal.

Beyond the asynchronous event flow, the architecture has two more communication needs that are easy to
under-estimate.

The first is request/reply between services. Some calls are inherently synchronous: "is this user a moderator of this
channel?", "give me the current rate-limit budget for this token", "what is the active configuration for this
tenant?". The default reflex here is gRPC, which is powerful but brings its own footprint: another transport on the
wire, a code generation pipeline, a service discovery story (DNS, sidecars, or a mesh), and per-service load
balancers. For the call patterns we actually have, that is a lot of moving parts for a one-person team to maintain.

The second is shard coordination. Several pieces of the system are inherently stateful per channel or per
WebSocket session: a Twitch chat connection, an EventSub session, the per-channel rate limiter, the per-tenant queue
of work. Each of those needs to be owned by exactly one instance at a time, and ownership has to be re-assigned
quickly when an instance dies. The usual answer is a coordination service (etcd, Consul, Zookeeper), which is another
stateful system with its own quorum, snapshots, and operational story.

Stacking gRPC plus a coordination service plus an event bus means three independent pieces of infrastructure to
operate, monitor, secure, and recover. The constraints from the previous ADRs still apply: we are scaling at low to
no cost on modest hardware, the team is small, and the operational surface has to stay manageable for one person.
We want one substrate, not three.

Laying down our requirements:

- Low memory and CPU footprint, in the same range as our Go services.
- Asynchronous events with at-least-once delivery and explicit acknowledgements, so important state changes are not
  lost on a brief consumer outage.
- Short-lived durability for those events: enough to ride out a restart or a redeployment, not a long-term log.
- A synchronous request/reply primitive that does not require a separate transport, code generation, or external
  service discovery.
- A coordination primitive (atomic put, TTL, watch) usable for shard ownership and leadership, without standing up a
  dedicated quorum service.
- A mature, well-maintained Go client.
- Simple operations: ideally a single binary, no JVM, no external coordination service.
- Subject-based routing that can evolve as our domain grows.

## Decision

Based on the requirements above, NATS jetstream is the communication substrate of choice for the entire system.
We use it in three modes, all served by the same cluster.

**Core NATS for request/reply (in place of gRPC).** A service publishes a request on a subject such as
`chat.mod.check`, any instance subscribed to that subject can answer, and the requester receives a single reply or a
timeout. Queue groups give us load balancing for free: every instance of a service joins the same queue group and
NATS hands each request to exactly one of them. The subject becomes the contract, the request and response shapes
live in a shared Go package, and there is no separate listener, port, or sidecar to wire up. We keep the door open
for gRPC if a specific call path later needs streaming, strict schemas, or extreme throughput, but it is no longer
the default.

**NATS JetStream for the event bus.**

- Footprint: the NATS server is a single static Go binary, in the same operational shape as our services. Idle memory
  usage stays in the tens of megabytes on the kind of hardware we are targeting.
- Delivery guarantees: JetStream provides at-least-once delivery with explicit acks, redelivery on timeout, and dead
  letter handling through max-deliver limits. That is exactly the safety net we want for the tiny window
  where a consumer might be unavailable.
- Short-lived durability: streams are configured with tight retention (by time and by size), so the bus acts as a
  short buffer rather than a historical record. We get the resilience without taking on a storage problem we did not
  ask for.
- Routing: subjects are hierarchical (`twitch.chat.message`, `twitch.eventsub.follow`, etc.), and wildcards let
  consumers subscribe at whatever granularity makes sense.

**JetStream KV for shard ownership and coordination.** KV buckets sit on top of JetStream and give us atomic
create-if-absent, per-key TTL, and watchers. That is the lease primitive we need. A service that wants to own a
shard does a create on `ingress.shard.<shard_id>` with its instance ID and a short TTL, renews the lease while it is
healthy, and lets it expire when it dies. Any other instance can watch the bucket and pick up the freed shard within
the TTL window. The same mechanism covers leader election for singletons. We get all of this from the cluster we are
already running for events and RPC, so HA collapses to "keep NATS healthy" instead of "keep NATS plus etcd plus a
discovery layer healthy."

**Go client.** `github.com/nats-io/nats.go` is maintained by the same team that ships the server, and core, JetStream,
and KV are all first-class in the same library rather than bolted on.

**Hub-Leaf.** The hub-leaf model allows us to easily maintain HA for our broker while allowing the leaves to enter
autonomous mode if the hub dies allowing service, even if degraded, for the time being.

It is worth naming what we are deliberately not buying. We are not using JetStream as an event log, we are not
planning to replay weeks of history, and we are not relying on it as a system of record. We are also not pretending
NATS req/rep is a full replacement for gRPC in every scenario; it is the default, not the only option. If either of
those positions changes, this ADR should be revisited.

## Consequences

- A critical piece of infrastructure is now in the path of every cross-service interaction. Its availability becomes
  part of the SLO of the whole system, and the blast radius of a NATS outage is wider than it would be with three
  separate systems: an outage stops events, RPC, and shard reassignment at the same time. We accept that trade in
  exchange for one HA story instead of three.
- Consumers must be idempotent. At-least-once delivery means the same event can be processed more than once after a
  redelivery, and silently double-processing is a class of bug we have to design out from the start.
- Retention windows have to be set short and watched. Long retention would quietly turn the bus into a data store,
  which contradicts our position on not collecting or logging user data.
- The subject hierarchy is a public contract, across events and RPC alike. Renaming a subject after consumers or
  callers exist is a breaking change, so the initial naming pass deserves real attention. Request and response types
  shared in a Go package have to be versioned with the same care a protobuf schema would receive.
- Without code-generated stubs, we lose the compile-time wire-type safety gRPC gives us. We mitigate that by keeping
  request and response types in shared internal packages and by writing thin typed wrappers around the raw NATS
  request call.
- We accept that an event missed beyond the retention window is gone. For a real-time system that is the right
  trade-off, but every consumer needs to be designed around that assumption rather than expecting unlimited backfill.
- Latency between services goes through the bus instead of staying in-process, which is the cost we already accepted
  when moving to microservices.

## Alternatives considered

### For the event bus

- RabbitMQ: a mature broker, but the operational footprint (Erlang VM, plugin management) is heavier than what a
  one-person team should carry, and its routing model is more rigid than subject-based routing once the domain starts
  to grow.
- Kafka: the industry standard for event logs, but it is built around the assumption that we want a long-lived,
  replayable log of every event. That is precisely what we are not building. The resource cost (JVM, KRaft or
  ZooKeeper, dedicated storage tuning) would be paying for capacity and complexity we are not going to use.
- Redis Pub/Sub: lightweight, but it is fire-and-forget. A message published while a consumer is
  briefly disconnected is lost, with no acknowledgement and no redelivery. Even in a real-time system, losing an
  EventSub callback or a chat command because a pod was mid-restart is not acceptable. Redis Streams would partially
  close that gap, but at that point we are reimplementing what JetStream already does, with weaker guarantees around
  redelivery and acks.

### For service-to-service request/reply

- gRPC: type-safe, fast, well-tooled, and the obvious default if RPC was the only need. The cost is the rest of the
  stack it pulls in (code generation, discovery, per-service load balancing) for a small set of internal calls that
  do not require streaming or strict schemas. Adopting NATS request/reponse does not close the door on gRPC; if a
  specific call path later needs what gRPC offers, we can introduce it for that path without changing the rest.
- Plain HTTP/JSON between services: simple, but reintroduces exactly the synchronous mesh and discovery problem we
  are trying to avoid, and adds yet another listener per service.

### For shard ownership and coordination

- etcd: battle-tested and designed exactly for this, with strong consistency and well-understood lease semantics. The
  blocker is operational: another stateful service with its own Raft cluster, snapshots, and recovery story, on top of
  NATS.
- Database-based locks (Postgres advisory locks, Redis Redlock): leans on storage we may already operate, but ties
  shard ownership to the availability and latency of that database. We would rather keep coordination on the same
  substrate as the events and RPC it is coordinating.
