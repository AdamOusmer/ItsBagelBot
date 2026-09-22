---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "Console"
description: "Application SvelteKit pour le tableau de bord des diffuseurs et l’interface d’administration."
---

La Console (`console/`) est une application SvelteKit (SSR) qui constitue l’interface principale des diffuseurs et des opérateurs du système.

## Architecture

- **Tableau de bord des diffuseurs** : gestion en libre-service des commandes personnalisées, de l’activation des modules et de la consultation des notifications.
- **Interface d’administration** : vues et contrôles privilégiés pour les opérateurs, par exemple surveillance de la flotte de shards, gestion globale des utilisateurs et annonces à tout le système.
- **Authentification** : les utilisateurs s’authentifient par OAuth Twitch avec la bibliothèque `oauth4webapi`. Les autorisations sont vérifiées et enregistrées par le service [Users](/fr/microservices/users/) via `grant_save`.
- **Communication** : la Console ne se connecte pas à la base de données. Elle communique exclusivement avec les services Go par RPC NATS via un adaptateur interne.
- **Exposition** : hébergée derrière un tunnel `cloudflared`, elle est exposée de façon sécurisée sur Internet pour les utilisateurs finaux, tandis que l’accès aux sections administratives est strictement contrôlé.
