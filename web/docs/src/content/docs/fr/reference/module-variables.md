---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Variables de modules dans les commandes personnalisées
description: "Utiliser les valeurs publiées des modules dans les réponses."
docType: reference
---

## Syntaxe et disponibilité

Une réponse de commande personnalisée peut utiliser une valeur publiée avec `{module:variable}`. Par exemple, `{valorant:tier}` lit une valeur publiée par Valorant. Le module doit être activé et disponible pour le diffuseur; une variable ne l'active pas. Si le module est indisponible, le jeton reste non résolu. Pour un module activé, une valeur de repli comme `{valorant:tier|rang indisponible}` fournit du texte lorsque la valeur est vide.

Le sélecteur de variables du tableau de bord montre les noms disponibles. Si un champ apparaît dans plusieurs vues de réponse, `{module:view:variable}` choisit explicitement une vue. Un minuteur n'a pas de spectateur déclencheur; les valeurs propres au spectateur peuvent donc être vides. Les valeurs propres à un événement nécessitent cet événement; lire une valeur n'exécute pas l'action du module.

## Catalogue et implémentation

Le catalogue public des réponses est `internal/domain/modulevars/catalog.json`, incorporé par `catalog.go`. L'inventaire de l'éditeur se trouve dans `web/kit/lib/variables/`; le traitement à l'exécution est sous `app/twitch/sesame/`. L'inventaire public se limite aux champs publiés, sans configuration privée ni identifiants. Vérifiez le catalogue et les tests de parité en modifiant une variable ou son orthographe.
