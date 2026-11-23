---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Passerelle de commandes
description: "Sert la source navigateur OBS pour la synthèse vocale."
docType: reference
---

## Responsabilités

La bêta sert la source navigateur OBS à
`https://overlay.itsbagelbot.com/tts#<token>`. La page échange le fragment
contre un cookie signé et lit les extraits audio dans leur ordre d’arrivée.
Le service Users émet et révoque les jetons.

## Interfaces

`POST /tts/session` ouvre la session. `GET /tts/events` transmet les événements
audio et reprend après le dernier identifiant à la reconnexion.
`GET /tts/clip/<event_id>` diffuse le fichier WAV associé à l’utilisateur de la
session. La passerelle sert aussi `/healthz` et `/readyz`. Le WebSocket de
l’application de bureau est désactivé.

## État et dépendances

JetStream conserve les événements et les fichiers audio pendant cinq minutes.
Une révocation ferme le flux d’événements. Un extrait absent ou expiré renvoie
une erreur 404.

## Code et validation

Voir [app/controls/gateway](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/controls/gateway).
Dans ce répertoire, exécuter `mix deps.get` puis `mix test` avec
`nats-server` disponible dans le PATH.
