---
title: "0001 - Rewriting to Microservices"
description: "Architecture decision record: Rewriting to Microservices"
---

**Date:** 2025-12-25

## Status

Accepted

## Context

The first version of the project (v1) has been developed in circumstances where AI was a core tool of development to
speed up the process. The main issues were then produced not by the tool, but by how the tool was used.

Three main mistakes while using this tool were:

- To give too much freedom for deciding how to build the system
- No context awareness. The AI didn't have access to the project and was used on the web.
- Lack of knowledge on networking.

These problems led to poor code quality and a high amount of antipatterns. Including non respect of Open/Closed
and Liskov Principles. The code ended becoming a huge spaghetti where a single change led to a change to at least five
different files and classes.

Also, the code was designed to work on local with a singular tenant. The plan to extend the code to multi-tenancy was
impossible to produce in a good amount of time. Moreover, the code was produced using Python, which led to the
infeasibility to scale properly using a monolith because of the GIL.

The v1 also had a major flaw in its networking. The library used to simplify the coding phase was really unstable and
was subject to a massive rewrite as Twitch migrated away from IRC towards EventSub using webhooks and websockets. This
led to the v1 to often have zombie processes where the heartbeat wouldn't come through and no reconnect attempts were
made. The library deliberately hid the access to the raw websocket and its lifecycle making the creation of a 
supervisor tedious and infeasible.

In order to keep on working on the multi-tenancy, the entire codebase would need refactoring and with the lack of
knowledge, the use of AI would have made things worse and a dependency for the feasibility of the project.

Moreover, the value brought to the portfolio wouldn't be as good as the proposed plan of migrating to microservices.
While it seems overkill, the value of scalability, isolation, maintainability and the value added to the portfolio
give the proposed plan its own strength.

## Decision

We are going to proceed to a full rewrite of the project while keeping the same repository to keep the progression of
the project on the worktree. The use of AI will be heavily reduced in order to learn and maintain good code quality and
promote the use of design patterns.

## Consequences

- The full rewrite will take time and bring the risk of the project not completing.
- The hardware needs will skyrocket since we are moving away from a monolith and a simple app on Digital Ocean is not 
enough
- The value for the portfolio and the knowledge to be acquired will be significant. 
- The scalability and overall stability of the project will greatly increase.
- Modularity will be forced throughout the project and rolling updates won't have downtime.
- The operational complexity will increase, since debugging across services, distributed transactions and the need for
an observability stack will become part of the daily cost.
- The network calls between services will add latency compared to in-process calls in a monolith, and eventual
consistency will introduce edge cases that didn't exist before.

## Alternatives considered

- Keep working with the current v1 and redesign it slowly. Rejected because the lack of networking knowledge would
keep AI as a dependency, and the unstable library would still need a full rewrite.
- Make a new monolith that is more modular. Rejected because the GIL would still cap the scaling, and the portfolio
value of working with orchestration, service meshes and observability would be lost.
