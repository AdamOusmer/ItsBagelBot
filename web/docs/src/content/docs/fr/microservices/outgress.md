---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "Outgress"
description: "Gère les messages sortants vers Twitch et la limitation du débit par diffuseur."
---

Le service Outgress (`app/twitch/outgress/`) est le point de passage de toutes les communications sortantes vers Twitch. Il garantit le respect des limites de débit strictes de Twitch et gère le cycle de vie des jetons utilisés pour les requêtes sortantes.

Outgress ne connaît pas Discord : les services Discord ([Dingress](/fr/microservices/dingress/), son moteur et son outgress) possèdent cette surface de bout en bout. Leur unique lien est un fait, pas une commande : après la réussite de Helix Create Clip et l’envoi de la réponse dans le chat, Outgress publie sur `BAGEL_DATA` un événement `data.twitch.clip.created` décrivant ce qui s’est produit sur Twitch ; les services intéressés, dont le moteur Discord, s’y abonnent.

## Architecture

Outgress consomme des messages d’action typés reçus par NATS : `twitch.outgress.premium` et `twitch.outgress.standard` (émis par le pipeline [Sesame](/fr/microservices/sesame/)) pour les sorties destinées aux spectateurs, et `twitch.outgress.system` pour les tâches de contrôle comme l’inscription EventSub et les vérifications d’état du stream. Il les convertit en appels Helix et en envois dans le chat. Il sert aussi des RPC de gestion et de lecture sous `bagel.rpc.outgress.*` (points de chaîne, infos du stream, spectateurs, essais).

Les messages de chat sont périssables : le flux de chat les garde cinq secondes, tandis que le flux système garde les tâches de contrôle cinq minutes. Outgress obtient l’identifiant du Conduit actif auprès de [Twitch Ingress](/fr/microservices/twitch-ingress/) (`bagel.rpc.ingress.conduit.get`).

### Limitation du débit

Twitch applique des limites par diffuseur, par compte bot et par application. Outgress s’appuie sur **Valkey** pour coordonner ces limites entre toutes ses instances, avec des budgets distincts pour le chat et pour les appels Helix. Avant d’exécuter une action, Outgress prélève son quota sur le budget partagé. Un message de chat qui ne peut pas partir avant la fin de la rétention de cinq secondes du flux de chat est abandonné.

### Cycle de vie des jetons

Outgress a besoin de jetons OAuth Twitch valides pour interagir avec l’API. Il utilise les RPC NATS internes `bagel.rpc.internal.tokens.*` (`get` et `save`) pour récupérer et enregistrer les jetons par le service [Users](/fr/microservices/users/), et coordonne les renouvellements par un bail partagé afin que deux instances ne renouvellent pas le même jeton en même temps.

### Sécurité

Outgress pouvant envoyer des messages au nom du bot ou du diffuseur, son accès est restreint. Un interrupteur de **pause**, conservé durablement (état clé-valeur JetStream), permet aux opérateurs d’arrêter l’exécution sortante sur toute la flotte en cas d’urgence : tant qu’il est actif, les tâches ne sont pas exécutées et sont retentées. Toute sortie marquée d’origine d’essai (chaîne observée non inscrite) est refusée avant le moindre effet externe.
