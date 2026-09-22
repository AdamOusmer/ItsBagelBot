---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Bien démarrer
description: Guide rapide pour configurer l'écosystème ItsBagelBot.
sidebar:
  order: 1
---

Bienvenue dans **ItsBagelBot**, un écosystème cloud natif et performant pour Twitch.

Ce guide présente les concepts et les étapes nécessaires pour lancer l'environnement local de développement.

## Vue d'ensemble de l'architecture

ItsBagelBot utilise une architecture de microservices découplés qui privilégie la rapidité et la sécurité zero trust :

* **Services backend :** propulsés par **Go** et **Elixir**.
* **Architecture événementielle :** **NATS JetStream** gère la messagerie en temps réel entre les services.
* **Communication temps réel :** les événements principaux passent exclusivement par des **WebSockets**. Les webhooks ne sont pas utilisés.
* **Frontend découplé :**
    * **Site web :** frontend statique optimisé pour un déploiement en périphérie avec Cloudflare Pages.
    * **Tableau de bord :** _en cours de développement_.
* **Infrastructure :** conçue pour fonctionner sur **k3s** avec un modèle réseau Zero Trust.
* **Réseau :** entièrement chiffré et sécurisé avec **Tailscale**, **Cloudflare Tunnels** et **Linkerd**.

## Prérequis

> En cours de rédaction.

## Installation locale

> En cours de rédaction.
