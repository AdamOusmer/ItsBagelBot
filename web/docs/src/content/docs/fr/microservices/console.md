---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "Console"
description: "Application SvelteKit pour le tableau de bord des diffuseurs et les pages publiques des commandes."
---

La Console (`web/dashboard/`) est une application SvelteKit (SSR), le tableau de bord destiné aux diffuseurs. Les opérations du staff se trouvent dans l’application distincte [Admin](/fr/microservices/admin/).

## Architecture

- **Tableau de bord du diffuseur** : intégration en libre-service, commandes personnalisées, modules et minuteurs, citations, compteurs, intégrations (Discord, Spotify, Govee), réglages et facturation Premium. Les délégués peuvent utiliser les sections qu’un propriétaire leur accorde, et le staff peut consulter un tableau de bord par une session « voir en tant que » de courte durée.
- **Pages publiques** : le répertoire public des commandes d’une chaîne, ainsi que les hôtes de statistiques, d’état et de classement. Une chaîne peut masquer sa page de commandes ; la masquer ne désactive pas les commandes.
- **Authentification** : les utilisateurs se connectent avec OAuth Twitch (`oauth4webapi`). L’autorisation est vérifiée et enregistrée par le service [Users](/fr/microservices/users/) via `grant_save`.
- **Communication** : la Console ne se connecte pas à la base de données. Ses routes serveur appellent les services Go propriétaires par RPC NATS, via des adaptateurs partagés, avec un cache en processus et une projection Valkey optionnelle devant.
- **Exposition** : routée par le tunnel `cloudflared` vers Traefik sur `dashboard.itsbagelbot.com` et sur les hôtes de statistiques, de commandes et de classement. Seuls les rendus anonymes par défaut sont mis en cache en périphérie.
