---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: WARP
description: "Builds the vendor client image used as Gossip’s untrusted HTTP proxy. This directory is an image, not a Go service."
docType: reference
---

## Responsibilities

Gossip validates destinations and routes untrusted requests through the proxy. The deployment starts the daemon in proxy mode; an unavailable route must not silently fall back to direct egress.

## Interfaces

The Containerfile installs the vendor client but does not define the runtime command. The deployment configures the daemon and loopback SOCKS listener. A listening socket is not proof of a working external tunnel; Gossip’s destination validation remains necessary.

## State and dependencies

The deployment starts the vendor daemon; the image itself does not define a command.

## Failure behavior

An unavailable untrusted route must not silently fall back to direct egress. A listener alone does not prove tunnel health.

## Code map

Source: [app/warp/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/warp). See `Containerfile`, `app/gossip/internal/core/http.go`, and `deploy/k8s/gossip.yaml`.

## Validation

Run `go test ./app/gossip/internal/core ./deploy/k8s` from the repository root for related source checks. Building the image and checking tunnel behavior require a container runtime.
