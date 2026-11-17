---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0007 - Cache et écriture différée"
description: "Décision d’architecture: Cache et écriture différée"
docType: decision
---

**Date:** 2026-06-09

**Statut:** Acceptée.

## Contexte

Les lectures et écritures répétées de réglages peuvent surcharger inutilement la base de données.

## Décision

Nous utilisons des caches locaux et des écritures regroupées pour certains réglages. Les commandes et préférences attendent désormais le commit de leur transaction MySQL groupée avant de répondre positivement. Les modules et données critiques empruntent des chemins synchrones. Voir le [contrat de durabilité actuel](/fr/data-and-state/caching/).

## Conséquences

Une panne avant le commit impose une nouvelle tentative; une réponse positive confirme la persistance. Une lecture en cache peut être périmée. Les compteurs utilisent des reçus Valkey pendant 70 minutes comme chemin rapide, ainsi que des reçus SQL durables, pour rendre les tentatives idempotentes pendant la durée de rétention des reçus SQL. Les erreurs attribuables à un élément sont isolées des écritures saines.

## Références

[Cache](https://github.com/AdamOusmer/ItsBagelBot/tree/main/pkg/cache), [Batch](https://github.com/AdamOusmer/ItsBagelBot/tree/main/pkg/batch), [Modules repository](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db/modules/repository).
