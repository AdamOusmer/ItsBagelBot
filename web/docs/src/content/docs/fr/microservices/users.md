---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "Users"
description: "Gère les comptes utilisateurs, la garde des jetons Twitch, le statut d’accès, le staff et la délégation."
---

Le service Users (`app/db/users/`) est l’autorité centrale de gestion des identités dans ItsBagelBot.

## Architecture

- **Propriété** : possède le schéma MySQL Users contenant les identifiants Twitch, les préférences, les jetons OAuth et l’e-mail de contact chiffrés, les statuts des comptes, les attributions Premium promotionnelles, les rôles du staff avec un journal d’audit et les délégations du tableau de bord.
- **Niveaux de statut** : détermine si un compte est `free`, `paid` ou `vip`, ce qui régit son accès aux fonctionnalités Premium ou aux voies de routage. `paid` peut venir d’un abonnement Tebex ou d’une attribution de concours ; `vip` est permanent.
- **Événements** : publie `data.users.changed` (avec une révision d’état) et `data.users.deleted`, et diffuse des invalidations de cache. Les services fonctionnels nettoient eux-mêmes leurs données après une suppression.
- **RPC** : sert le tableau de bord (`bagel.rpc.dashboard.*`), les opérations admin sur les comptes (`bagel.rpc.admin.user.*`), l’application de la facturation (`bagel.rpc.internal.billing.apply`), l’éligibilité et les attributions des concours, la délégation et la projection sans secret (`bagel.rpc.internal.projection.users.get`).
- **Coffre de jetons** : gère les jetons OAuth Twitch sensibles. Il sert de coffre interne ; les autres services, comme Outgress, interrogent Users de façon sécurisée par RPC NATS (`bagel.rpc.internal.tokens.*`, opérations `get` et `save`) afin d’obtenir un accès pour agir au nom de l’utilisateur. Les jetons et les e-mails de contact sont chiffrés au repos.

## Authentification entre services

L’autorisation NATS restreint l’accès aux sujets `bagel.rpc.internal.tokens.*`. Seuls les services autorisés, Twitch Outgress et Users lui-même, peuvent accéder à ces sujets, ce qui protège strictement les jetons en clair au sein du cluster.
