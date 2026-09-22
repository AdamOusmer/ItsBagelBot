---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "Dingress"
description: "Passerelle Discord à instance unique pour l’accueil, les salons vocaux, les tickets, les journaux et les commandes slash."
---

Dingress (`app/dingress/`) constitue la partie **passerelle** Discord de Bagel. Outgress conserve la partie REST : annonces de direct intégrées, archivage des clips, remplissage du serveur en un clic, copies des raids et cadeaux. Dingress détient l’unique session Identify du jeton de bot de la flotte et transforme les événements de la passerelle en opérations communautaires.

Deux sessions Identify utilisant le même jeton entrent en conflit. La passerelle dispose donc de son propre Deployment, avec **replicas: 1** et une stratégie Recreate ; ce n’est pas une réplique d’outgress.

## Ce qu’il remplace

Un seul bot Bagel remplace l’ensemble habituel des bots Discord d’un streamer :

| À la place de | Bagel propose |
|---|---|
| Welcomer | Message d’accueil intégré dans `#welcome`, attribution automatique du rôle Member, message de départ facultatif |
| TempVoice | Création d’un salon vocal en rejoignant `+ Create voice`, limite de 12 clones, boutons de verrouillage/déverrouillage dans le salon |
| Ticket Tool | Panneau d’assistance avec **Ouvrir un ticket** ; chaque ticket est un message intégré avec **Fermer le ticket** |
| Sapphire (modération/journaux) | `/timeout` `/kick` `/ban` `/purge` et `#logs` |
| OwO | Miettes de chat (15/message, délai de 60 s) ; rang et récompense quotidienne sous forme de cartes intégrées avec **Récupérer la récompense quotidienne** |
| Urchin | Déjà un module Sesame Twitch (`!daily` et commandes associées). Les commandes slash Discord pour Urchin sont reportées jusqu’à ce que gossip puisse répondre depuis dingress. |

Les copies des annonces de direct, clips, raids, cadeaux et étapes importantes restent sur le chemin REST associé dans [Outgress](/fr/microservices/outgress/).

## Architecture

Dingress **ne s’abonne pas** à NATS. Il :

1. Se connecte à `wss://gateway.discord.gg/?v=10&encoding=json` avec les intents de serveurs, membres, états vocaux, messages de serveur et contenu des messages.
2. Résout chaque serveur par la clé Valkey `discord:guild:{id}`, l’index inverse écrit par outgress lors de la configuration ou de la dissociation.
3. Charge le bloc du module Discord depuis le stockage du projector, afin d’appliquer les réglages du tableau de bord sans appel RPC supplémentaire.
4. Écrit les clones, tickets et compteurs de miettes dans Valkey (`discord:voice:`, `discord:ticket:`, `discord:xp:`).

Si `DISCORD_BOT_TOKEN` est vide, le processus reste inactif : les contrôles de santé répondent toujours, mais la passerelle reste déconnectée.

### Intents privilégiés

Les options Server Members Intent et Message Content Intent doivent être activées dans l’application Discord. Sans elles, ni les messages d’accueil ni les rangs de miettes ne se déclenchent.

### Panneaux destinés aux membres

Les tickets, salons vocaux et miettes utilisent des **messages intégrés avec des boutons**. Les commandes slash restent enregistrées (`/ticket`, `/voice`, `/timeout`, `/kick`, `/ban`, `/purge`, `/daily`, `/rank`) comme solution de repli et pour le renommage ou les limites, qui nécessitent une saisie.

## Déploiement

Le projet Doppler `dingress-env` contient `DISCORD_BOT_TOKEN` (même valeur que dans `outgress-env`), `VALKEY_PASSWORD` et New Relic. Le jeton de bot est le seul identifiant Discord ; il n’est jamais conservé dans le bloc de configuration du module.
