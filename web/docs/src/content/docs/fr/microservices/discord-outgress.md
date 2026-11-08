---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Discord outgress
description: "Exécute les actions REST Discord et gère les opérations de configuration et de disposition."
docType: reference
---

## Responsabilités

Il consomme les actions décrites par Engine et sert les RPC de configuration et d’orchestration. Il utilise le limiteur Discord partagé et le stockage Discord par RPC. Ingress conserve l’exception d’acquittement immédiat des interactions.

## Interfaces

Les actions de modération utilisent `discord.outgress.mod` ; les actions ordinaires utilisent `discord.outgress.default`. Le worker consulte d’abord la modération, mais une course de sélection peut laisser passer une action ordinaire avant elle. Les commandes mal formées sont acquittées puis abandonnées ; les erreurs des handlers entraînent des reprises espacées et bornées. Les RPC de configuration conservent le préfixe historique `bagel.rpc.dingress` ; les RPC privées d’Engine utilisent `bagel.rpc.discord-outgress`.

## État et dépendances

Le limiteur Discord partagé coordonne la capacité REST ; les données durables restent dans Discord data.

## Comportement en cas d’échec

Les commandes mal formées sont abandonnées après acquittement. Les erreurs de traitement entraînent des reprises espacées et bornées.

## Code

Source : [app/discord/outgress/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/discord/outgress). Voir `main.go`, `internal/commands/`, `internal/setup/` et `internal/rpc/`.

## Validation

Depuis la racine, exécuter `go test ./app/discord/outgress/...`. Voir `internal/commands/` et `internal/rpc/`.
