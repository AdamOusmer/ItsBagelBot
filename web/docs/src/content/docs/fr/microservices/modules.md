---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "Modules"
description: "Gère les réglages des modules par diffuseur, les citations, les compteurs d’alimentation et les identifiants d’intégrations."
---

Le service Modules (`app/db/modules/`) gère la configuration et l’état d’activation des modules fonctionnels pour chaque diffuseur. Il conserve aussi les carnets de citations des chaînes, les totaux à vie de l’alimentation des bagels et les identifiants Govee et Spotify chiffrés.

## Architecture

Comme les autres services de données, Modules suit un modèle de propriété strict :

- **Propriété** : propriétaire unique du schéma MySQL des modules.
- **Interface RPC** : sert le tableau de bord sous `bagel.rpc.modules.*` (`list`, `upsert`, `patch`, `patch-existing`, plus les opérations de citations et de personnalité) et fournit des vues de réparation sans secret via `bagel.rpc.internal.projection.modules.get`.
- **Émission d’événements** : émet `data.modules.changed` avec la révision enregistrée. Un `upsert` du tableau de bord est écrit avant le retour de la réponse ; les écritures en arrière-plan sont groupées.
- **Invalidation du cache** : diffuse sur `bagel.cache.invalidate.<portée>` lorsqu’un module est activé, désactivé ou reconfiguré, afin que le moteur Sesame adapte son pipeline d’exécution.
- **Identifiants** : les clés Govee et l’application et les jetons de rafraîchissement Spotify sont chiffrés dans des tables séparées et n’entrent jamais dans les blocs de configuration ni dans les projections. Gossip les lit par des RPC internes de déchiffrement.

## Configuration des modules

Les modules d’ItsBagelBot sont configurables par chaîne. Ce service conserve les blocs de configuration JSON et les booléens d’activation (`IsEnabled`) associés aux identités de module déclarées dans le moteur [Sesame](/fr/microservices/sesame/). Lorsqu’il traite un événement, Sesame consulte sa projection locale `ModuleView`, alimentée par ce service, pour déterminer si les handlers ou commandes du module doivent s’exécuter.
