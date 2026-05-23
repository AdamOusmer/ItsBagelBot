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
*   **Real-Time Communication:** We rely strictly on **WebSockets** for main events. Webhooks are not used.
*   **Decoupled Frontend:**
    *   **Website:** A static frontend optimized for edge deployment (Cloudflare Pages).
    *   **Dashboard:** _In progress_
*   **Infrastructure:** Designed to run on **k3s** utilizing a Zero Trust network model.
*   **Networking:** Fully encrypted and secured with **Tailscale**, **Cloudflare Tunnels** and **Linkerd**

## Prerequisites

> In Progress

## Local Setup

> In Progress
