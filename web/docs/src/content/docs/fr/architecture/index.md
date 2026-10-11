---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Vue d'ensemble du système
description: "La vue à 10 000 pieds d'ItsBagelBot : acteurs, frontières et systèmes avec lesquels il communique."
---

ItsBagelBot est un bot Twitch hautement disponible et entièrement évolutif, qui s'étend à de nouveaux services en ajoutant les workers d'ingress appropriés. Les streamers multi-plateformes peuvent ainsi être pilotés depuis une seule configuration.

Nous gardons l'empreinte de chaque service réduite afin de limiter le coût matériel et énergétique du système.

Cette page présente la **vue C4 niveau 1 (contexte système)**. Elle masque volontairement l'intérieur du bot et montre uniquement les acteurs qui interagissent avec lui et les systèmes externes dont il dépend. Pour voir l'intérieur de la boîte, consultez les [microservices →](/fr/microservices/). Pour sa forme en fonctionnement (plan de données, bus et flux des requêtes), consultez [l'état du système →](/fr/reference/system-overview/).

## Diagramme de contexte

```mermaid
flowchart TB
    Broadcaster["Broadcaster / chatter<br/><i>Utilisateur Twitch</i>"]
    Operator["Opérateur<br/><i>appareil tailnet</i>"]

    subgraph Bagel ["ItsBagelBot"]
        Core["Système du bot<br/><i>microservices sur NATS</i>"]
    end

    Twitch["Twitch<br/><i>EventSub + API Helix</i>"]
    Discord["Discord<br/><i>passerelle + API REST</i>"]
    Tebex["Tebex<br/><i>achats / monétisation</i>"]
    Providers["Fournisseurs d'API externes<br/><i>via Gossip</i>"]
    NewRelic["New Relic<br/><i>observabilité</i>"]

    Broadcaster -- "utilise la console<br/>(OAuth, réglages)" --> Core
    Operator -- "console admin / kubectl<br/>(tailnet uniquement)" --> Core

    Twitch -- "événements EventSub" --> Core
    Core -- "chat + gestion EventSub" --> Twitch
    Discord -- "événements de la passerelle" --> Core
    Core -- "actions REST" --> Discord
    Tebex -- "webhooks d'achat" --> Core
    Core -- "recherches typées" --> Providers
    Core -- "télémétrie" --> NewRelic
```

## Acteurs et systèmes externes

| Partie | Relation |
|---|---|
| **Broadcaster / chatter** | Pilote le bot via le chat Twitch et EventSub ; le configure via la console (OAuth Twitch avec oauth4webapi). |
| **Opérateur** | Exploite le bot. Accède à la console admin et à `kubectl` uniquement via le tailnet Tailscale ; aucun chemin public. |
| **Twitch** | Source des événements EventSub (ingress) et cible des messages de chat et de la gestion EventSub (outgress), via l'API Helix et le jeton du compte du bot. |
| **Discord** | Source des événements de la passerelle (ingress Discord) et cible des actions REST (outgress Discord). |
| **Fournisseurs d'API externes** | Sources de données tierces, comme des statistiques de jeux et des services musicaux. Seul Gossip les appelle, avec des identifiants résolus au moment de la requête. |
| **Tebex** | Monétisation. Les achats alimentent le service transactions et font passer le niveau d'un broadcaster en payant. |
| **New Relic** | Destination d'observabilité pour les services Go et Elixir. |

La surface publique du bot comprend la console du broadcaster et ses pages publiques (commandes, statistiques, classements), les points d'état et le récepteur de webhooks Tebex, tous servis par le tunnel cloudflared sortant. Tout ce qui concerne l'opérateur est limité au tailnet. À l'intérieur de la boîte, les services communiquent exclusivement sur NATS.
