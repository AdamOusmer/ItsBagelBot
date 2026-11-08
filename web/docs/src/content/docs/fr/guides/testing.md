---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Tests
description: "Choisir les vérifications ciblées et trouver les besoins d’intégration."
docType: how-to
---

## Prérequis et portée

Lancez les vérifications depuis la racine du dépôt, sauf indication contraire. Commencez par le paquet modifié, puis élargissez aux appelants et aux contrats associés.

## Lancer les vérifications Go

```sh
go test ./app/projector/...
go test ./internal/projection/...
go test ./...
```

Le workflow Go (`.github/workflows/main.yml`) lance les tests par groupes avec le détecteur de courses et exécute le build guard sans cache. Utilisez `go test -race ./path/to/package/...` pour une vérification ciblée de concurrence. Certains tests d'intégration exigent des services externes ou des variables explicites; lisez le test et le `context.md` du service avant de supposer qu'un test ignoré a exercé un système externe.

## Lancer les vérifications web et ingress

Pour le web :

```sh
cd web
bun install --frozen-lockfile
bun run check
bun run test
```

Pour la documentation, lancez `bun run build` depuis `web/docs/`. Pour l'entrée Twitch, lancez `mix deps.get` puis `mix test` depuis `app/twitch/ingress/`. La CI utilise `MIX_ENV=test` pour ses tests (`.github/workflows/ingress.yml`). Consultez le workflow concerné pour son environnement complet et ses autres vérifications; les tests unitaires locaux ne remplacent pas les tests d'intégration ou de bout en bout.

## Vérifications de la documentation

Depuis `web/docs/`, lancez `bun run check:content` pour le contenu et `bun run check:diagrams` pour le rendu Mermaid. La vérification des diagrammes lance Chromium avec Playwright. Après l'installation des dépendances de l'espace web, installez le navigateur avec `bun x playwright install chromium` s'il n'est pas déjà disponible. L'implémentation est dans [check-content.ts](https://github.com/AdamOusmer/ItsBagelBot/blob/main/web/docs/scripts/check-content.ts). Lancez aussi le build de la documentation après une modification de navigation ou de rendu; ces vérifications couvrent des échecs différents.

## Vérifier et résoudre les problèmes

Un code de sortie zéro indique que la commande sélectionnée a réussi. Lisez les tests ignorés et leur configuration avant de considérer un parcours d'intégration couvert. En cas d'échec de connexion, vérifiez le système externe et les variables indiqués par le test ou le `context.md` du service. Si le contrôle des diagrammes ne lance pas Chromium, installez le navigateur Playwright comme indiqué ci-dessus. Élargissez les tests ciblés aux vérifications du workflow pertinent avant de soumettre une modification entre services.

Voir [Configuration pour le développement](/fr/guides/getting-started/) pour les dépendances et [Contrats RPC](/fr/reference/rpc-contracts/) pour les limites des contrats.
