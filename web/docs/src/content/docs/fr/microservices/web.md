---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Web
description: Application Astro du site marketing public, à côté du site de documentation.
---

Le projet Web (`web/marketing/`) est une application Astro qui construit le site public du produit : pages de fonctionnalités et de tarifs, guides localisés, constructeur de commandes, journal des modifications et pages légales.

## Architecture

- **Technologie** : construit avec [Astro](https://astro.build/) en pages statiques localisées, dans l’espace de travail Bun `web/`. L’anglais n’a pas de préfixe ; les autres langues utilisent `/<code>/`.
- **Séparation des responsabilités** : volontairement distinct de la [Console](/fr/microservices/console/) et d’[Admin](/fr/microservices/admin/), des applications SvelteKit qui exigent un rendu côté serveur et l’accès à NATS. Le site marketing ne conserve aucun réglage de diffuseur et n’exécute aucune commande du bot. Le constructeur de commandes simule les modèles dans le navigateur et renvoie vers le tableau de bord.
- **Documentation** : la documentation elle-même (celle que vous lisez) est construite avec le modèle Starlight d’Astro dans `web/docs/`, un paquet distinct du même espace de travail.
