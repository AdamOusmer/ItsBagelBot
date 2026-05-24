---
title: "0002 - Adoption of Go as Primary Service Language"
description: "Architecture decision record: Adoption of Go as Primary Service Language"
---

**Date:** 2026-01-04

## Status

Accepted

## Context

The previous iterations were made with Python. It was chosen for its ease to develop and its big community, offering a
large amount of libraries and tools to make development faster. However, sticking to python will contradict our
[decision to rewrite to microservices](/adr/0001-rewriting-to-microservices/) since the current libraries are not ready
to handle production systems.

Moreover, the language is not ready to handle high throughput in a low-power environment because of its Global
Interpreter Lock (GIL). While Python 3.13 introduced an experimental free-threaded build and later versions have
refined it up to today's (3.14), the runtime is not yet mature and the libraries aren't ready or stable enough for
production.

One could argue that the GIL is not really an issue for throughput on today's hardware, but we want to scale at low to
no cost. In that regime, Python's limited concurrency model quickly becomes a bottleneck.

In addition, Python image sizes and startup times are significant. Resolving wheel dependencies on each Docker build,
and re-indexing on each startup, costs substantially more compute than the proposed alternatives.

Laying down our requirements:

- High throughput in low-compute environments.
- Strong concurrency model.
- Maturity and stability of available tools for websockets/webhooks.
- Small image size and fast startup time.

## Decision

Based on the requirements above, Go is the language of choice. It satisfies every requirement we laid down.

- High throughput in low-compute environments: Go compiles to a single static binary that runs without a VM, and its
  runtime stays small under load.
- Concurrency model: goroutines and channels make concurrent code cheap and idiomatic, with no GIL to contend with.
- Maturity for websockets/webhooks: `net/http` is production-grade out of the box, and `gorilla/websocket` covers our
  streaming and event needs.
- Image size and startup time: a Go binary on top of a `scratch` or `distroless` base typically lands around 10–20 MB
  and starts in milliseconds.

## Consequences

- The learning curve for the language is non-trivial and must be accounted for.
- Concurrency cost is low enough to be almost negligible in our use cases.
- Managing multiple services and sharing state will require coordination through extra services or key-value storage,
  with strict conventions to respect during development.
- Error handling is verbose: every fallible call returns an explicit error, which adds boilerplate but forces failure
  paths to be considered up front.
- The talent pool familiar with Go is smaller than Python's, which makes onboarding future contributors more effortful.
- Some Twitch and Discord SDKs are Python-first, so we may end up wrapping third-party APIs ourselves where the Go
  community has not caught up yet. 

## Alternatives considered  

- Free-threaded Python ([PEP 703](https://peps.python.org/pep-0703/), the no-GIL build) was a good candidate, but the
  runtime's current maturity means we would spend more time fighting it than using it.
- Java's footprint is heavy for our low-cost
  hardware. [GraalVM native-image](https://www.graalvm.org/latest/reference-manual/native-image/)
  brings cold-start and memory down considerably, but the build pipeline complexity does not match the size of the team.
- Rust would give us comparable throughput and even smaller binaries, but learning curve is an absolute no in the 
current context.
- Node.js with TypeScript has a rich Twitch/Discord ecosystem, but the single-threaded event loop and the higher memory
  footprint per connection erode the throughput advantage we are optimizing for.
- C++ would yield extremely low compute cost, but the time-to-develop would push a one-person team into a high-risk
  zone of failure.
