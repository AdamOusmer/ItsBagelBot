---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0009 - Transactions possède les prix de concours"
description: "Décision d’architecture: Transactions possède les prix de concours"
docType: decision
---

**Date:** 2026-09-15

**Statut:** Acceptée et mise en œuvre; les chemins d’attribution ont des contrôles distincts.

## Contexte

L’obligation liée à un prix doit survivre aux retards d’accès, de protection de facturation ou de courriel.

## Décision

Transactions possède les tirages, les dossiers des gagnants et l’historique d’attribution. Users fait autorité sur l’identité et l’accès effectif.

## Conséquences

Les services se coordonnent par des contrats idempotents. La récupération doit tenir compte des opérations validées dans des bases distinctes.

## Références

[Transactions](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db/transactions/giveaway), [Users](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db/users).
