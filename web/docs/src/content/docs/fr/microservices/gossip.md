---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Gossip
description: "Fournit un accès RPC typé aux API externes, avec cache, budgets en amont et recherche des identifiants propres aux fournisseurs."
docType: reference
---

## Responsabilités

Les appelants demandent des données au lieu d’appeler directement les fournisseurs. Les routes HTTP fiables et non fiables ont des contrôles distincts ; les requêtes non fiables utilisent une résolution contrôlée et le proxy WARP, sans repli direct si celui-ci est indisponible.

## Interfaces

Le préfixe RPC par défaut est `bagel.rpc.gossip` ; les points d’accès des fournisseurs sont enregistrés dans `internal/providers/`.

Les fournisseurs sont enregistrés dans `internal/providers/` ; chacun déclare ses points d’accès et leur niveau de confiance avant de construire les clients. Une absence en cache diffère d’une panne d’infrastructure. Les requêtes personnalisées vérifient à nouveau les destinations et appliquent des budgets par chaîne, définition et hôte. La disponibilité du port WARP ne prouve pas le succès du trafic externe ; les fournisseurs fiables peuvent rester disponibles si la route non fiable tombe.

## État et dépendances

Les caches et budgets partagés servent les requêtes typées. Les identifiants restent chez leurs propriétaires et sont résolus au besoin.

## Comportement en cas d’échec

Les requêtes non fiables échouent si la route proxy est indisponible. Une absence en cache diffère d’une panne d’infrastructure.

## Code

Source : [app/gossip/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/gossip). Voir `main.go`, `internal/engine/`, `internal/providers/` et `internal/core/http.go`. La configuration figure dans `internal/config/`.

## Validation

Depuis la racine, exécuter `go test ./app/gossip/...` ; `internal/providers/custom/` couvre les requêtes personnalisées.
