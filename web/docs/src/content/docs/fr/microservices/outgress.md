---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "Outgress"
description: "Gère les messages sortants vers Twitch et la limitation du débit par diffuseur."
---

Le service Outgress (`app/twitch/outgress/`) est le point de passage de toutes les communications sortantes vers Twitch. Il garantit le respect des limites de débit strictes de Twitch et gère le cycle de vie des jetons utilisés pour les requêtes sortantes.

Outgress ne connaît pas Discord : [Dingress](/fr/microservices/dingress/) possède cette surface de bout en bout. Leur unique lien est un fait, pas une commande : après la réussite de Helix Create Clip et l’envoi de la réponse dans le chat, Outgress publie sur `BAGEL_DATA` un événement `data.twitch.clip.created` décrivant ce qui s’est produit sur Twitch ; les services intéressés, dont Dingress, s’y abonnent.

## Architecture

Outgress consomme les actions reçues par NATS, par exemple `twitch.outgress.premium` et `twitch.outgress.standard`, émises par le pipeline [Sesame](/fr/microservices/sesame/). Il sert de passerelle unifiée pour convertir les sorties internes des modules en véritables requêtes API ou messages IRC.

### Limitation du débit

Twitch applique des limites par diffuseur ou par compte bot. Outgress s’appuie sur **Valkey** pour suivre ces limites de manière distribuée entre ses instances. Avant d’envoyer un message à Twitch, Outgress vérifie son quota dans Valkey ; s’il est épuisé, le message est retardé ou abandonné selon la priorité de sa voie.

### Cycle de vie des jetons

Outgress a besoin de jetons OAuth Twitch valides pour interagir avec l’API. Il utilise les RPC NATS internes `bagel.rpc.internal.tokens.*` pour récupérer ces jetons de façon sécurisée auprès du service [Users](/fr/microservices/users/).

### Sécurité

Outgress pouvant envoyer des messages au nom du bot ou du diffuseur, son accès est restreint. Un mécanisme d’**arrêt d’urgence** permet aux opérateurs de couper immédiatement les communications sortantes en cas d’urgence.
