---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Users
description: "Gère les comptes, les droits, les préférences des chaînes, les identifiants Twitch, le personnel, les délégations et les autorisations d’appareil."
docType: reference
---

## Responsabilités

Les autres services utilisent ses RPC plutôt que son schéma. Il émet des événements de modification et de suppression pour les projections et le nettoyage. Les opérations sur les jetons ont une surface RPC interne distincte.

## Interfaces

Les RPC du tableau de bord utilisent par défaut `bagel.rpc.dashboard.*` ; l’autorisation des appareils utilise `bagel.rpc.dashboard.device.*`. Les changements et suppressions publient `data.users.changed` et `data.users.deleted`.

L’inscription et les changements de droits sont persistés immédiatement. Certaines préférences sont regroupées avant écriture : acceptation et persistance sont donc distinctes. Une modification persistée publie `data.users.changed`. La suppression écrit d’abord dans un registre d’effacement, révoque les jetons et émet `data.users.deleted` ; un échec du registre empêche la suppression. Les lectures sensibles utilisent des RPC internes, pas les projections. Les opérations du tableau de bord et des appareils utilisent par défaut `bagel.rpc.dashboard.*`.

## État et dépendances

Users possède le schéma des comptes et des identifiants ; les consommateurs utilisent les RPC ou les projections.

## Comportement en cas d’échec

Les préférences acceptées en écriture différée ne sont pas encore persistées. La suppression s’arrête si l’écriture du registre d’effacement échoue.

## Code

Source : [app/db/users/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db/users). Voir `main.go`, `rpc/`, `repository/` et `ent/schema/` ; la signature des jetons d’appareil se trouve dans `tokensign/`.

## Validation

Depuis la racine, exécuter `go test ./app/db/users/...`.
