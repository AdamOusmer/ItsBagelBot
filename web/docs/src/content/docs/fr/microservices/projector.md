---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "Projector"
description: "Construit et gère les modèles de lecture centralisés des réglages."
---

Le service Projector (`app/projector/`) construit la projection Valkey des réglages que les services consommateurs, principalement [Sesame](/fr/microservices/sesame/), utilisent pour les lectures à haut débit.

## Architecture

Au lieu de laisser les workers à fort trafic exécuter des requêtes SQL sur les différents services de données (Users, Modules, Commands), le Projector construit des modèles de lecture aplatis.

- **Déclenchement** : lorsqu’un stream passe en direct, via les événements ingress, le Projector préchauffe le cache.
- **Invalidation du cache** : les services de données émettent les événements `data.*.changed` et `bagel.cache.invalidate.broadcaster` lorsque les réglages changent. Le Projector les écoute pour reconstruire la projection.
- **Repli RPC** : les services dont le cache local est froid peuvent appeler le Projector par RPC NATS afin de récupérer la dernière projection de manière synchrone.

En isolant la charge de lecture dans une projection Valkey dédiée, les bases MySQL de configuration sont protégées du volume massif de lectures généré par les chats Twitch actifs.
