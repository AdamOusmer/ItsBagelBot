---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Architecture Decision Records
description: "Le journal des décisions d'ItsBagelBot et les raisons de la forme actuelle du système."
---

Les Architecture Decision Records (ADR) consignent le **raisonnement** derrière les choix d'architecture : le contexte, l'option retenue, ses conséquences et les alternatives étudiées.

Les ADR sont **immuables** une fois acceptés. Si une décision ultérieure change de direction, nous n'éditons pas l'original : nous rédigeons un nouvel ADR qui le remplace et mettons à jour le statut de l'ancien pour pointer vers l'avenir.

## Le journal

Les ADR apparaissent dans la barre latérale par ordre numérique. Chaque nom suit le modèle `NNNN-kebab-case-title.md`.

## Rédiger un ADR

Depuis le répertoire `docs/` :

```sh
bun run adr:new "Short title of the decision"
# ou : npm run adr:new "Short title of the decision"
```

Le wrapper utilise le modèle du projet (`docs/.adr/template.md`) et écrit le nouveau fichier dans ce dossier avec le prochain numéro séquentiel.

Pour remplacer ou relier un enregistrement existant :

```sh
bun run adr:new -- -s 3 "Replaces decision 3"
bun run adr:new -- -l "3:Amends:Amended by" "Amends decision 3"
adr list
```

## Quand faut-il un ADR ?

Rédigez un ADR lorsqu'un choix est :

- **Important pour l'architecture :** il façonne les autres composants ou limite les options futures.
- **Coûteux à inverser :** revenir dessus demanderait une coordination entre services, infrastructure ou données.
- **Non évident :** un mainteneur futur demanderait raisonnablement « pourquoi de cette façon ? » en lisant le code.

Les refactorings quotidiens, corrections de bugs et préférences de nommage n'ont pas besoin d'un ADR. Les messages de commit et descriptions de PR sont adaptés.

## Anatomie d'un enregistrement

Chaque ADR contient :

- **Statut :** Proposed, Accepted, Deprecated ou Superseded by `[ADR-NNNN]`.
- **Contexte :** contraintes, exigences, décisions antérieures et incidents à l'origine du choix.
- **Décision :** le choix, à la voix active (« Nous allons… »).
- **Conséquences :** ce qui devient plus simple, plus difficile ou plus risqué.
- **Alternatives étudiées :** les options évaluées et les raisons de leur rejet.

Consultez [`adr help`](https://github.com/npryce/adr-tools) pour la référence complète.
