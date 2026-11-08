---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0005 - Elixir pour l’ingress Twitch"
description: "Décision d’architecture: Elixir pour l’ingress Twitch"
docType: decision
---

**Date:** 2026-05-23

**Statut:** Acceptée; remplace en partie l’[ADR 0001](/fr/adr/0001-rewrite-as-services/).

## Contexte

Le cycle de vie initial des connexions Twitch était difficile à superviser. L’[ADR 0002](/fr/adr/0002-go-for-services/) avait choisi Go comme langage principal des services.

## Décision

Nous avons choisi Elixir et OTP pour l’ingress Twitch. La supervision donne aux connexions longues un mécanisme explicite de récupération. Ce choix précise la partie ingress de l’[ADR 0001](/fr/adr/0001-rewrite-as-services/).

## Conséquences

Le projet maintient un second environnement d’exécution et une seconde chaîne d’outils. Le choix des services demeure en vigueur.

## Références

[Ingress](https://github.com/AdamOusmer/ItsBagelBot/blob/main/app/twitch/ingress/lib/ingress/application.ex), [Ingress Mix](https://github.com/AdamOusmer/ItsBagelBot/blob/main/app/twitch/ingress/mix.exs).
