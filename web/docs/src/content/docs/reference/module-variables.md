---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Module variables in custom commands
description: "Use published module values in command responses."
docType: reference
---

## Syntax and availability

A custom command response can reference a published module value with `{module:variable}`. For example, `{valorant:tier}` reads a published Valorant value. The module must be enabled and otherwise available to that broadcaster; a variable does not enable it. When the module is unavailable, the token remains unresolved. For an enabled module, a fallback such as `{valorant:tier|rank unavailable}` supplies text for an empty value.

The dashboard's variable picker shows the available names. When a field appears in more than one reply view, `{module:view:variable}` selects a view explicitly. A timer has no triggering viewer, so viewer-specific values can be empty. Event-specific values likewise require the corresponding event; referencing a value does not execute the module's action.

## Catalogue and implementation

For implementation, the public reply catalogue is `internal/domain/modulevars/catalog.json`, embedded by `catalog.go`. The editor inventory is in `web/kit/lib/variables/`; runtime handling lives under `app/twitch/sesame/`. The public inventory is intentionally limited to published fields, not private configuration or credentials. Check the catalogue and parity tests when changing a variable or its spelling.
