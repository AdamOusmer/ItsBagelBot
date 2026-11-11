---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Notifications
description: "Gère les notifications destinées aux comptes et leur nettoyage."
docType: reference
---

## Responsabilités

Les RPC servent les notifications du tableau de bord et les annonces administratives. Les autres services passent par ce propriétaire au lieu d’écrire directement ses données.

## Interfaces

Les opérations utilisateur utilisent `rpc.notifications.*` ; les opérations administratives utilisent `rpc.admin.notifications.*`.

`rpc.notifications.*` liste et marque les notifications ; `rpc.admin.notifications.*` les envoie, liste et supprime. Un identifiant de requête permet à une création répétée de renvoyer la ligne existante. L’expiration globale et celle par utilisateur ont des rôles distincts ; marquer une notification comme lue ne supprime pas l’annonce globale. Le nettoyage utilise une RPC interne.

## État et dépendances

Le service possède les notifications et les reçus par utilisateur.

## Comportement en cas d’échec

Les créations répétées avec le même identifiant renvoient la ligne existante. L’expiration et le nettoyage sont distincts des reçus de lecture.

## Code

Source : [app/db/notifications/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db/notifications). Voir `main.go`, `rpc/`, `repository/` et `ent/schema/`.

## Validation

Depuis la racine, exécuter `go test ./app/db/notifications/...`.
