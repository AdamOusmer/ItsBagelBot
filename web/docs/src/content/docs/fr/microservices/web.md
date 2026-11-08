---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Marketing site
description: "Construit le site public Astro, son contenu localisé et le créateur de commandes."
docType: reference
---

## Responsabilités

Le créateur permet d’essayer des modèles dans le navigateur ; il ne sauvegarde pas la configuration et n’exécute pas les commandes en direct. Les catalogues et les aperçus partagés sont dans `web/kit/`.

## Interfaces

Les catalogues de langues et les routes sont dans `src/i18n/` ; les guides et le créateur de commandes utilisent les définitions partagées de Kit. Les exemples du créateur illustrent les modèles et ne constituent pas une configuration enregistrée.

## État et dépendances

Kit fournit les catalogues et aperçus partagés. Le site marketing ne possède pas les réglages des chaînes.

## Comportement en cas d’échec

Les exemples sont des aperçus locaux et ne prouvent pas qu’une commande a été enregistrée ou exécutée.

## Code

Source : [web/marketing/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/web/marketing). Voir `astro.config.mjs`, `src/i18n/`, `src/content/` et `src/components/builder/`.

## Validation

Depuis `web/marketing/`, utiliser les scripts de `package.json` pour construire et vérifier le contenu.
