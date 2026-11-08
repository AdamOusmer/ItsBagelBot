---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Mesurer les performances
description: Mesurer le parcours du code, préserver ses invariants et produire des résultats reproductibles.
docType: how-to
---

Un chiffre de débit exige une charge de travail définie. La taille des données, l’état du cache, le débit de sortie, la persistance, la réplication et la latence des dépendances changent tous le sens du résultat. Conservez ces éléments avec le résultat.


## Prérequis et résultat attendu

Choisissez l'opération et ses exigences de correction, notez le commit de référence et utilisez la chaîne Go indiquée par `go.mod`. Un benchmark local permet une comparaison reproductible pour son jeu de données; mesurer de vraies dépendances exige aussi ces services et leur configuration. Ne comparez pas des charges différentes comme si seul le code avait changé.

## 1. Exécuter une référence ciblée

Le dépôt contient des benchmarks du moteur dans `app/twitch/sesame/engine`, des workers dans `app/twitch/outgress/internal/worker` et du bus dans `pkg/bus/hotpath_bench_test.go`.

```sh
# Run from the repository root.
go test -run '^$' -bench . -benchmem ./app/twitch/sesame/engine
go test -run '^$' -bench . -benchmem ./app/twitch/outgress/internal/worker
```

Conservez la sortie et vérifiez que la commande se termine sans erreur. Lisez d’abord la configuration du benchmark. Une sortie en mémoire mesure le travail du moteur sans démontrer la latence de publication. Un faux serveur de plateforme mesure le chemin HTTP local sans démontrer la capacité de Twitch.

## 2. Mesurer la frontière concernée

Pour modifier une dépendance, réintroduisez la vraie frontière dans la charge : opérations Valkey réelles pour le limiteur ou la projection, publication avec accusé pour JetStream et données représentatives pour les codecs. `deploy/k8s/bus-bench` est un banc distinct destiné au déploiement; il n’est pas nécessaire pour travailler localement sur le moteur.

Notez le commit, la commande, la chaîne d’outils, le jeu de données, l’état chaud ou froid, la concurrence, la durée, les erreurs et la distribution des latences. Distinguez l’accusé de publication de l’achèvement de bout en bout. Indiquez le retard final ainsi que le débit d’envoi.

## 3. Modifier le code et préserver le comportement

- Les voies Premium et standard ont des règles d’admission distinctes. Un limiteur plus rapide doit préserver le compartiment consommé pour chaque refus.
- Une nouvelle tentative après une écriture au résultat ambigu peut dupliquer un effet. Conservez la politique de reprise du package partagé.
- Un raccourci de cache exige un chemin d’invalidation et de réparation. Incluez la première requête après éviction dans la comparaison.
- Une action périmée peut être inutile même si elle finit par réussir. Préservez les contrôles d’âge et d’expiration des messages.
- Un résultat local à un processus ne démontre pas une coordination à l’échelle de la flotte.

Répétez la même charge avant et après une modification. Examinez les profils d’allocations ou de CPU lorsqu’ils révèlent un véritable point chaud. Conservez les résultats datés comme preuves; ne transformez pas un ancien résultat de cluster en garantie de capacité actuelle.

[Tests](/fr/guides/testing/) traite des vérifications de correction. [Examiner la latence](/fr/infrastructure/observability/) traite de l’attribution du temps aux étapes.

## 4. Vérifier, comparer et résoudre les problèmes

Lancez les tests de correction pertinents avec le benchmark. Une commande réussie et un chiffre inférieur ne prouvent ni la correction ni la capacité de bout en bout. Si les résultats varient, vérifiez la concurrence, le préchauffage, l'état du cache, les autres charges, les erreurs et la latence des dépendances; répétez avec les mêmes paramètres avant de conclure. Conservez la commande et la sortie brute avec le commit et l'environnement pour permettre la reproduction. Utilisez [Examiner la latence](/fr/infrastructure/observability/) pour localiser une frontière lente.
