---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Examiner un constat candidat
description: Valider un candidat à partir d’un déclencheur concret et de preuves.
docType: how-to
---

Cette procédure distingue un constat proposé par un modèle d’une conclusion validée.

## Prérequis

Identifiez le code concerné et le périmètre de la revue. Le modèle se trouve dans `web/docs/.qa/template-ai-review.md`. Des revues de travail ciblées se trouvent dans `docs/reviews/`; vérifiez leur périmètre et leur date avant de considérer une conclusion comme actuelle.

## Procédure

1. Identifiez le code concerné et l’entrée ou l’état précis qui déclenche le problème candidat.
2. Suivez le chemin de code ou exécutez un test adapté pour établir la conséquence observée. Si du code généré est concerné, examinez le schéma propriétaire ou le contrat partagé.
3. Consignez si le candidat est confirmé, rejeté ou laissé ouvert, ainsi que les preuves et le raisonnement.
4. Après correction, liez le changement et la validation qui couvre le déclencheur.

## Vérifier et consigner

Vérifiez qu’une autre personne peut suivre le déclencheur, le code et les preuves jusqu’à la conclusion. Un paragraphe affirmatif ne constitue pas une preuve. Suivez les conseils sur les [preuves de validation](/fr/qa/) et consultez les [tests](/fr/guides/testing/) pour les commandes.

## Limites

Distinguez les candidats proposés par un modèle de leur validation humaine. Aucun rapport numéroté de revue par IA n’est publié dans cette section.
