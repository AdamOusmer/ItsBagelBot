---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "Modules"
description: "Gère l’activation et la configuration des fonctionnalités par diffuseur."
---

Le service Modules (`app/db/modules/`) gère la configuration et l’état d’activation des modules fonctionnels pour chaque diffuseur.

## Architecture

Comme les autres services de données, Modules suit un modèle de propriété strict :

- **Propriété** : propriétaire unique du schéma MySQL des modules.
- **Interface RPC** : sert les réglages du tableau de bord via sa file RPC NATS et fournit les recherches internes via `bagel.rpc.internal.projection.modules.get`.
- **Émission d’événements** : émet `data.modules.changed` lors des mises à jour.
- **Invalidation du cache** : publie `bagel.cache.invalidate.broadcaster` lorsqu’un module est activé, désactivé ou reconfiguré, afin que le moteur Sesame adapte immédiatement son pipeline d’exécution.

## Configuration des modules

Les modules d’ItsBagelBot sont configurables par chaîne. Ce service conserve les blocs de configuration JSON et les booléens d’activation (`IsEnabled`) associés aux identités de module déclarées dans le moteur [Sesame](/fr/microservices/sesame/). Lorsqu’il traite un événement, Sesame consulte sa projection locale `ModuleView`, alimentée par ce service, pour déterminer si les handlers ou commandes du module doivent s’exécuter.
