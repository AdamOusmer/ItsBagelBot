---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Modules
description: "Gère la configuration des modules par chaîne, les citations, l’état des fonctionnalités et les identifiants d’intégration."
docType: reference
---

## Responsabilités

Le tableau de bord écrit auprès du service propriétaire ; Sesame lit les projections. Gossip résout les identifiants au besoin. Les changements alimentent Projector ; un module enregistré dans le code n’est pas forcément activé pour une chaîne.

## Interfaces

Le préfixe RPC par défaut est `rpc.modules`, notamment `list`, `upsert`, `patch` et `patch-existing`. La réparation des projections utilise `rpc.internal.projection.modules.get`.

Les écritures du tableau de bord sont persistées avant la réponse ; celles en arrière-plan peuvent être regroupées. Une modification avec une révision attendue périmée renvoie un conflit. `patch-existing` exige aussi l’identifiant attendu et ne crée jamais de remplacement. Les changements persistés invalident les caches locaux et la RPC interne de projection renvoie une vue sans secrets.

## État et dépendances

Modules possède les réglages des chaînes et les identifiants d’intégration ; Projector conserve des vues dérivées sans secrets.

## Comportement en cas d’échec

Une révision attendue périmée produit un conflit. Les écritures en arrière-plan peuvent rester en attente après acceptation.

## Code

Source : [app/db/modules/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db/modules). Voir `main.go`, `rpc/`, `repository/` et `ent/schema/`.

## Validation

Depuis la racine, exécuter `go test ./app/db/modules/...`.
