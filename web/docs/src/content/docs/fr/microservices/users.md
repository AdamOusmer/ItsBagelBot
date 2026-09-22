---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "Users"
description: "Gère les comptes utilisateurs, les jetons OAuth et les niveaux de statut."
---

Le service Users (`app/db/users/`) est l’autorité centrale de gestion des identités dans ItsBagelBot.

## Architecture

- **Propriété** : possède le schéma MySQL Users contenant les identifiants Twitch, les jetons OAuth et les statuts des comptes.
- **Niveaux de statut** : détermine si un compte est `free`, `paid` ou `vip`, ce qui régit son accès aux fonctionnalités Premium ou aux voies de routage.
- **Coffre de jetons** : gère les jetons OAuth Twitch sensibles. Il sert de coffre interne ; les autres services, comme Outgress, interrogent Users de façon sécurisée par RPC NATS (`bagel.rpc.internal.tokens.*`) afin d’obtenir un accès temporaire pour agir au nom de l’utilisateur.

## Authentification entre services

L’autorisation NATS restreint l’accès aux sujets `bagel.rpc.internal.tokens.*`. Seuls les services autorisés, Outgress et Users lui-même, peuvent accéder à ces sujets, ce qui protège strictement les jetons en clair au sein du mesh du cluster.
