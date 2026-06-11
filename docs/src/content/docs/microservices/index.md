---
title: Service registry
description: The services that make up ItsBagelBot, what they own, and how they authenticate to each other.
---

## Registry

| Service                                            | Repo path                  | Language       | Owns                                                                                  | Public to                                                            |
|----------------------------------------------------|----------------------------|----------------|---------------------------------------------------------------------------------------|----------------------------------------------------------------------|
| [Twitch Ingress](/microservices/twitch-ingress/)   | `services/twitch-ingress/` | Elixir (OTP 27+) | Twitch EventSub Conduit and its WebSocket shards; per-shard supervision; tenant OAuth lifecycle; filter-and-publish of normalized events | All NATS subscribers via `twitch.ingress.event.*` and `twitch.ingress.status.*` |

## Service-to-service shape

> In Progress

## Inter-service authentication

> In Progress
