---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0012 - Le manifeste Kit est l'inventaire public"
description: "ADR : le resolver du bot possède le comportement, le manifeste Kit possède l'inventaire public et les textes, et une fixture Go lie les deux."
---

**Date :** 2026-09-18

## Statut

Accepté. Implémenté dans les branches empilées à partir de `feat/kit-variables-manifest`.

## Contexte

Les variables de réponse étaient décrites à six endroits maintenus à la main entre Go, Kit, le tableau de bord et le marketing, sans vérification entre eux. Les listes divergeaient : exemples différents, alias absents d'un côté, variable présente uniquement dans les chips du tableau de bord.

## Décision

- La chaîne de scopes Go (`app/twitch/sesame/engine/scope`) est l'unique autorité sur l'expansion d'un token.
- `web/kit/lib/variables/` est l'unique autorité sur les variables publiques, leurs formes, exemples, catégories et clés de texte. Le tableau de bord, le builder marketing et le guide en dérivent.
- Une fixture golden (`scope/testdata/token_catalog.golden.json`), écrite par le test Go avec un flag et lue par un test Kit, échoue en CI lorsque les deux inventaires divergent dans un sens ou dans l'autre.

## Alternatives rejetées

- Générer le manifeste TypeScript depuis Go au build : cela impose un build Go à la CI web, tandis que les textes auraient toujours besoin d'un emplacement de locale extérieur à Go.
- Garder le catalogue marketing comme source et faire importer le tableau de bord : cela inverse la direction de dépendance (le marketing dépend déjà de Kit).
- Manifeste sans test de parité : le statu quo qui a produit la dérive.

## Conséquences

- Ajouter une variable demande une modification Go, une modification Kit et des clés de locale ; oublier l'une d'elles fait échouer un test.
- Le sens propre à une surface d'un nom partagé (`{user}` sur une alerte de follow) vit dans la réponse du module du catalogue de modules Kit, pas sur la variable.
