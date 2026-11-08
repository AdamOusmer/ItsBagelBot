---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Discord data
description: "Gère les liens et réglages des serveurs Discord, les tickets et les points d’expérience des membres."
docType: reference
---

## Responsabilités

Discord Engine et Outgress utilisent le stockage par RPC plutôt que ce schéma directement. Le service consomme aussi les suppressions de compte pour le nettoyage.

## Interfaces

Le préfixe RPC par défaut est `bagel.rpc.discord-data`. `module.list` et `module.set` gèrent les réglages par serveur ; Modules conserve l’interrupteur principal de la chaîne. Une chaîne peut lier plusieurs serveurs, mais un serveur appartient à une seule chaîne. `module.set` valide les réglages et utilise une version attendue pour gérer la concurrence ; changer de propriétaire exige une dissociation explicite.

## État et dépendances

Le service possède la configuration et les liens des serveurs, les tickets et les points d’expérience ; Engine et Outgress utilisent son adaptateur RPC.

## Comportement en cas d’échec

Les contrôles de version refusent les écritures conflictuelles. La suppression d’un compte est consommée pour le nettoyage.

## Code

Source : [app/db/discord/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db/discord). Voir `main.go`, `rpc/`, `repository/`, `ent/schema/` et `internal/discordstore/`.

## Validation

Depuis la racine, exécuter `go test ./app/db/discord/...`.
