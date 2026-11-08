---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: AutoMod
description: "La modération des messages entrants est implémentée dans Sesame. app/twitch/automod/ est un emplacement réservé, pas un service Rust en cours d’exécution."
docType: reference
---

## Responsabilités

Sesame évalue les messages entrants et décrit les actions à exécuter pour Twitch Outgress. L’application automatique est désactivée par défaut. Sesame vérifie aussi les réponses sortantes complètes ; Projector ne possède pas d’API de journal AutoMod.

## Interfaces

Le filtre évalue les règles et indices configurés dans `automod/` ; `engine/moderate.go` relie les décisions au pipeline de Sesame. L’application produit des actions typées pour Outgress au lieu d’appeler Twitch directement. La vérification minimale des réponses sortantes est une étape distincte.

## État et dépendances

L’implémentation s’exécute dans Sesame ; le répertoire séparé `app/twitch/automod/` est un emplacement réservé.

## Comportement en cas d’échec

L’application automatique est désactivée par défaut ; les actions décrites passent par Outgress.

## Code

Source : [app/twitch/sesame/automod/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/twitch/sesame/automod). Voir `automod/gate.go`, `automod/config.go`, `engine/moderate.go` et `modules/automod.go` dans `app/twitch/sesame/`.

## Validation

Depuis la racine, exécuter `go test ./app/twitch/sesame/automod/... ./app/twitch/sesame/engine/...`.
