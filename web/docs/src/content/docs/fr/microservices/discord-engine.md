---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Discord engine
description: "Transforme les événements Discord et certaines données Twitch en comportements communautaires et en actions Discord décrites."
docType: reference
---

## Responsabilités

Ingress reçoit les événements et Outgress exécute les effets REST. Engine résout la configuration auprès de Discord data, applique les règles d’activation et utilise les RPC d’Outgress pour certaines orchestrations synchrones. Ses modules couvrent notamment les annonces de direct, tickets, salons vocaux, rôles, journaux et liens.

## Interfaces

Les entrées comprennent `discord.ingress.event.{message,member,voice,interaction,audit,guild}` et certaines données Twitch. Les sorties sont des commandes Discord typées ou des RPC privées vers Outgress.

Engine consomme les six sujets d’événements Discord, déduplique par UUID de message et résout le lien du serveur, les droits de la chaîne et les modules avant distribution. Les doublons sont acquittés ; une panne du stockage de déduplication laisse passer l’événement avec un avertissement. Un serveur désactivé ou non lié ne produit pas de travail ordinaire. Une donnée Twitch peut être distribuée à plusieurs serveurs liés à une chaîne.

## État et dépendances

Discord data possède les réglages durables ; Valkey conserve l’état éphémère et les caches.

## Comportement en cas d’échec

Les doublons sont acquittés. Une panne du stockage de déduplication laisse passer l’événement avec un avertissement ; les serveurs non liés ou désactivés ne produisent pas de travail ordinaire.

## Code

Source : [app/discord/engine/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/discord/engine). Voir `main.go`, `modules/all.go`, `internal/dispatch/` et les contrats dans `internal/domain/discord/`.

## Validation

Depuis la racine, exécuter `go test ./app/discord/engine/...`. Voir `internal/dispatch/` et `modules/all.go`.
