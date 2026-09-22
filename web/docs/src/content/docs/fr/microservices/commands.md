---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "Commands"
description: "Gère les commandes de chat personnalisées."
---

Le service Commands (`app/db/commands/`) assure les opérations CRUD (création, lecture, modification et suppression) des commandes de chat personnalisées définies par les diffuseurs.

## Architecture

Il s’agit d’un service de données standard de l’architecture ItsBagelBot :

- **Propriété** : il est le seul propriétaire du schéma MySQL des commandes personnalisées. Aucun autre service ne lit directement sa base.
- **Interface RPC** : il expose `bagel.rpc.commands.*` pour la gestion des commandes depuis le tableau de bord et `bagel.rpc.internal.projection.commands.get` pour les services internes.
- **Émission d’événements** : lorsqu’une commande est créée, modifiée ou supprimée, il écrit le changement en base, renvoie une réponse optimiste à l’appelant et émet de façon asynchrone `data.commands.changed`.
- **Invalidation du cache** : il publie sur `bagel.cache.invalidate.broadcaster` pour forcer Projector et Sesame à abandonner leurs caches périmés et à récupérer les nouvelles définitions.

## Exécution

Le service Commands **n’exécute pas** les commandes. Il en conserve uniquement les définitions. L’exécution est assurée par le moteur principal [Sesame](/fr/microservices/sesame/), qui intègre les commandes personnalisées dans sa table de routage aux côtés des commandes intégrées.
