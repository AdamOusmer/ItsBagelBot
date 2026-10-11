---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Mise en place développeur
description: Vue d'ensemble de l'architecture et notes d'installation locale pour les contributeurs d'ItsBagelBot.
sidebar:
  order: 1
---

Vous configurez le bot sur votre chaîne ? Suivez plutôt le [guide pour streamers](https://itsbagelbot.com/fr/guides/getting-started/). Cette page s'adresse aux contributeurs.

Bienvenue dans **ItsBagelBot**, un écosystème cloud natif et performant pour Twitch.

Ce guide présente les concepts et les étapes nécessaires pour lancer l'environnement local de développement.

## Vue d'ensemble de l'architecture

ItsBagelBot utilise une architecture de microservices découplés :

* **Services backend :** propulsés par **Go** et **Elixir** (l'ingress Twitch).
* **Architecture événementielle :** **NATS JetStream** transporte les événements et le **request-reply NATS** transporte les RPC entre services.
* **Communication temps réel :** les événements Twitch arrivent par des shards **WebSocket** EventSub. Les webhooks d'achat Tebex sont le seul webhook entrant et arrivent sur le service transactions.
* **Applications web :**
    * **Tableau de bord et Admin :** applications SvelteKit dans `web/dashboard` et `web/admin`, servies par le cluster.
    * **Marketing et Docs :** applications Astro dans `web/marketing` et `web/docs`.
* **Infrastructure :** fonctionne sur **k3s** avec Cilium, des politiques réseau de refus par défaut, **Cloudflare Tunnel** pour l'entrée publique et **Tailscale** pour l'accès des opérateurs.

Voir la [vue d'ensemble du système](/fr/reference/system-overview/) et `CONTEXT-MAP.md` à la racine du dépôt pour la responsabilité de chaque service.

## Prérequis

* **Go** 1.26.5 (`go.mod`).
* **Bun** 1.4.2 (`packageManager` dans `web/package.json`).
* **Elixir** 1.17 ou plus récent, uniquement pour l'ingress Twitch (`app/twitch/ingress/mix.exs`).

## Installation locale

Services Go, depuis la racine du dépôt :

```sh
go test ./...
```

Applications web, depuis `web/` :

```sh
bun install --frozen-lockfile
bun run dev:dashboard
bun run dev:admin
bun run check
bun run test
```

Documentation, depuis `web/docs/` :

```sh
bun run dev
```

Ingress Twitch, depuis `app/twitch/ingress/` :

```sh
mix deps.get
mix test
```

Le fichier `context.md` de chaque application liste ses vérifications ciblées.
