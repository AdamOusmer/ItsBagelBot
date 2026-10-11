---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Registre des services
description: Les services d'ItsBagelBot, leurs responsabilités, leurs communications et leur authentification mutuelle.
---

Chaque service se déploie indépendamment pour permettre des mises en production sans interruption. Go est utilisé pour les services de données et les workers, Elixir/OTP pour l'ingress Twitch, et SvelteKit (SSR) pour les consoles du tableau de bord et d’administration. Le seul transport interservices est **NATS** : pub/sub par sujet pour les événements et request-reply pour les RPC. Aucun service ne lit la base d'un autre.

## Registre

| Service | Chemin du dépôt | Langage | Possède / fait | Expose |
|---|---|---|---|---|
| [Twitch Ingress](/fr/microservices/twitch-ingress/) | `app/twitch/ingress/` | Elixir (OTP 27+) | EventSub Conduit et shards WebSocket ; supervision par shard ; filtrage, regroupement et normalisation des événements | `twitch.ingress.event.*`, `twitch.ingress.status.*`, `twitch.ingress.admin.*`, `bagel.rpc.ingress.conduit.get` |
| [Sesame](/fr/microservices/sesame/) | `app/twitch/sesame/` | Go | Moteur principal et traitement des commandes ; consommation des événements ingress, modération entrante, handlers et commandes des modules, routage vers outgress | consomme `twitch.ingress.event.*` ; publie vers `twitch.outgress.*` |
| [Outgress](/fr/microservices/outgress/) | `app/twitch/outgress/` | Go | API Twitch et envoi dans le chat ; limites de débit coordonnées pour la flotte (Valkey) ; registre des chaînes et interrupteur de pause ; usage et renouvellement des jetons ; inscription EventSub | consomme `twitch.outgress.*` ; sert `bagel.rpc.outgress.*` |
| [Discord Ingress](/fr/microservices/dingress/) (`dingress`) | `app/discord/ingress/` | Go | Session de passerelle Discord pour le jeton de bot de la flotte (bail Valkey, un seul réplica actif) ; relais des événements ; accusé des interactions | publie `discord.ingress.event.*` |
| Discord Engine | `app/discord/engine/` | Go | Décide du comportement communautaire Discord (accueil, tickets, salons vocaux, journaux, protection des liens, annonces de direct) à partir des événements Discord et Twitch | consomme `discord.ingress.event.*` ; publie `discord.outgress.*` |
| Discord Outgress | `app/discord/outgress/` | Go | Exécute les actions REST Discord ; mise en place et agencement des serveurs ; enregistrement des commandes slash | consomme `discord.outgress.*` ; sert `bagel.rpc.dingress.*`, `bagel.rpc.discord-outgress.*` |
| [Projector](/fr/microservices/projector/) | `app/projector/` | Go | Intègre les événements des services propriétaires dans des modèles de lecture Valkey ; sert les lectures d’état, de direct, d’infos du stream et du tableau de bord ; compteurs en direct | `bagel.rpc.broadcaster.status.get`, `bagel.rpc.broadcaster.live.get`, `bagel.rpc.projector.dashboard.*` |
| Gossip | `app/gossip/` | Go | Fournisseurs d’API externes derrière des RPC typés : cache, budgets amont, recherche des identifiants, requêtes personnalisées | `bagel.rpc.gossip.*` |
| [Users](/fr/microservices/users/) | `app/db/users/` | Go | Comptes, statut (free/paid/vip), activation du bot, garde des jetons Twitch, staff et délégation | `bagel.rpc.dashboard.*`, `bagel.rpc.admin.user.*`, `bagel.rpc.internal.projection.users.get`, `bagel.rpc.internal.tokens.*` ; émet `data.users.*` |
| [Commands](/fr/microservices/commands/) | `app/db/commands/` | Go | Commandes de chat personnalisées, compteurs d’utilisation, définitions de requêtes externes et clés chiffrées | `bagel.rpc.commands.*`, `bagel.rpc.internal.projection.commands.get` ; émet `data.commands.*` |
| [Modules](/fr/microservices/modules/) | `app/db/modules/` | Go | Réglages des modules par diffuseur, citations, compteurs d’alimentation, identifiants Govee et Spotify | `bagel.rpc.modules.*`, `bagel.rpc.internal.projection.modules.get` ; émet `data.modules.changed` |
| Loyalty | `app/db/loyalty/` | Go | Points des spectateurs, temps de visionnage, compteurs de chaîne | `bagel.rpc.loyalty.*` ; consomme `data.loyalty.*` |
| Discord data (`discord-data`) | `app/db/discord/` | Go | Liaisons de serveurs, réglages par serveur, tickets, XP des membres | `bagel.rpc.discord-data.*` |
| [Notifications](/fr/microservices/notifications/) | `app/db/notifications/` | Go | Notifications du tableau de bord, annonces administratives et nettoyage par TTL | `bagel.rpc.notifications.*`, `bagel.rpc.admin.notifications.*` |
| [Transactions](/fr/microservices/transactions/) | `app/db/transactions/` | Go | Audit des webhooks Tebex, paniers de paiement, tirages et attributions des concours | `bagel.rpc.transactions.*` ; webhook HTTPS `/tebex` |
| [Admin](/fr/microservices/admin/) | `web/admin/` | SvelteKit SSR | Console du staff : comptes, flotte de shards, voies, concours, essais, notifications, déploiements | HTTPS via le tailnet uniquement ; communication avec les services par RPC NATS |
| [Console](/fr/microservices/console/) | `web/dashboard/` | SvelteKit SSR | Tableau de bord des diffuseurs et pages publiques des commandes ; OAuth Twitch | HTTPS via Traefik ; communication avec les services par RPC NATS |
| [Web](/fr/microservices/web/) | `web/marketing/` | Astro | Site marketing public (la documentation se trouve dans `web/docs/`) | HTTPS public |

Les répertoires `app/twitch/automod/`, `app/yt-ingress/` et `app/plain/` ne contiennent aucun service en fonctionnement. L’AutoMod entrant s’exécute dans Sesame.

Voir [l'état du système](/fr/reference/system-overview/) pour le plan de données, le bus et le flux complet d'une requête, ainsi que les [contrats RPC](/fr/reference/rpc-contracts/) pour toute la surface request-reply.

## Forme des échanges entre services

NATS transporte deux classes de trafic :

- **Événements** (pub/sub sans attente de réponse). Événements de changement `data.*` émis par le service propriétaire et consommés par le projector et ses pairs ; événements ingress `twitch.ingress.event.*` ; état des shards `twitch.ingress.status.*` ; voies d’envoi outgress `twitch.outgress.*` ; voies Discord `discord.ingress.event.*` et `discord.outgress.*` ; diffusions d’invalidation du cache `bagel.cache.invalidate.<portée>` (par exemple `status`, `modules`, `commands`, `live`).
- **RPC** (requête-réponse). Tout ce qui se trouve sous `bagel.rpc.*`, ainsi que l’instantané des shards ingress `twitch.ingress.admin.shards.get`. Chaque handler s’abonne dans un **groupe de file**, de sorte que n’importe quelle réplique peut répondre et que la charge se répartit dans la flotte.

Un service de données est le **seul écrivain** de son schéma. Tout autre service qui en a besoin le demande via RPC et n'ouvre jamais la base. De nombreuses écritures sont write-behind : l'appelant reçoit une réponse optimiste, la base est mise à jour de façon asynchrone, puis une publication `bagel.cache.invalidate.<portée>` reconverge les lecteurs mis en cache. Les enregistrements interactifs (par exemple un `upsert` de module depuis le tableau de bord) sont écrits avant la réponse.

## Authentification interservices

- **Périmètre de transport** : les services s’exécutent dans le cluster sur le tailnet ; aucun port NATS n’est exposé publiquement. Le seul point d’entrée public est le tunnel sortant cloudflared.
- **Autorisation NATS** : les sujets transportant des secrets (les opérations de jeton du compte bot sous `bagel.rpc.internal.tokens.*`) sont restreints par compte et permissions NATS, afin que seuls Twitch Outgress et Users puissent les utiliser. Les jetons en clair ne transitent jamais sur un autre sujet.
- **TLS mutuel et comptes** : le serveur NATS exige un certificat client de chaque charge de travail (`verify: true`), et des comptes NATS à base de JWT décident quels sujets chaque service peut publier, servir ou importer (`deploy/messaging/accounts.yaml`). La console d’administration n’est exposée que par Tailscale (voir [Réseau](/fr/infrastructure/networking/)).
- **OAuth** : le tableau de bord authentifie les utilisateurs finaux avec OAuth Twitch ; le service users enregistre les autorisations des diffuseurs via `grant_save`.
