---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Transactions
description: "Gère l’intégration Tebex et l’audit des webhooks, les paniers, les tirages, les gagnants et la remise des prix."
docType: reference
---

## Responsabilités

Le webhook `/tebex` et les RPC de transaction entrent dans ce service. Users gère l’admissibilité et les droits ; un prix promotionnel et un abonnement Tebex sont distincts.

## Interfaces

Le handler Tebex vérifie la taille et l’authenticité du corps avant son analyse. Les événements de facturation acceptés appellent Users de façon synchrone ; un échec permet à Tebex de réessayer. Un tirage persiste ensemble les prix et l’intention de livraison, puis un répartiteur gère les droits et la remise. La sélection et la remise confirmée sont deux états distincts.

## État et dépendances

Transactions possède les données de facturation et les prix ; Users gère les droits et l’admissibilité.

## Comportement en cas d’échec

Un échec d’application des droits permet à Tebex de réessayer. La sélection ne prouve pas à elle seule la remise du prix.

## Code

Source : [app/db/transactions/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db/transactions). Voir `main.go`, `tebex/`, `web/`, `giveaway/`, `rpc/` et `repository/`.

## Validation

Depuis la racine, exécuter `go test ./app/db/transactions/...`. Voir `tebex/` et `giveaway/`.
