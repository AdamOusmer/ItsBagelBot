---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "Normes de documentation"
description: "Types de pages, rédaction, diagrammes et exigences de revue pour la documentation technique."
docType: reference
---

Ces conventions s’appliquent au site technique. Chaque page répond à un besoin précis, identifie le code concerné et distingue le comportement implémenté d’une proposition. Le code et ses contrats restent la référence pour le comportement à l’exécution.

## Types de pages

Utiliser [Diátaxis](https://diataxis.fr/start-here/) pour séparer l’apprentissage, l’exécution d’une tâche, la consultation et la compréhension. Les décisions et les preuves datées ont des exigences supplémentaires.

| `docType` | Besoin du lecteur | Contenu requis |
| --- | --- | --- |
| `tutorial` | Apprendre par un exercice contrôlé | État initial, étapes, résultats observables, nettoyage |
| `how-to` | Accomplir une tâche précise | Objectif, prérequis, procédure, vérification, dépannage pertinent |
| `reference` | Consulter un contrat ou un composant | Périmètre, noms et champs exacts, valeurs par défaut pertinentes, exemples, limites, sources |
| `explanation` | Comprendre l’architecture | Périmètre, composants, relations, invariants, compromis, comportement en cas d’échec, sources |
| `decision` | Comprendre un choix important | Date originale, statut, contexte, décision, conséquences, références |
| `record` | Évaluer un test ou une revue achevés | Date, commit source, méthode, environnement, résultat, preuves, limites |

Choisir le type avant de rédiger. Une page de service est une référence ; une procédure pour modifier ce service est un guide pratique. Les relier. Ne pas présenter un essai prévu comme un rapport achevé ni une liste de commandes comme un tutoriel.

## Rédaction et organisation

Le [guide de Google](https://developers.google.com/style/highlights) inspire les conventions : voix active, titres en casse de phrase, liens descriptifs et formatage précis du code. Conserver le ton direct du projet. Donner d’abord le résultat ou la responsabilité, nommer l’acteur et expliquer la condition avant l’action qu’elle modifie.

Pour les procédures, suivre les [conseils de Microsoft](https://learn.microsoft.com/en-us/style-guide/procedures-instructions/writing-step-by-step-instructions) : préciser le point de départ, numéroter les actions dans l’ordre et donner un critère d’achèvement observable. Séparer les environnements alternatifs pour permettre de suivre un seul parcours.

- Le titre de page est le seul titre de niveau un ; Astro le produit à partir du frontmatter. Commencer les sections du corps au niveau deux sans sauter de niveau.
- Utiliser des paragraphes courts pour expliquer, des tableaux pour les champs ou responsabilités comparables et des listes numérotées pour les procédures.
- Mettre les identifiants, chemins, sujets, variables et champs JSON en code. Conserver leur orthographe et leur casse.
- Définir les termes propres au projet dans le [glossaire partagé](https://github.com/AdamOusmer/ItsBagelBot/blob/main/CONTEXT.md). Éviter plusieurs sens pour le même terme.
- Donner des exemples concrets avec des identifiants synthétiques. Préciser le répertoire de travail et les prérequis externes. Décrire le résultat attendu sans inventer une sortie réussie.
- Lier le code propriétaire et les guides associés. Définir un contrat à un seul endroit et y renvoyer depuis ses consommateurs.

## Références de services

Pour un service implémenté, couvrir les responsabilités, interfaces, état et dépendances, échecs, emplacements du code et validation. Garder seulement les sections utiles. Un squelette ou une image exige un statut d’implémentation clair et son périmètre réel, sans interfaces fictives.

Distinguer le travail accepté, l’état durable, la livraison acquittée et l’effet externe achevé. Décrire reprises, expiration, ordre, déduplication et lectures périmées dans les limites du code cité. Un test unitaire ciblé ne démontre pas un comportement distribué ou en production.

## Diagrammes d’architecture

Choisir un seul niveau de détail par diagramme. Les [conseils du modèle C4](https://c4model.com/diagrams) distinguent contexte système, conteneurs et composants ; retenir le niveau adapté à la question. Un diagramme d’interactions entre services et une topologie de déploiement répondent à des besoins différents.

Utiliser les organigrammes Mermaid pour les relations, les séquences pour le temps et les messages, et les diagrammes d’état pour les transitions. Nommer les flèches selon l’opération ou la donnée transportée. Conserver des identifiants stables, mettre entre guillemets les libellés avec ponctuation et expliquer les chemins omis dans le texte. Ajouter une explication textuelle pour ne pas dépendre seulement du diagramme ou de la couleur.

Exécuter `bun run check:diagrams`. Examiner la page sur ordinateur et mobile si sa mise en page ou son diagramme change.

## Décisions et historique

Conserver les identifiants ADR séquentiels et les dates originales. Créer une nouvelle décision lorsqu’un compromis accepté change ; marquer l’ancienne comme remplacée et relier les deux. Décrire seulement les alternatives dont le raisonnement est établi. Ne pas déduire un motif historique du code actuel.

Conserver les routes publiées par redirection lors d’un déplacement. Si une ancre change, modifier les liens entrants dans la même modification. Le français conserve le chemin relatif et le `docType` de l’anglais ; identifiants et commandes gardent leur orthographe. Les autres langues configurées utilisent le repli anglais documenté.

## Rédaction et revue

Partir de `web/docs/templates/` ou utiliser `bun run adr:new "Decision title"`. Ajouter chaque nouvelle page dans `src/lib/spaces.ts` et créer sa version française. Garder les modèles hors de la collection publiée.

Avant la revue, exécuter depuis `web/docs/` :

```sh
bun test src
bun run check:content
bunx playwright install chromium
bun run check:diagrams
bun run build
```

Depuis la racine du dépôt, exécuter `python3 scripts/translations.py check --strict fr`. Installer Chromium seulement si le navigateur manque. Le job CI de documentation répète les vérifications automatisées pour les modifications pertinentes.

Vérifier les affirmations modifiées dans l’implémentation propriétaire, pas seulement dans une autre page. Les contrôles automatisés couvrent métadonnées, navigation, liens locaux et ancres, cibles source, parité des traductions, numérotation et rendu des diagrammes. Ils ne prouvent ni la vérité d’une explication technique ni la fidélité d’une traduction. Cette évaluation appartient à la revue de code.
