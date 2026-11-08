---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0002 - Go pour les services"
description: "Décision d’architecture: Go pour les services"
docType: decision
---

**Date:** 2026-01-04

**Statut:** Acceptée; précisée par l’[ADR 0005](/fr/adr/0005-elixir-twitch-ingress/).

## Contexte

La réécriture nécessitait un langage de service adapté au travail concurrent et à une empreinte modeste.

## Décision

Nous avons choisi Go comme langage principal des services pour son modèle de concurrence, son empreinte et ses caractéristiques de déploiement.

## Conséquences

Le projet doit maintenir Go avec d’autres écosystèmes. L’ingress Twitch a ensuite adopté Elixir pour la supervision ([ADR 0005](/fr/adr/0005-elixir-twitch-ingress/)).

## Références

[Go module](https://github.com/AdamOusmer/ItsBagelBot/blob/main/go.mod), [Twitch ingress](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/twitch/ingress).
