---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Dashboard
description: "Fournit l’intégration des chaînes, leur configuration, la gestion du compte et les pages publiques."
docType: reference
---

## Responsabilités

Les routes serveur appellent les services propriétaires par RPC. Les délégués accèdent aux sections autorisées et le personnel peut consulter un tableau de bord avec view-as. Le tableau de bord n’exécute pas les commandes du bot.

## Interfaces

La garde de session vérifie le compte et la délégation ; `board.ts` choisit la chaîne effective pour un délégué. Les actions de modules et commandes valident les données et appellent leurs propriétaires. Kit fournit les catalogues et fonctions serveur partagés ; UI fournit les composants de présentation.

## État et dépendances

Les services de données possèdent les comptes et la configuration ; Kit fournit les catalogues et fonctions partagés.

## Comportement en cas d’échec

Les gardes de session, de compte et de délégation contrôlent l’accès avant les actions protégées.

## Code

Source : [web/dashboard/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/web/dashboard). Voir `src/hooks.server.ts`, `src/lib/server/services.ts`, `guard.ts` et `board.ts` ; les définitions partagées sont dans `web/kit/`.

## Validation

Depuis `web/`, exécuter `bun run --filter dashboard check` ; voir `web/dashboard/package.json` pour les vérifications disponibles.
