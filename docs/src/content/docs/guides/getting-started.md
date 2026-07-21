---
title: Getting Started
description: A quick start guide to setting up the ItsBagelBot ecosystem.
sidebar:
  order: 1
---

Welcome to **ItsBagelBot**, a cloud-native, high-performance ecosystem designed for Twitch.

This guide covers the core concepts and steps needed to spin up the environment locally for development.

## Architecture Overview

ItsBagelBot uses a highly decoupled, microservice-oriented architecture prioritizing speed and zero-trust security:

*   **Backend Services:** Powered by **Go** and **Elixir**.
*   **Event Driven:** **NATS JetStream** handles all real-time message brokering between services.
*   **Real-Time Communication:** Twitch events arrive over **EventSub WebSockets** (Conduit shards), not Twitch webhooks. The one inbound HTTP webhook is the Tebex billing callback.
*   **Decoupled Frontend:**
    *   **Website:** A static frontend optimized for edge deployment (Cloudflare Pages).
    *   **Dashboard:** A **SvelteKit (SSR)** console: a public broadcaster dashboard and a tailnet-only operator admin.
*   **Infrastructure:** Designed to run on **k3s** utilizing a Zero Trust network model.
*   **Networking:** Management and SSH ride **Tailscale**, the public edge rides **Cloudflare Tunnels**, pod data rides a kernel **WireGuard** mesh, and the bus is protected by **NATS-native TLS**. There is no service mesh.

## Prerequisites

> In Progress

## Local Setup

> In Progress
