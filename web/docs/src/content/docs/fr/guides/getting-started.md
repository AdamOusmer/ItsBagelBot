---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Configuration pour le développement
description: "Commandes et points de départ pour contribuer."
docType: how-to
---

Ce guide aide les contributeurs à lancer les vérifications et un serveur de développement. Pour configurer une chaîne, consultez le [guide des diffuseurs](https://itsbagelbot.com/guides/getting-started/). Ces parcours sont indépendants; aucun ne démarre une pile complète de services.

## Prérequis

Installez les versions indiquées dans `go.mod`, `web/package.json` et, pour l'entrée Twitch, `app/twitch/ingress/mix.exs`. Certains tests d'intégration exigent NATS, Valkey, MySQL ou des variables propres au service; consultez le test et son `context.md` pour leur configuration.

## 1. Vérifier les services Go

Depuis la **racine du dépôt**, lancez :

```sh
go test ./...
```

La commande se termine sans erreur si les tests sélectionnés réussissent. Examinez les tests ignorés avant de considérer un parcours d'intégration couvert.

## 2. Vérifier et lancer l'espace web

Dans un terminal distinct, partez de la **racine du dépôt** :

```sh
cd web
bun install --frozen-lockfile
bun run check
bun run test
bun run dev:dashboard
```

Les vérifications doivent se terminer sans erreur. La dernière commande reste active et affiche une adresse locale; arrêtez-la avec Ctrl+C. Pour lancer plutôt la documentation, partez de la **racine du dépôt** dans un terminal distinct :

```sh
cd web/docs
bun run dev
```

Le serveur de documentation affiche son adresse locale et reste actif jusqu'à son arrêt.

## 3. Vérifier l'entrée Twitch

Dans un terminal distinct, partez de la **racine du dépôt** :

```sh
cd app/twitch/ingress
mix deps.get
mix test
```

La commande de test doit se terminer sans erreur. Cette étape n'est nécessaire que pour travailler sur l'entrée Elixir.

## Dépannage et suite

Si l'installation des dépendances échoue, vérifiez les versions requises. Si un test d'intégration ne peut pas se connecter, vérifiez ses systèmes externes et ses variables; ces commandes ne les provisionnent pas. Voir [Tests](/fr/guides/testing/) pour les vérifications ciblées et [État du système](/fr/reference/system-overview/) pour trouver les services.
