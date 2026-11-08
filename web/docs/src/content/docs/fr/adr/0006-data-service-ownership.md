---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0006 - Propriété des données par service"
description: "Décision d’architecture: Propriété des données par service"
docType: decision
---

**Date:** 2026-06-09

**Statut:** Acceptée.

## Contexte

Les données de domaines différents nécessitent une propriété explicite. Users, commands, modules et transactions formaient les frontières initiales; le code comprend maintenant d’autres services de données.

## Décision

Les services de données possèdent leurs schémas et exposent leurs changements par des contrats plutôt que de permettre aux autres services de joindre ou modifier leurs tables.

## Conséquences

Les processus et suppressions entre services sont éventuellement cohérents au lieu de former une seule transaction.

## Références

[Data services](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db), [Contracts](https://github.com/AdamOusmer/ItsBagelBot/tree/main/internal/domain).
