---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Web
description: Application Astro du site marketing public et de la documentation.
---

Le projet Web (`web/`) est une application Astro qui héberge le contenu marketing public, les pages d'accueil et d'autres ressources web publiques potentielles d'ItsBagelBot.

## Architecture

- **Technologie :** construit avec [Astro](https://astro.build/) pour la génération statique (SSG) et des chargements rapides, avec **Bun** comme gestionnaire de paquets et runtime.
- **Séparation des responsabilités :** séparé volontairement de la [Console](/fr/microservices/console/), qui nécessite le rendu côté serveur et l'intégration NATS. Le projet Web se limite au contenu statique, au SEO et à la présentation du bot.
- **Documentation :** la documentation que vous lisez est construite avec le template Starlight d'Astro dans le dossier `docs/`, en reprenant les fondations techniques du site `web/` principal.
