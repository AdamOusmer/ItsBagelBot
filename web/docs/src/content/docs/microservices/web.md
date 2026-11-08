---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Marketing site
description: "Builds the public Astro site, localized product content and command builder."
docType: reference
---

## Responsibilities

The builder rehearses templates in the browser; it does not persist settings or execute live commands. Shared catalogs and preview logic live in `web/kit/`.

## Interfaces

Locale catalogs and page routing live in `src/i18n/`; guides and the command builder use shared Kit definitions. The builder’s samples illustrate template behavior and are not saved broadcaster state.

## State and dependencies

Kit provides shared catalogs and preview logic. The marketing site does not own broadcaster settings.

## Failure behavior

Builder examples are local previews and do not establish that a command was saved or executed.

## Code map

Source: [web/marketing/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/web/marketing). See `astro.config.mjs`, `src/i18n/`, `src/content/`, and `src/components/builder/`.

## Validation

From `web/marketing/`, use the scripts in `package.json` to build and check content.
