---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Discord ingress
description: "Gère la session de passerelle Discord, les battements, la reprise, la présence et le relais de certains événements."
docType: reference
---

## Responsabilités

Un bail Valkey autorise une seule session active par jeton ; une instance en attente peut reprendre depuis le point de contrôle. Ingress diffère immédiatement les interactions reçues par la passerelle. Il sert aussi `POST /discord/interactions`, vérifie les signatures, acquitte la requête et publie l’interaction pour Engine. Les autres effets REST appartiennent à Outgress.

## Interfaces

Le détenteur du bail ouvre la session et enregistre son identifiant, son URL de reprise et sa séquence. Son successeur peut reprendre depuis ce point. L’acquittement d’une interaction de passerelle est une exception REST immédiate ; le point HTTP acquitte quant à lui la requête après vérification de signature. Les deux voies publient une interaction à traiter : l’acquittement ne prouve donc pas sa réussite.

## État et dépendances

Un bail Valkey choisit la session active ; les points de contrôle permettent la reprise.

## Comportement en cas d’échec

Une interaction différée est acquittée, pas nécessairement exécutée. Les instances en attente n’ouvrent pas une autre session.

## Code

Source : [app/discord/ingress/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/discord/ingress). Voir `main.go`, `internal/gateway/` et `internal/relay/` ; les contrats sont dans `internal/domain/discord/`.

## Validation

Depuis la racine, exécuter `go test ./app/discord/ingress/...`. Voir `internal/gateway/session.go`, `lease.go` et `internal/relay/interactions.go`.
