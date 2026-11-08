---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0003 - NATS pour la communication entre services"
description: "Décision d’architecture: NATS pour la communication entre services"
docType: decision
---

**Date:** 2026-05-23

**Statut:** Acceptée.

## Contexte

Les services ont besoin de requêtes-réponses, de livraison d’événements et de coordination sans exploiter un système distinct pour chaque usage.

## Décision

Nous avons choisi NATS pour les requêtes-réponses, la livraison d’événements JetStream et la coordination.

## Conséquences

NATS devient une dépendance commune sur des chemins critiques. JetStream peut livrer un événement plusieurs fois : les consommateurs doivent gérer les reprises. La rétention est limitée et le bus ne fait pas autorité sur les données.

## Références

[Bus](https://github.com/AdamOusmer/ItsBagelBot/tree/main/pkg/bus), [Contracts](https://github.com/AdamOusmer/ItsBagelBot/tree/main/internal/domain).
