---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Preuves de validation
description: Référence pour consigner et interpréter les résultats de validation.
docType: reference
---

Les tests vérifient un comportement, les revues expliquent des constats et les benchmarks décrivent des charges de travail. Gardez ces affirmations distinctes. Une note de conception ou de déploiement ne prouve pas que son plan a été exécuté.

## Consigner les preuves de validation

Indiquez le commit source, la date, le périmètre, la commande ou méthode, l’environnement et le résultat. Liez les preuves conservées et précisez les dépendances réelles, celles simulées et le comportement non testé. Distinguez le constat candidat de sa conclusion validée et de sa correction. Consignez les exécutions ultérieures comme de nouveaux résultats sans remplacer silencieusement les preuves précédentes. Ne publiez ni secrets ni charges utiles privées.

## Guides et sources

- [Exécuter des vérifications aléatoires](/fr/qa/monkey/) pour rendre les échecs générés reproductibles.
- [Examiner un constat candidat](/fr/qa/ai-review/) pour documenter son déclencheur et ses preuves.
- [Valider la livraison Twitch](/fr/qa/outgress-hot-path-canary/) pour vérifier le parcours Outgress.
- Les [tests](/fr/guides/testing/) fournissent des commandes; les [travaux de performance](/fr/infrastructure/performance/) traitent des mesures.

Les modèles se trouvent dans `web/docs/.qa/`. Le dépôt contient aussi des documents ciblés dans `docs/reviews/`, `docs/specs/`, `docs/runbooks/` et `docs/operations/`; vérifiez leur périmètre et leur date. Un test local réussi ne certifie pas un canari en production.
