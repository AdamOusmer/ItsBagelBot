---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0010 - Kit possède l’inventaire public des variables"
description: "Décision d’architecture: Kit possède l’inventaire public des variables"
docType: decision
---

**Date:** 2026-09-18

**Statut:** Acceptée et mise en œuvre.

## Contexte

Les inventaires et exemples de variables de réponse avaient divergé entre le code Go et le web.

## Décision

Le résolveur de portée Go définit le comportement des jetons; le manifeste kit définit l’inventaire public, les formes, exemples et clés de texte utilisés par les interfaces web. Un fichier de référence partagé vérifie leur parité.

## Conséquences

Le CI web n’a pas à générer le manifeste depuis Go, mais les changements de variables nécessitent une mise à jour des deux inventaires.

## Références

[Go resolver](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/twitch/sesame/engine/scope), [Kit manifest](https://github.com/AdamOusmer/ItsBagelBot/tree/main/web/kit/lib/variables).
