---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "Projector"
description: "Construit et sert les modèles de lecture Valkey partagés : réglages, état en direct et compteurs en direct."
---

Le service Projector (`app/projector/`) intègre les événements des services de données dans des modèles de lecture Valkey partagés et sert des lectures orientées cache. Les services consommateurs, principalement [Sesame](/fr/microservices/sesame/), les utilisent pour les lectures à haut débit.

## Architecture

Au lieu de laisser les workers à fort trafic exécuter des requêtes SQL sur les différents services de données (Users, Modules, Commands), le Projector construit des modèles de lecture aplatis.

- **Intégration des événements** : Users, Commands et Modules publient `data.users.changed`, `data.users.deleted`, `data.modules.changed` et `data.commands.changed`. Le Projector les consomme, écrit la projection Valkey, puis diffuse une invalidation sur `bagel.cache.invalidate.<portée>` (par exemple `status`, `modules`, `commands`, `live`) pour que chaque cache de processus abandonne sa copie.
- **Cycle de vie du stream** : lorsqu’un stream passe en direct (`twitch.ingress.event.stream`), le Projector enregistre l’état en direct puis préchauffe la projection complète en arrière-plan.
- **Hydratation** : une lecture à froid interroge le service propriétaire via `bagel.rpc.internal.projection.*` et renvoie cette section. Les sections manquantes sont remplies en arrière-plan.
- **Lectures** : il sert `bagel.rpc.broadcaster.status.get`, `bagel.rpc.broadcaster.live.get`, `bagel.rpc.broadcaster.stream_info.get` et les lectures du tableau de bord sous `bagel.rpc.projector.dashboard.*`.
- **Compteurs en direct** : il intègre les incréments `data.loyalty.counters` dans des totaux en direct par bot et par chaîne, et initialise les classements à partir de Loyalty.

En isolant la charge de lecture dans une projection Valkey dédiée, les bases MySQL de configuration sont protégées du volume massif de lectures généré par les chats Twitch actifs.
