---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Registre des services
description: Les services d'ItsBagelBot, leurs responsabilités, leurs communications et leur authentification mutuelle.
---

Chaque service se déploie indépendamment pour permettre des mises en production sans interruption. Go est utilisé pour les services de données et les workers, Elixir/OTP pour l'ingress Twitch, et SvelteKit (SSR) pour la console. Le seul transport interservices est **NATS** : pub/sub par sujet pour les événements et request-reply pour les RPC. Aucun service ne lit la base d'un autre.

## Registre

| Service | Chemin du dépôt | Langage | Possède / fait | Expose |
|---|---|---|---|---|
| [Twitch Ingress](/fr/microservices/twitch-ingress/) | `app/twitch/ingress/` | Elixir (OTP 27+) | EventSub Conduit et shards WebSocket ; supervision par shard ; OAuth par locataire ; filtrage et normalisation des événements | `twitch.ingress.event.*`, `twitch.ingress.status.*`, `twitch.ingress.admin.shards.get` |
| [YouTube Ingress](/fr/microservices/youtube-ingress/) | `app/yt-ingress/` | Elixir (OTP 27+) | Flux gRPC `liveChatMessages.streamList` par chaîne ; surveillance de découverte des diffusions ; location de jetons par RPC ; normalisation des événements | `youtube.ingress.event.*`, `youtube.ingress.status.*`, `youtube.ingress.admin.chats.get` |
| [Sesame](/fr/microservices/sesame/) | `app/twitch/sesame/` | Go | Moteur principal et traitement des commandes ; consommation des événements ingress, exécution des handlers et commandes des modules, routage vers outgress | consomme `twitch.ingress.event.*` ; publie vers `twitch.outgress.*` |
| [Outgress](/fr/microservices/outgress/) | `app/twitch/outgress/` | Go | Envoi vers Twitch, YouTube Live Chat et Discord (REST) ; limite par diffuseur et budget quotidien de quota (Valkey) ; registre des chaînes ; cycle de vie des jetons ; arrêt d’urgence. Les notifications Discord de début/fin de direct se lient directement à `twitch.ingress.event.stream`. | consomme `twitch.outgress.*`, `youtube.outgress.*`, `discord.outgress.*` ; sert `bagel.rpc.outgress.*` |
| [Dingress](/fr/microservices/dingress/) | `app/dingress/` | Go | Instance unique de passerelle Discord : accueil, salons vocaux créés à l’arrivée, tickets, journaux du staff, commandes slash, rangs de miettes. Lit l’index inverse des serveurs écrit par outgress. | WebSocket de la passerelle Discord ; `/status` |
| [Projector](/fr/microservices/projector/) | `app/projector/` | Go | Construit la projection Valkey des réglages au passage en direct ; répond aux recherches de niveau des diffuseurs | `bagel.rpc.broadcaster.status.get` |
| [Users](/fr/microservices/users/) | `app/db/users/` | Go | Comptes utilisateurs, statut (free/paid/vip), activation, jetons OAuth Twitch | `bagel.rpc.dashboard.*`, `bagel.rpc.admin.user.*`, `bagel.rpc.internal.projection.users.get`, `bagel.rpc.internal.tokens.*` ; émet `data.users.*` |
| [Commands](/fr/microservices/commands/) | `app/db/commands/` | Go | Commandes de chat personnalisées | `bagel.rpc.commands.*`, `bagel.rpc.internal.projection.commands.get` ; émet `data.commands.changed` |
| [Modules](/fr/microservices/modules/) | `app/db/modules/` | Go | Modules fonctionnels par diffuseur | `bagel.rpc.internal.projection.modules.get` ; émet `data.modules.changed` |
| [Notifications](/fr/microservices/notifications/) | `app/db/notifications/` | Go | Notifications du tableau de bord, annonces administratives et nettoyage par TTL | `bagel.rpc.notifications.*`, `bagel.rpc.admin.notifications.*` |
| [Transactions](/fr/microservices/transactions/) | `app/db/transactions/` | Go | Enregistrements des achats Tebex | consomme `data.transactions.recorded` |
| [Admin](/fr/microservices/admin/) (ancien) | `app/admin/` | Go + templ | Vue opérateur en lecture seule via NATS (flotte de shards, utilisateurs) | Opérateurs uniquement, via le tailnet |
| [Console](/fr/microservices/console/) | `console/` | SvelteKit SSR | Applications `dashboard` (libre-service diffuseur) et `admin` (opérateur) ; OAuth via oauth4webapi | HTTPS via cloudflared / tailnet ; communication avec les services uniquement par RPC NATS |
| [Web](/fr/microservices/web/) | `web/` | Astro | Site public de présentation et documentation | HTTPS public |

Voir [l'état du système](/fr/reference/system-overview/) pour le plan de données, le bus et le flux complet d'une requête, ainsi que les [contrats RPC](/fr/reference/rpc-contracts/) pour toute la surface request-reply.

## Forme des échanges entre services

NATS transporte deux classes de trafic :

- **Événements** (pub/sub sans attente de réponse). Événements de changement `data.*` émis par le service propriétaire et consommés par le projector et ses pairs ; événements ingress `twitch.ingress.event.*` ; état des shards `twitch.ingress.status.*` ; voies d’envoi outgress `twitch.outgress.*` ; diffusion d’invalidation du cache `bagel.cache.invalidate.broadcaster`.
- **RPC** (requête-réponse). Tout ce qui se trouve sous `bagel.rpc.*`, ainsi que l’instantané des shards ingress `twitch.ingress.admin.shards.get`. Chaque handler s’abonne dans un **groupe de file**, de sorte que n’importe quelle réplique peut répondre et que la charge se répartit dans la flotte.

Un service de données est le **seul écrivain** de son schéma. Tout autre service qui en a besoin le demande via RPC et n'ouvre jamais la base. Les écritures sont write-behind : l'appelant reçoit une réponse optimiste, la base est mise à jour de façon asynchrone, puis une publication `bagel.cache.invalidate.broadcaster` reconverge les lecteurs mis en cache.

## Authentification interservices

- **Périmètre de transport** : les services s’exécutent dans le cluster sur le tailnet ; aucun port NATS n’est exposé publiquement. Le seul point d’entrée public est le tunnel sortant cloudflared.
- **Autorisation NATS** : les sujets transportant des secrets (les opérations de jeton du compte bot sous `bagel.rpc.internal.tokens.*`) sont restreints par compte et permissions NATS, afin que seuls outgress et users puissent les utiliser. Les jetons en clair ne transitent jamais sur un autre sujet.
- **Mesh** : le sidecar natif Linkerd fournit le mTLS entre les services du mesh. L’ancien outil admin est délibérément **hors du mesh** et accessible uniquement par Tailscale, avec les ACL du tailnet comme contrôle d’accès (voir [Réseau](/fr/infrastructure/networking/)).
- **OAuth** : la console authentifie les utilisateurs finaux avec oauth4webapi (OAuth Twitch) ; le service users enregistre les autorisations des diffuseurs via `grant_save`.
