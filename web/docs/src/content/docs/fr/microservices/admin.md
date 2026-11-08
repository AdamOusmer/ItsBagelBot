---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Admin
description: "Fournit la console du personnel pour les comptes, Ingress, les accès, les notifications et les opérations."
docType: reference
---

## Responsabilités

Les actions serveur autorisent les opérateurs et appellent les services propriétaires. L’accès réseau et l’autorisation active du personnel sont deux contrôles distincts ; un échec d’autorisation ne doit pas être traité comme une réussite.

## Interfaces

`hooks.server.ts` établit la session et vérifie la révocation du personnel. Les fonctions serveur vérifient à nouveau le statut actif et appliquent les droits requis aux actions protégées. Les opérations de tirage et de déploiement appellent leurs propriétaires ; l’indisponibilité d’un service ne prouve pas la réussite.

## État et dépendances

Users et les autres services conservent les données métier ; Kit et UI fournissent le code partagé.

## Comportement en cas d’échec

L’autorisation est vérifiée à nouveau côté serveur. Une panne du service appelé ne constitue pas une opération réussie.

## Code

Source : [web/admin/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/web/admin). Voir `src/hooks.server.ts`, `src/lib/server/access.ts`, `services.ts` et `admin-action.ts`.

## Validation

Depuis `web/`, exécuter `bun run --filter admin check` pour les vérifications configurées ; consulter `web/admin/package.json` pour les scripts actuels.
