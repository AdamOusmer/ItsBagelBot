---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0010 - Kit owns the public variable inventory"
description: "Architecture decision: Kit owns the public variable inventory"
docType: decision
---

**Date:** 2026-09-18

**Status:** Accepted and implemented.

## Context

Reply-variable inventories and examples had drifted across the Go and web code.

## Decision

The Go scope resolver owns token behavior; the kit manifest owns the public inventory, forms, samples and copy keys used by web surfaces. A shared golden fixture checks parity.

## Consequences

Web CI does not need to generate the manifest from Go, but variable changes require updates to both inventories.

## References

[Go resolver](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/twitch/sesame/engine/scope), [Kit manifest](https://github.com/AdamOusmer/ItsBagelBot/tree/main/web/kit/lib/variables).
