---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Loyalty
description: "Gère les soldes de fidélité, le temps de visionnement et les compteurs des chaînes."
docType: reference
---

## Responsabilités

Il consomme les événements de gains et de compteurs ainsi que ceux du cycle de vie des comptes. Un consommateur de temps de visionnement applique les gains ; les RPC exposent les opérations et lectures. Projector maintient des vues dérivées des compteurs en direct.

## Interfaces

Le préfixe RPC par défaut est `rpc.loyalty`. Le service consomme les événements de gains, de compteurs et du cycle de vie des comptes.

Les anciens événements de gains s’accumulent en mémoire : un gain accepté peut manquer dans une lecture immédiate. Les compteurs sont acquittés après traitement SQL, avec un reçu Valkey temporaire : un arrêt entre SQL et ce reçu peut entraîner un double comptage. Les gains de visionnement utilisent une boîte d’envoi distincte ; déduplication et crédit sont persistés ensemble, et une erreur laisse la livraison en attente. Ces voies ne garantissent pas toutes une exécution unique.

## État et dépendances

SQL conserve les soldes et compteurs ; Valkey participe à la livraison du temps de visionnement et aux reçus de compteurs.

## Comportement en cas d’échec

Un arrêt entre SQL et le reçu peut entraîner un double comptage. Une erreur de gain de visionnement laisse la livraison en attente.

## Code

Source : [app/db/loyalty/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db/loyalty). Voir `main.go`, `repository/`, `rpc/` et `internal/watchtime/`.

## Validation

Depuis la racine, exécuter `go test ./app/db/loyalty/... ./internal/watchtime/...`.
