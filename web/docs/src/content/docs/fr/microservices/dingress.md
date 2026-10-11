---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "Dingress"
description: "Ingress Discord, le récepteur de passerelle du volet Discord, et sa transmission au moteur et à l’outgress."
---

Dingress (`app/discord/ingress/`, déployé sous le nom `discord-ingress`) est le récepteur de la **passerelle** Discord. Il détient la session Identify du jeton de bot de la flotte, enveloppe certains événements de la passerelle et les publie sur NATS. Il ne décide pas du comportement communautaire et n’exécute pas d’appels REST Discord généraux.

Le volet Discord comprend trois services et un service de données :

| Service | Chemin | Rôle |
|---|---|---|
| Discord Ingress (cette page) | `app/discord/ingress/` | Session de passerelle, relais des événements, accusé immédiat des interactions. |
| Discord Engine | `app/discord/engine/` | Décide quoi faire : accueil, tickets, salons vocaux, journaux d’audit, protection des liens, annonces de direct et de clips. Émet des commandes décrites ; n’appelle pas le REST Discord. |
| Discord Outgress | `app/discord/outgress/` | Exécute les commandes via le REST Discord, gère la mise en place et l’agencement des serveurs, enregistre les commandes slash. |
| Discord data | `app/db/discord/` | Possède les liaisons de serveurs, les réglages par serveur, les tickets et l’XP des membres. |

## Architecture

1. Ingress se connecte à la passerelle Discord avec le jeton de bot de la flotte et maintient la session (battements de cœur, reprise, reconnexion).
2. Pour chaque événement reconnu, il publie un événement enveloppé sur l’un de six sujets : `discord.ingress.event.{message,member,voice,interaction,audit,guild}`. Les types d’événement inconnus sont écartés. Ingress ne fait que publier ; c’est le Discord Engine qui crée le flux `DISCORD_INGRESS` et lie ses consommateurs.
3. Le moteur résout la liaison et les réglages du serveur (depuis Discord data par RPC, avec des caches Valkey) et publie des commandes sur `discord.outgress.mod` (modération urgente) ou `discord.outgress.default`.
4. Outgress consomme les deux voies, la modération d’abord, et appelle le REST Discord.

Deux réplicas tournent pour la disponibilité, mais un bail Valkey décide lequel détient le socket de la passerelle. Le réplica en attente n’en détient aucun et reprend à partir du point de reprise enregistré par le détenteur du bail, de sorte qu’un déploiement ne consomme pas un nouvel Identify. C’est pourquoi un même jeton de bot n’a jamais deux sessions actives.

### Accusé des interactions

Ingress fait lui-même un seul appel REST : pour `INTERACTION_CREATE`, il diffère l’interaction avant l’échéance de trois secondes de Discord. Si cet accusé échoue, l’événement est écarté. Le moteur et l’outgress terminent l’interaction plus tard par un message de suivi. L’accusé partage avec l’outgress le budget de débit Valkey de toute la flotte.

### Présence

Ingress définit la présence du bot à partir du RPC public de comptage des utilisateurs (`NATS_INTERNAL_USERS_COUNTS_SUBJECT`, par défaut `bagel.rpc.internal.users.counts.get`).

### Intents privilégiés

La session de passerelle demande les intents Guilds, Guild Members, Guild Moderation, Guild Expressions, Guild Invites, Guild Voice States, Guild Messages et Message Content. Server Members Intent et Message Content Intent sont privilégiés et doivent être activés sur l’application Discord. Sans eux, les messages d’accueil et les rangs de miettes ne se déclenchent jamais.

## Déploiement

`deploy/k8s/discord.yaml` exécute `discord-ingress` (2 réplicas), `discord-engine` (3) et `discord-outgress` (3), tous avec des mises à jour progressives. Leurs secrets proviennent du projet Doppler derrière le secret `discord-env`. `DISCORD_BOT_TOKEN` est le seul identifiant Discord et il parvient à ingress et à outgress ; il ne figure jamais dans un bloc de module. Un `DISCORD_BOT_TOKEN` vide laisse ingress inactif : la santé reste servie, la passerelle reste éteinte.

Le RPC de configuration Discord du tableau de bord garde son préfixe historique `bagel.rpc.dingress`, mais il est servi par Discord Outgress, pas par ce service.
