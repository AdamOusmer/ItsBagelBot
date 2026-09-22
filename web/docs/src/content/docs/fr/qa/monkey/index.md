---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "Tests aléatoires"
description: "Exécutions aléatoires et tests de propriétés sur les services actifs pour trouver plantages, interblocages et états inattendus."
---

Chaque entrée ci-dessous est un rapport d’exécution daté. Ouvrez le rapport pour consulter le verdict, la méthode, les constats et les liens vers les artefacts. Voir [Rapports QA](/fr/qa/) pour les conventions de lecture et de rédaction communes aux deux catégories.

## Rapports

| N° | Date | Périmètre | Build | Environnement | Verdict |
|---|------|-------|-------|-------------|---------|

*Aucune exécution publiée pour le moment. Le premier rapport sera ajouté après la première campagne de tests aléatoires de Twitch Ingress.*

## Tests réalisés

- **Chaos du cycle de vie.** Arrêter, suspendre et ralentir certains processus d’un service. Vérifier que le superviseur rétablit le fonctionnement dans le délai annoncé.
- **Fuzzing des entrées.** Rejouer un flux Twitch EventSub enregistré, avec des trames altérées par inversion de bits, tronquées ou désordonnées, sur un locataire de préproduction.
- **Pics de charge.** Envoyer de brèves rafales d’événements à haut débit pour révéler la gestion de la contre-pression et des débordements de file.

## Tests exclus

- **Tout ce qui touche un véritable diffuseur.** Les tests aléatoires utilisent des locataires de préproduction avec des autorisations OAuth synthétiques. Une campagne qui atteint le chat d’un diffuseur en direct révèle un bug de l’outil de test.
- **Tests d’endurance prolongés.** Ils relèvent de la tâche CI nocturne. Cette catégorie est réservée aux exécutions bornées et reproductibles qui produisent un ensemble unique d’artefacts.
