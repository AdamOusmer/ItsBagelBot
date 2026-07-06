---
title: Web
description: Astro application for the public marketing site and documentation.
---

The Web (`web/`) project is an Astro application that houses the public-facing marketing material, landing pages, and potentially additional public web assets for ItsBagelBot.

## Architecture

- **Technology**: Built using [Astro](https://astro.build/) for static site generation (SSG) and fast page loads, along with `Bun` as the package manager and runtime.
- **Separation of Concerns**: Kept intentionally separate from the [Console](/microservices/console/) (which requires active server-side rendering and NATS integration). The web project is purely for static content, SEO, and presenting the bot to new users.
- **Documentation**: The documentation itself (which you are reading now) is built with Astro's Starlight template in the `docs/` folder, closely mirroring the technical foundation of the main `web/` site.
