---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Exécuter des vérifications aléatoires
description: Rendre reproductibles les échecs issus de vérifications aléatoires.
docType: how-to
---

Utilisez des entrées aléatoires pour vérifier un invariant choisi et conservez les informations nécessaires à la reproduction d’un échec.

## Prérequis

Choisissez le code, le jeu de données et l’invariant à tester. Commencez par les [tests et benchmarks](/fr/guides/testing/) du dépôt. Simulez les effets sur les plateformes ou utilisez un compte de test isolé.

## Procédure

1. Consignez le commit source, la graine, le jeu de données, les limites et la commande exacte de l’exécution.
2. Exercez les événements mal formés, doublons, annulations, reconnexions et pressions sur les files bornées pertinents.
3. Vérifiez un invariant, comme l’absence de prix attribué deux fois ou d’action après expiration, plutôt que la seule survie du processus.
4. En cas d’échec, conservez les preuves initiales, réduisez le scénario à un cas reproductible et ajoutez un test de régression ciblé.

## Vérifier et consigner

Reproduisez l’échec avec les entrées consignées et exécutez le test de régression. Un futur rapport peut utiliser le nom `NNNN-YYYY-MM-DD-scope.md`, avec la graine et la commande de reproduction près du début. Voir les [preuves de validation](/fr/qa/) pour les autres éléments à conserver.

## Limites

Ce guide ne désigne pas de banc de tests aléatoires et n’affirme pas qu’une exécution a eu lieu. Aucun rapport numéroté de tests aléatoires n’est publié dans cette section.
