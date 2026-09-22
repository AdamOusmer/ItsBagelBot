---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: État du système
description: "La forme actuelle d'ItsBagelBot : services, plan de données, bus de messages et transformation d'un message de chat en réponse."
---

Cette page documente le système **tel qu'il fonctionne aujourd'hui**, après le
passage aux microservices de données par schéma, au bus NATS, à l'ingress Elixir
et à la console SvelteKit. Lorsque d'anciennes pages mentionnent encore
« RabbitMQ » ou `services/twitch-ingress/`, cette page fait foi.

## Vue d'ensemble

- **Microservices**, chacun consacré à une fonction et déployable indépendamment pour des mises à jour sans interruption.
- **Go** pour les services de données et les workers, **Elixir/OTP** pour l'ingress Twitch, **SvelteKit (SSR)** pour la console opérateur/utilisateur.
- **NATS** est le seul transport interservices : pub/sub par sujet pour les événements, request-reply pour les RPC. Aucun service ne lit la base d'un autre.
- **MySQL HeatWave**, avec un schéma par service de données, accessible via `ent`.
- **Valkey** conserve la projection des réglages/niveaux et les caches temporaires.
- Hébergé sur **Oracle Cloud (PAYG, Canada)**, avec deux nœuds k3s, livré par **Flux GitOps** derrière un tunnel **cloudflared** sur un réseau **Tailscale**.

## Services

| Service | Chemin | Langage | Responsabilité |
|---|---|---|---|
| **ingress** | `app/twitch/ingress/` | Elixir | Twitch EventSub Conduit et shards WebSocket ; supervision par shard ; OAuth des tenants ; normalisation et publication des événements sur `twitch.ingress.event.*` ; RPC d'instantané des shards |
| **outgress** | `app/twitch/outgress/` | Go | Envoi vers Twitch (chat et gestion EventSub) ; limitation par chaîne via Valkey ; registre des chaînes ; cycle de vie des jetons d'application et d'utilisateur ; coupe-circuit |
| **projector** | `app/projector/` | Go | Construit la projection Valkey des réglages lors de stream-online ; sert les recherches de **niveau** de facturation des chaînes |
| **users** | `app/db/users/` | Go | Comptes utilisateur, statut (free/paid/vip), activation, jetons OAuth Twitch ; émet `data.users.*` |
| **commands** | `app/db/commands/` | Go | Commandes de chat personnalisées ; émet `data.commands.changed` |
| **modules** | `app/db/modules/` | Go | Modules de fonctionnalités par chaîne ; émet `data.modules.changed` |
| **transactions** | `app/db/transactions/` | Go | Enregistrements d'achats Tebex ; consomme `data.transactions.recorded` |
| **admin** (ancien) | `app/admin/` | Go + templ | Vue opérateur en lecture seule sur NATS (parc de shards, utilisateurs) ; progressivement remplacée par la console |
| **console** | `console/` | SvelteKit SSR | Applications `dashboard` (libre-service des chaînes) et `admin` (opérateur) ; OAuth oauth4webapi ; communication avec les services uniquement via RPC NATS |

## Plan de données

- **Base de données** : MySQL HeatWave. Chaque service de données possède son
  propre schéma et en est le seul écrivain. Les lectures interservices passent
  par les RPC NATS, jamais par SQL.
- **ORM** : `ent`, avec du code généré pour chaque service sous `app/<svc>/ent/`.
- **Projection** : Valkey stocke la projection des réglages/niveaux par chaîne
  lue par le chemin critique. Les écritures sont **write-behind** (~2 s) : le
  tableau de bord renvoie une vue optimiste, la base est mise à jour en différé
  et un événement d'invalidation reconverge les lecteurs.
- **Cache** : des caches LRU en mémoire, de courte durée, devant Valkey (par
  exemple le cache de niveaux du projector, TTL de 30 s) absorbent les lectures répétées.

## Bus de messages

NATS transporte deux classes de trafic :

- **Événements** (pub/sub fire-and-forget). Événements de changement de domaine
  `data.*`, événements ingress `twitch.ingress.event.*`, état des shards ingress
  `twitch.ingress.status.*`, voies d'envoi outgress `twitch.outgress.*` et
  diffusion d'invalidation du cache `bagel.cache.invalidate.broadcaster`.
- **RPC** (request-reply). Tout ce qui se trouve sous `bagel.rpc.*`, ainsi que
  l'instantané des shards ingress `twitch.ingress.admin.shards.get`. Voir les
  [contrats RPC →](/fr/reference/rpc-contracts/).

### Voies premium / standard

Le trafic outgress est réparti selon le statut de la chaîne. Les chaînes de niveau premium (paid
ou vip), ainsi qu'un ensemble configuré d'identifiants spéciaux toujours premium, passent par
`twitch.outgress.premium` ; les autres passent par `twitch.outgress.standard`.
Les messages système utilisent `twitch.outgress.system`. Le chat hors commande provenant d'utilisateurs
non spéciaux et non premium est abandonné avant d'atteindre une voie.

## Flux d'une requête (commande de chat)

1. Twitch remet un événement de chat à un shard **ingress** via le WebSocket EventSub Conduit.
2. Ingress le normalise et le publie sur `twitch.ingress.event.{premium|standard}`.
3. Le worker propriétaire consomme la voie, résout la commande via la projection **commands**
   (ou un RPC), puis produit une réponse.
4. La réponse est publiée sur `twitch.outgress.{premium|standard|system}`.
5. **outgress** applique la limite par chaîne et envoie via Helix avec
   le jeton d'accès de l'application. Twitch associe `sender_id` à l'autorisation précédente du bot
   `user:bot` / `user:write:chat`
   et à celle `channel:bot` de la chaîne, ce qui permet aux réponses éligibles d'afficher le badge Chat Bot.

## Infrastructure

- **Cloud** : Oracle Cloud Infrastructure, paiement à l'usage, région canadienne.
- **Cluster** : deux nœuds k3s. `node1` est ARM (`opc@`), `node2` est Intel (`ubt@`).
  Les images sont construites nativement sur chaque nœud puis importées dans k3s avec `ctr`, sans émulation interarchitectures.
- **Réseau** : les nœuds n'ont pas d'IP publiques. Tout le trafic passe par le réseau Tailscale ;
  l'accès public se fait via le tunnel sortant **cloudflared**. Traefik et cloudflared s'exécutent
  comme DaemonSets par nœud avec routage vers le nœud le plus proche.
- **Mesh** : sidecar natif Linkerd.
- **Livraison** : **Flux CD** en mode pull depuis GHCR avec des images épinglées par digest. Les anciens
  scripts racine de build/déploiement SSH sont obsolètes.
- **Configuration NATS** : les fichiers `.conf` sont gérés par Flux via un `configMapGenerator` kustomize
  dans `deploy/k8s` ; un push git les recharge à chaud par le sidecar `config-reloader` (`SIGHUP`), sans redémarrage de pod.

## Objectifs

- Latence p99 de la console SSR : ≤ 200 ms.
- Les déploiements se font sans interruption ; sur le cluster à deux nœuds, les mises à jour utilisent
  `maxSurge=0 / maxUnavailable=1` pour éviter le blocage lié à l'anti-affinité stricte des pods, et les
  images sont importées sur les deux nœuds avant toute mise à jour de digest.
