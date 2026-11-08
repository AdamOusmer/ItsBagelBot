---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Controls gateway
description: "Accepte les connexions WebSocket des commandes de bureau sur GET /v1/connect."
docType: reference
---

## Responsabilités

Il vérifie les jetons signés émis par Users, relaie les événements et commandes sur NATS et ferme les connexions lors d’une révocation ou d’une reprise. L’identité de l’appareil provient du jeton vérifié. Il ne délivre pas de jetons et ne gère pas la logique Twitch ou Discord.

## Interfaces

Le point WebSocket est `GET /v1/connect`. NATS transporte les événements sur `controls.device.<device_id>.evt`, les commandes sur `.cmd`, les reprises sur `.takeover` et les révocations sur `controls.revoked.<device_id>`.

La passerelle vérifie la signature, l’émetteur, le destinataire et l’expiration avant d’utiliser l’identité de l’appareil. NATS relaie les événements et commandes entre instances. Une nouvelle connexion publie une reprise pour le même appareil ; une révocation ferme sa connexion. Users gère la signature et les autorisations.

## État et dépendances

Users signe les jetons et possède les autorisations. NATS relaie les messages et événements de connexion entre instances.

## Comportement en cas d’échec

Les jetons invalides, expirés ou invérifiables sont refusés. Une reprise ou révocation ferme la connexion concernée.

## Code

Source : [app/controls/gateway/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/controls/gateway). Voir `lib/controls_gateway/ws_handler.ex`, `token.ex`, `nats.ex` et `subjects.ex` ; les tests sont dans `test/`.

## Validation

Depuis `app/controls/gateway/`, exécuter `mix test`. Voir `lib/controls_gateway/ws_handler.ex` et `token.ex`.
