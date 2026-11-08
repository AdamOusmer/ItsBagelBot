---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0004 - MySQL pour les données durables"
description: "Décision d’architecture: MySQL pour les données durables"
docType: decision
---

**Date:** 2026-05-23

**Statut:** Acceptée.

## Contexte

Les données relationnelles durables nécessitent une capacité de base de données. La décision initiale privilégiait une capacité gérée et moins d’opérations que l’auto-hébergement.

## Décision

Nous avons choisi MySQL pour les données relationnelles durables, avec des schémas propres aux services plutôt qu’un schéma applicatif partagé.

## Conséquences

La persistance et les migrations sont liées à MySQL. Les accès entre services passent par leurs contrats.

## Références

[Database](https://github.com/AdamOusmer/ItsBagelBot/tree/main/pkg/db), [Data services](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db).
