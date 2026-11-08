---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0008 - Projection de lecture partagée"
description: "Décision d’architecture: Projection de lecture partagée"
docType: decision
---

**Date:** 2026-06-09

**Statut:** Acceptée.

## Contexte

Les lectures partagées nécessitent des données de plusieurs propriétaires sans interroger chaque schéma à chaque lecture.

## Décision

Nous avons choisi une projection dans Valkey maintenue par le projector. Les propriétaires restent les sources de vérité; leurs événements et instantanés alimentent la projection.

## Conséquences

La projection peut être périmée ou indisponible et nécessite hydratation et réconciliation. Les consommateurs ne doivent pas la traiter comme source durable de vérité.

## Références

[Projector](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/projector), [Projection](https://github.com/AdamOusmer/ItsBagelBot/tree/main/internal/projection).
