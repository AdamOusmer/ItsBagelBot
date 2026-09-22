---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0001 - Réécriture en microservices"
description: "Document de décision d’architecture : réécriture en microservices"
---

**Date :** 2025-12-25

## Statut

Remplacé par [l’ADR 0006](/fr/adr/0006-adoption-of-elixir-for-twitch-ingress/)

## Contexte

La première version du projet (v1) a été développée dans un contexte où l’IA était un outil central pour accélérer le travail. Les principaux problèmes ne venaient pas de l’outil lui-même, mais de la manière dont il était utilisé.

Trois erreurs principales ont été commises avec cet outil :

- Lui laisser trop de liberté pour décider de la manière de construire le système.
- Ne pas lui fournir de contexte. L’IA n’avait pas accès au projet et était utilisée sur le web.
- Manquer de connaissances en réseau.

Ces problèmes ont entraîné une mauvaise qualité de code et un grand nombre d’anti-patterns, notamment le non-respect des principes ouvert/fermé et de substitution de Liskov. Le code est devenu un immense plat de spaghettis : une seule modification entraînait des changements dans au moins cinq fichiers et classes différents.

De plus, le code était conçu pour fonctionner localement avec un seul tenant. Étendre le projet au multi-tenant dans un délai raisonnable était irréaliste. Le code avait aussi été produit en Python, ce qui rendait difficile la montée en charge correcte d’un monolithe à cause du GIL.

La v1 présentait également un défaut majeur de réseau. La bibliothèque utilisée pour simplifier le développement était très instable et devait subir une réécriture massive lorsque Twitch est passé d’IRC à EventSub, avec des webhooks et des websockets. La v1 produisait alors souvent des processus zombies : le heartbeat n’arrivait pas et aucune tentative de reconnexion n’était effectuée. La bibliothèque masquait délibérément l’accès au websocket brut et à son cycle de vie, ce qui rendait la création d’un superviseur pénible et irréalisable.

Pour continuer à travailler sur le multi-tenant, il aurait fallu refactoriser toute la base de code. Avec le manque de connaissances, l’utilisation de l’IA aurait aggravé la situation et créé une dépendance dont la faisabilité du projet aurait elle-même dépendu.

En outre, la valeur ajoutée au portfolio aurait été moindre que celle du projet proposé de migration vers les microservices. Même si cette solution semble excessive, la scalabilité, l’isolation, la maintenabilité et la valeur ajoutée au portfolio lui donnent toute sa force.

## Décision

Nous allons procéder à une réécriture complète du projet tout en conservant le même dépôt afin de préserver la progression du projet dans le worktree. L’utilisation de l’IA sera fortement réduite pour favoriser l’apprentissage, maintenir une bonne qualité de code et promouvoir l’emploi de patterns de conception.

## Conséquences

- La réécriture complète prendra du temps et comporte le risque que le projet ne soit jamais terminé.
- Les besoins matériels vont fortement augmenter : en quittant le monolithe, une simple application sur DigitalOcean ne suffira plus.
- La valeur pour le portfolio et les connaissances acquises seront considérables.
- La scalabilité et la stabilité générale du projet augmenteront fortement.
- La modularité sera imposée partout et les mises à jour progressives ne provoqueront pas d’interruption.
- La complexité opérationnelle augmentera, car le débogage entre services, les transactions distribuées et la nécessité d’une pile d’observabilité feront partie du coût quotidien.
- Les appels réseau entre services ajouteront de la latence par rapport aux appels internes d’un monolithe, et la cohérence éventuelle introduira des cas limites qui n’existaient pas auparavant.

## Alternatives étudiées

- Continuer à travailler sur la v1 et la remanier progressivement. Rejeté, car le manque de connaissances en réseau maintiendrait l’IA comme dépendance et la bibliothèque instable nécessiterait toujours une réécriture complète.
- Créer un nouveau monolithe plus modulaire. Rejeté, car le GIL limiterait toujours la montée en charge et la valeur pour le portfolio du travail sur l’orchestration, les service meshes et l’observabilité serait perdue.
