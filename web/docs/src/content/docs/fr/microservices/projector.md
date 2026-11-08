---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Projector
description: "Maintient des modèles de lecture Valkey à partir des événements des propriétaires et sert les lectures de statut, de direct, de diffusion et du tableau de bord."
docType: reference
---

## Responsabilités

Il applique les événements des comptes, commandes, modules, automatisations et compteurs. L’écriture précède l’invalidation du cache ; les données invalides sont acquittées et les erreurs de stockage entraînent une nouvelle livraison. Une lecture à froid peut interroger le propriétaire et une hydratation en arrière-plan répare les sections manquantes. Les révisions protègent les données récentes.

## Interfaces

Un consommateur durable valide les événements, met à jour l’état partagé puis diffuse les invalidations. Une section absente du tableau de bord peut être obtenue de son propriétaire ; l’hydratation asynchrone répare le cache sans bloquer toutes les lectures. Les écritures de cycle de vie précèdent l’acquittement ; les travaux consécutifs au début d’un direct sont exécutés au mieux. Projector n’écrit pas la clé `live:<id>` de Sesame.

## État et dépendances

Projector stocke des vues dérivées dans Valkey ; les services de données restent les sources de vérité.

## Comportement en cas d’échec

Les événements invalides sont acquittés ; les erreurs de stockage entraînent une nouvelle livraison. Les contrôles de révision protègent les mises à jour récentes.

Un propriétaire de données indisponible dégrade le diagnostic sans retirer Projector du service. Les sondes des propriétaires s’exécutent en parallèle avec un délai de 500 ms chacune : `/readyz` conserve HTTP 200, tandis que `/status` renvoie HTTP 207 avec le propriétaire en échec. Les projections déjà présentes et le travail des autres propriétaires continuent ; une lecture à froid nécessitant le propriétaire indisponible peut encore échouer. La connexion NATS, l’abonnement RPC et le consommateur de flux de Projector restent obligatoires pour sa disponibilité. `/healthz` reste limité au processus.

## Code

Source : [app/projector/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/projector). Voir `main.go`, `hydration/`, `rpc/` et `internal/projection/`.

## Validation

Depuis la racine, exécuter `go test ./app/projector/... ./internal/projection/...`. Voir `hydration/` et `rpc/live.go` pour l’ordre et les lectures.
