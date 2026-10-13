---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "Commands"
description: "Gère les commandes de chat personnalisées, leurs compteurs d’utilisation et les définitions de requêtes externes."
---

Le service Commands (`app/db/commands/`) possède les commandes de chat personnalisées définies par les diffuseurs, leurs compteurs d’utilisation et les définitions nommées de requêtes externes (fetch) avec leurs clés d’API chiffrées.

## Architecture

Il s’agit d’un service de données standard de l’architecture ItsBagelBot :

- **Propriété** : il est le seul propriétaire du schéma MySQL des commandes personnalisées. Aucun autre service ne lit directement sa base.
- **Interface RPC** : il expose `bagel.rpc.commands.*` pour le tableau de bord (`list`, `upsert`, `delete` et les opérations `fetch_*` pour les requêtes externes), ainsi que `bagel.rpc.internal.projection.commands.get` (et `...commands.fetches.get`) pour des vues de lecture sans secret destinées aux services internes.
- **Émission d’événements** : les créations et modifications ordinaires sont acceptées aussitôt puis écrites par lots (fenêtre de 2 secondes ou 256 éléments). Le renommage et la suppression sont immédiats. Les changements validés sont publiés sur `data.commands.changed`, et ceux des requêtes externes sur `data.commands.fetch_changed`.
- **Compteurs d’utilisation** : Sesame signale chaque utilisation sur `data.commands.used`. Chaque lot est enregistré avec un reçu et son incrément dans une seule transaction SQL.
- **Invalidation du cache** : il diffuse sur `bagel.cache.invalidate.<portée>` pour que Projector et Sesame abandonnent leurs caches périmés et récupèrent les nouvelles définitions.
- **Clés de requêtes externes** : les clés d’API sont chiffrées au repos (`TINK_KEYSET_PATH`). Le tableau de bord ne voit que des métadonnées ; Gossip déchiffre une clé par un RPC interne quand il effectue la requête.

## Exécution

Le service Commands **n’exécute pas** les commandes. Il en conserve uniquement les définitions. L’exécution est assurée par le moteur principal [Sesame](/fr/microservices/sesame/), qui intègre les commandes personnalisées dans sa table de routage aux côtés des commandes intégrées. Gossip effectue les requêtes HTTP externes des requêtes personnalisées. L’affichage de la page publique des commandes d’une chaîne est un réglage de Users et ne désactive pas l’exécution dans le chat.
