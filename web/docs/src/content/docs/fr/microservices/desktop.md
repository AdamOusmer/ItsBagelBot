---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Application de bureau
description: Implémentation et périmètre actuel du client de bureau.
docType: reference
---

L’application de bureau dans `app/desktop/` utilise Wails et une interface Svelte. La version de production affiche l’écran de connexion. Une prévisualisation de l’interface est accessible en développement avec `?screen=shell` ; elle utilise des données d’exemple. L’appairage des appareils et la conservation des identifiants ne sont pas implémentés dans ce client, même si la [passerelle de contrôle](/fr/microservices/controls-gateway/) possède un contrat de connexion côté serveur.

## Responsabilités

`main.go` intègre la compilation de l’interface et ouvre la fenêtre. L’application possède son propre module Go afin de séparer ses dépendances de celles des serveurs. Les liens Web s’ouvrent dans le navigateur système.

## État et dépendances

L’appairage et les identifiants persistés ne sont pas encore implémentés.

## Code et validation

Pour vérifier l’interface, exécuter `bun run check` depuis `app/desktop/frontend/`. Le [README](https://github.com/AdamOusmer/ItsBagelBot/blob/main/app/desktop/README.md) décrit les commandes de développement et de création des paquets ; voir [App.svelte](https://github.com/AdamOusmer/ItsBagelBot/blob/main/app/desktop/frontend/src/App.svelte) et [l’état d’authentification](https://github.com/AdamOusmer/ItsBagelBot/blob/main/app/desktop/frontend/src/lib/auth.svelte.ts) pour le comportement actuel.
