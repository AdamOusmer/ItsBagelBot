---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0001 - Réécrire en services"
description: "Décision d’architecture: Réécrire en services"
docType: decision
---

**Date:** 2025-12-25

**Statut:** Remplacée en partie par l’[ADR 0005](/fr/adr/0005-elixir-twitch-ingress/); la réécriture et les frontières entre services demeurent.

## Contexte

La première version était difficile à étendre à plusieurs locataires et le cycle de vie de sa connexion Twitch était difficile à superviser.

## Décision

Nous avons choisi une réécriture en services dans le même dépôt pour l’isolation, la maintenabilité, la mise à l’échelle et l’apprentissage. L’[ADR 0005](/fr/adr/0005-elixir-twitch-ingress/) précise le choix du langage et de la supervision de l’ingress.

## Conséquences

Les opérations distribuées, la latence réseau et la cohérence éventuelle font désormais partie du système.

## Références

[Services](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app).
