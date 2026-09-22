---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Rapports QA
description: "Tests monkey et revues assistées par IA : des preuves datées et immuables du comportement d'ItsBagelBot à un instant donné."
---

Les rapports QA sont des **preuves**, pas de la documentation. Chaque rapport décrit le comportement d'un service, ou la lecture du code, sur un commit précis. Ils ne sont pas des guides et ne sont pas réécrits lorsqu'un bug découvert est corrigé.

Deux flux vivent dans cette section :

- **[Tests monkey →](/fr/qa/monkey/).** Exécutions aléatoires et fondées sur des propriétés contre un service en fonctionnement. Elles cherchent les crashs, deadlocks, bugs de cycle de vie et états inattendus sous charge.
- **[Revues de code IA →](/fr/qa/ai-review/).** Lectures assistées par modèle d'un sous-système ou d'une PR, comme une seconde paire d'yeux sur l'intention, la structure et la posture de sécurité. L'avis du modèle est conservé tel quel, avec le triage humain dans une section distincte.

Les audits de sécurité ne sont volontairement **pas** publiés ici. Un résumé assaini peut figurer sur la page d'état du projet ; les rapports bruts restent dans un dépôt privé. Publier les audits complets, même après correction, exposerait une surface d'attaque et des motifs de faiblesses de dépendances encore utiles à un attaquant.

## Lire un rapport

Chaque rapport commence par un **bloc de verdict** : date, périmètre, SHA du build, environnement, outil ou modèle, et verdict en une ligne (`Pass`, `Pass with notes`, `Regressed`, `Failed`). Parcourez d'abord ce bloc. La méthode complète, les résultats et les liens vers les artefacts suivent.

Les rapports sont **immuables** une fois publiés. Une exécution ultérieure avec un résultat différent reçoit son propre rapport et son numéro suivant. L'ancien reste la trace de ce qui était vrai à sa date, comme les [ADR](/fr/adr/) restent en place lorsqu'ils sont remplacés.

## Numérotation

Les noms suivent le modèle `NNNN-YYYY-MM-DD-<short-scope>.md`. Les numéros recommencent pour chaque sous-groupe afin de garder les deux flux indépendants, comme deux journaux d'ADR.

## Rédiger un rapport

Les modèles vivent avec celui des ADR :

- `docs/.qa/template-monkey.md`
- `docs/.qa/template-ai-review.md`

Copiez le modèle approprié dans le sous-dossier correspondant, incrémentez le numéro et commencez par remplir le bloc de verdict. Le reste doit pouvoir être rédigé à partir des artefacts bruts de l'exécution.

## Artefacts

Les journaux, traces et sorties brutes des modèles ne vivent pas dans le dépôt de documentation. Chaque rapport les référence et ils sont stockés hors site (bucket privé, Grafana interne). Cela garde le build léger et évite de publier des hôtes, des jetons ou des chemins internes.
