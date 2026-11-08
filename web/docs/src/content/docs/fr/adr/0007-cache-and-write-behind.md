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

Nous utilisons des caches locaux et des écritures regroupées pour certains réglages que l’utilisateur peut soumettre de nouveau. Les données critiques empruntent des chemins de persistance immédiate.

## Conséquences

Une panne avant la vidange peut perdre des écritures différées et une lecture en cache peut être périmée. Chaque appelant doit choisir selon les besoins de durabilité et de cohérence.

## Références

[Cache](https://github.com/AdamOusmer/ItsBagelBot/tree/main/pkg/cache), [Batch](https://github.com/AdamOusmer/ItsBagelBot/tree/main/pkg/batch), [Modules repository](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db/modules/repository).
