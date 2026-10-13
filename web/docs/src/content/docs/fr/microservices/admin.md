---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Administration
description: Console SvelteKit réservée au staff pour les comptes, la flotte de shards, les voies, les concours, les essais et les déploiements, accessible via le tailnet.
---

La console Admin (`web/admin/`) est l’application SvelteKit (SSR) des opérateurs. Elle remplace l’ancien outil Go d’administration en lecture seule. Elle ne possède aucune donnée : chaque lecture et chaque écriture est un RPC NATS vers le service propriétaire, et elle n’ouvre jamais MySQL.

L’accès comporte deux couches. La première est le réseau : l’application n’est servie que par l’ingress Tailscale (`ORIGIN` est le nom du tailnet, `deploy/k8s/console-admin.yaml`), sans nom d’hôte public. La seconde est l’identité : l’opérateur se connecte avec Twitch et doit être un membre actif du staff. Chaque action refait sa propre vérification de rôle.

## Responsabilités

- **Shards** : état en direct de la flotte de shards de [Twitch Ingress](/fr/microservices/twitch-ingress/) (`twitch.ingress.admin.shards.get`, avec des commandes de mise à l’échelle et d’autoscale), actualisé par les événements `twitch.ingress.status.>`.
- **Utilisateurs** : consulter les comptes, bannir, accorder un accès, définir des indicateurs de test, réinitialiser un compte, relancer ses abonnements EventSub et ouvrir une session « voir en tant que » sur le tableau de bord d’un diffuseur (`bagel.rpc.admin.user.*`).
- **Voies** : inspecter et administrer les pipelines JetStream et leurs débits de livraison.
- **Concours** : créer des campagnes, prévisualiser et figer l’ensemble des candidats, tirer les gagnants et relancer les attributions via [Transactions](/fr/microservices/transactions/).
- **Essais** : gérer les chaînes d’essai observées (`twitch.ingress.admin.trials.*`).
- **Notifications, audit, staff et secrets** : envoyer des annonces via [Notifications](/fr/microservices/notifications/), lire le journal d’audit des opérateurs, gérer les rôles du staff et renouveler les identifiants des services.
- **Déploiements** : exécutions de déploiement réservées au propriétaire, avec plan, approbation et événements en direct, appuyées par les adaptateurs RPC de déploiement.

## Contrôle d’accès

Les rôles du staff sont `moderator`, `admin` et `owner`. Une table de rôles unique associe chaque clé d’accès nommée au rôle minimal autorisé. Les mutations passent par des assistants audités. Un bouton masqué dans l’interface est une commodité, pas une protection.

## Déploiement

`deploy/k8s/console-admin.yaml` exécute 3 réplicas avec des mises à jour progressives et un PodDisruptionBudget (`minAvailable: 1`). Il est exposé par un Ingress de classe `tailscale`. La configuration (secret Doppler `console-admin-env`) comprend les points d’accès NATS et Valkey et les préfixes de sujets RPC utilisés (`NATS_ADMIN_SUBJECT`, `NATS_ADMIN_USER_SUBJECT_PREFIX`, `NATS_ADMIN_DEPLOY_SUBJECT_PREFIX`, entre autres). Voir [Réseau](/fr/infrastructure/networking/) pour le modèle à deux plans qui donne son sens à « tailnet uniquement ».

## Références

- [Twitch Ingress](/fr/microservices/twitch-ingress/) : le service dont cette console observe les shards.
- [Réseau](/fr/infrastructure/networking/) : la posture du tailnet.
- [Contrats RPC](/fr/reference/rpc-contracts/) : la surface `bagel.rpc.admin.*`.
