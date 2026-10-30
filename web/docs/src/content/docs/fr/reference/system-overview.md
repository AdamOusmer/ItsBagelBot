---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: État du système
description: "La forme actuelle d'ItsBagelBot : services, plan de données, bus de messages et transformation d'un message de chat en réponse."
---

Cette page documente le système **tel qu'il fonctionne aujourd'hui** : microservices de données
par schéma, bus NATS, ingress Elixir et consoles SvelteKit. Lorsqu'une ancienne page diverge,
cette page et le code font foi.

## En bref

- **Microservices**, une responsabilité chacun, déployables indépendamment pour des mises à jour sans interruption.
- **Go** pour les services de données et de traitement, **Elixir/OTP** pour l'ingress Twitch, **SvelteKit (SSR)** pour les consoles des diffuseurs et de l'équipe.
- **NATS** est le seul transport interservices : pub/sub par sujets et JetStream pour les événements, request-reply pour les RPC. Aucun service ne lit la base d'un autre.
- **MySQL HeatWave**, un schéma par service de données, accessible via `ent`.
- **Valkey** contient la projection des réglages et des niveaux, ainsi que des caches de courte durée.
- Fonctionne sur **k3s** avec Cilium, derrière un tunnel **cloudflared**, avec **Tailscale** pour l'accès des opérateurs.

## Services

| Service | Chemin | Langage | Responsabilité |
|---|---|---|---|
| **ingress** | `app/twitch/ingress/` | Elixir | Conduit EventSub et shards WebSocket Twitch ; supervision par shard ; normalise et publie les événements sur `twitch.ingress.event.*` ; expose le RPC d'instantané des shards |
| **sesame** | `app/twitch/sesame/` | Go | Consomme les voies de l'ingress ; exécute modules, commandes personnalisées, minuteurs et AutoMod entrant ; publie des requêtes outgress typées |
| **outgress** | `app/twitch/outgress/` | Go | Exécute les actions Twitch (chat, gestion EventSub) ; limite de débit par diffuseur via Valkey ; registre des chaînes ; cycle de vie des jetons ; interrupteur d'arrêt |
| **projector** | `app/projector/` | Go | Construit les projections Valkey à partir des événements des propriétaires ; sert les lectures de niveau, d'état en direct et de tableau de bord ; compteurs et classements en direct |
| **users** | `app/db/users/` | Go | Comptes, statut (free/paid/vip), rôles du personnel, jetons OAuth Twitch ; émet `data.users.*` |
| **commands** | `app/db/commands/` | Go | Commandes de chat personnalisées et définitions de fetch ; émet `data.commands.*` |
| **modules** | `app/db/modules/` | Go | Configuration des modules par diffuseur ; émet `data.modules.changed` |
| **loyalty** | `app/db/loyalty/` | Go | Soldes des spectateurs, temps de visionnage et compteurs de chaîne |
| **transactions** | `app/db/transactions/` | Go | Facturation Tebex, concours et remise des récompenses |
| **notifications** | `app/db/notifications/` | Go | Enregistrements de notifications destinées aux comptes |
| **discord-data** | `app/db/discord/` | Go | Configuration des serveurs Discord, tickets et XP |
| **discord** | `app/discord/{ingress,engine,outgress}/` | Go | Session de la passerelle Discord, décisions des modules et effets REST |
| **gossip** | `app/gossip/` | Go | Passerelle vers les API externes, avec cache et budgets amont |
| **release** | `app/release/` | Go | CLI du train de publication exécutée par le workflow GitHub Release, une étape par job |
| **clusterops** | `app/clusterops/` | Go | Redémarre les consoles au renouvellement TLS et garde les claims de comptes NATS à jour |
| **console** | `web/dashboard/`, `web/admin/` | SvelteKit SSR | `dashboard` (libre-service des diffuseurs et pages publiques) et `admin` (équipe) ; ne parle aux services que par RPC NATS |

`CONTEXT-MAP.md` à la racine du dépôt liste des emplacements réservés comme `app/plain/` et
`app/yt-ingress/` ; ce ne sont pas des services en fonctionnement.

## Plan de données

- **Base de données** : MySQL HeatWave. Chaque service de données possède son propre schéma et en
  est le seul écrivain. Les lectures entre services passent par RPC NATS, jamais par SQL.
- **ORM** : `ent`, code généré par service sous `app/db/<svc>/ent/`.
- **Projection** : Valkey stocke la projection des réglages par diffuseur (`settings:<user_id>`)
  lue par le chemin critique. Les écritures de commandes et de modules sont en **write-behind**
  (~2 s) : le tableau de bord renvoie une vue optimiste, la base est mise à jour de façon asynchrone,
  et un événement d'invalidation de cache réaligne les lecteurs.
- **Cache** : des caches en processus de courte durée devant Valkey (par exemple le cache de niveau
  du projector, TTL de 30 s) absorbent les recherches répétées.

## Bus de messages

NATS transporte deux classes de trafic :

- **Événements** (pub/sub et JetStream). Événements de changement de domaine `data.*`, événements
  d'ingress `twitch.ingress.event.*`, état des shards `twitch.ingress.status.*`, voies d'envoi
  outgress `twitch.outgress.*` et diffusions d'invalidation de cache `bagel.cache.invalidate.<scope>`.
  Le catalogue des flux se trouve dans `pkg/bus/streams.go`.
- **RPC** (request-reply). Tout ce qui est sous `bagel.rpc.*`, plus les sujets d'administration de
  l'ingress `twitch.ingress.admin.*`. Voir [contrats RPC →](/fr/reference/rpc-contracts/).

Les brokers fonctionnent en mode opérateur JWT, avec des comptes et des rôles par service
(`deploy/messaging/accounts.yaml`) : un service ne peut publier et s'abonner qu'aux sujets que son rôle accorde.

### Voies premium / standard

L'ingress oriente chaque événement vers une voie. Le chat des diffuseurs Premium (statut paid ou vip),
ainsi que d'un ensemble configuré d'identifiants spéciaux toujours premium, passe par
`twitch.ingress.event.premium` ; tous les autres par `.standard`. Les événements de début et de fin
de stream vont aussi sur `.stream`. Sesame propage la priorité entrante jusqu'à sa sortie : les réponses
partent donc sur `twitch.outgress.premium` ou `twitch.outgress.standard`. Les tâches de contrôle,
comme l'enrôlement EventSub, utilisent `twitch.outgress.system`. Les diffuseurs inconnus et les
échecs de recherche sont routés en standard.

## Flux d'une requête (commande de chat)

1. Twitch livre un événement de chat à un shard **ingress** via le WebSocket du Conduit EventSub.
2. L'ingress le normalise et le publie sur `twitch.ingress.event.{premium|standard}`.
3. **Sesame** consomme la voie, charge les modules et commandes de la chaîne depuis la projection
   (Valkey, avec repli par RPC auprès du propriétaire), applique les contrôles et construit une réponse.
4. La réponse est publiée sur `twitch.outgress.{premium|standard}`.
5. **outgress** applique la limite de débit par diffuseur et envoie via Helix. Twitch associe
   `sender_id` à l'autorisation `user:bot` du bot et à l'autorisation `channel:bot` du diffuseur, ce qui
   affiche le badge Chat Bot sur les réponses éligibles.

## Infrastructure

- **Cluster** : k3s avec un nœud de plan de contrôle ARM portant une taint (`cp1`) et trois nœuds de
  travail étiquetés `role=node`. Les images sont compilées nativement pour ARM et Intel et tirées depuis GHCR.
  Voir [Matériel et cluster →](/fr/infrastructure/hardware-and-cluster/).
- **Réseau** : le trafic public entre par le tunnel sortant **cloudflared** vers Traefik. Les opérateurs
  atteignent la console admin, la supervision NATS et l'API Kubernetes via Tailscale. Cilium fournit le CNI,
  et chaque namespace part de politiques de refus par défaut. Voir [Réseau →](/fr/infrastructure/networking/).
- **Livraison** : GitHub Actions compile les images et le deployer les épingle par empreinte dans
  `deploy/k8s` (un test CI impose les empreintes). Flux ne tourne plus sur le cluster.
- **NATS** : trois membres de hub conservent chaque flux JetStream en R3, plus un DaemonSet leaf auquel
  les services se connectent (`deploy/messaging/README.md`).

## Objectifs

- Les mises à jour se font sans interruption. Comme l'anti-affinité d'un pod par nœud ne laisse aucun
  nœud libre pour un pod supplémentaire, la plupart des Deployments fixent `maxSurge: 0` et
  `maxUnavailable: 1` (`deploy/k8s/*.yaml`).
