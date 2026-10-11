---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Web
description: Astro application for the public marketing site, next to the documentation site.
---

The Web project (`web/marketing/`) is an Astro application that builds the public product site: feature and pricing pages, localized guides, a command builder, a changelog, and legal pages.

## Architecture

- **Technology**: Built with [Astro](https://astro.build/) into static, localized pages, in the `web/` Bun workspace. English is unprefixed; other locales use `/<code>/`.
- **Separation of Concerns**: Kept intentionally separate from the [Console](/microservices/console/) and [Admin](/microservices/admin/) (SvelteKit apps that need server-side rendering and NATS access). The marketing site does not store broadcaster settings or run bot commands. The command builder rehearses templates in the browser and links to the dashboard.
- **Documentation**: The documentation itself (which you are reading now) is built with Astro's Starlight template in `web/docs/`, a separate package in the same workspace.
