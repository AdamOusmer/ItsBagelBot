---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Commands
description: "Gère les commandes personnalisées, leurs alias et compteurs, les sources de données externes et leurs clés chiffrées."
docType: reference
---

## Responsabilités

Sesame lit la configuration par projection ou RPC. Gossip résout les définitions et les clés au besoin ; une projection ne doit pas exposer les identifiants déchiffrés. Les modifications alimentent Projector.

## Interfaces

Le préfixe RPC par défaut est `bagel.rpc.commands`. Les écritures ordinaires sont regroupées : une réponse positive signifie leur acceptation, pas leur persistance immédiate. Le renommage et la suppression sont immédiats. Un consommateur durable traite `data.commands.used` et persiste le reçu du lot avec son incrément avant acquittement. La suppression d’un compte retire ses commandes et identifiants de sources selon la date limite de suppression.

## État et dépendances

Commands possède son schéma ; Projector et Gossip utilisent ses vues et RPC internes.

## Comportement en cas d’échec

Une écriture regroupée acceptée n’est pas encore persistée. Le reçu du compteur et son incrément sont enregistrés ensemble.

## Code

Source : [app/db/commands/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db/commands). Voir `main.go`, `rpc/`, `repository/` et `ent/schema/`.

## Validation

Depuis la racine, exécuter `go test ./app/db/commands/...`.
