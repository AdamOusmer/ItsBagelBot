---
title: System overview
description: The 10,000-foot view of ItsBagelBot. Actors, boundaries, and the systems it talks to.
---

ItsBagelBot is a fully scalable and high-availability Twitch bot that can be easily extended into more services by
adding the appropriate ingress workers. This allows an easy way to control multi-platform streamers with a single
configuration. 

We kept in mind the foortpring of each services in order to reduce the hardware and energy footprint of the system.

This page is the **C4 Level 1 (System Context) view**. It deliberately hides everything inside the bot and shows only
the actors that interact with it and the external systems it depends on. For what's inside the box,
see [Microservices →](/microservices/).

## Context diagram

> In Progress