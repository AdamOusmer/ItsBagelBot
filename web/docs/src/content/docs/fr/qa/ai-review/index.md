---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "Revues de code par IA"
description: "Lectures d’un sous-système ou d’une pull request assistées par modèle, avec verdict du modèle et triage humain."
---

Chaque entrée ci-dessous est une revue datée d’un commit ou d’une pull request précise. Le modèle reçoit toujours la même consigne : signaler les bugs de correction, les problèmes de sécurité et les contrats faciles à mal utiliser. Sa sortie brute est conservée dans le rapport ; le triage humain figure dans une section séparée, afin qu’un futur lecteur distingue ce qui vient du modèle de ce qui vient de l’équipe.

## Rapports

| N° | Date | Périmètre | Commit | Modèle | Verdict |
|---|------|-------|--------|-------|---------|

*Aucune revue publiée pour le moment.*

## Pourquoi les conserver

Les revues par IA produisent du bruit : elles signalent de vrais bugs, des faux positifs et inventent parfois des éléments absents du diff. Publier le verdict brut avec le triage humain permet de répondre rétrospectivement à la question « le modèle a-t-il réellement aidé ici ? » avec des faits plutôt qu’une impression.

## Périmètre inclus

- Nouveaux microservices et sous-systèmes importants lors de leur première intégration.
- Changements justifiant un ADR : un changement disposant de son propre ADR est revu une fois son implémentation intégrée.
- Surfaces sensibles pour la sécurité : authentification, stockage de jetons, tout ce qui traite les identifiants des diffuseurs.

## Périmètre exclu

- Refactorisations, renommages et corrections du quotidien : le modèle produirait surtout du bruit.
- Code généré (mocks, protobufs, stubs OpenAPI) : le contrat appartient au schéma, pas au résultat généré.
