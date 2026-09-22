---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0007 - Adoption de microservices de données par schéma"
description: "ADR : microservices de données par schéma comme contextes délimités sous app/, chacun possédant son schéma MySQL."
---

**Date :** 2026-06-09

## Statut

Accepté.

## Contexte

Le système a besoin d'une couche persistante pour quatre types d'état : les comptes utilisateurs avec leurs jetons OAuth et leur niveau payant (free, paid ou vip, niveau payant permanent), les commandes de chat personnalisées, les activations de modules avec leur configuration et les transactions Tebex (uniquement l'identifiant de transaction et l'utilisateur propriétaire ; les détails du paiement restent chez Tebex).

[L'ADR 0005](/fr/adr/0005-adoption-of-mysql-heatwave/) a déjà établi que chaque service possède son schéma MySQL et que les lectures interservices passent par des API et des événements, jamais par des jointures interschémas. Elle n'a pas fixé l'emplacement des frontières. Un premier prototype gardait toutes les entités dans un client ent partagé sous un unique paquet `internal/db`, ce qui rendait facile la violation accidentelle de l'isolation : un client, un schéma et chaque table à une clé étrangère de toute autre.

Trois forces déterminent le découpage :

- Les frontières doivent suivre les changements, pas les tables. Un compte et ses jetons OAuth changent toujours ensemble (une connexion actualise les deux) ; les séparer transformerait chaque connexion en transaction distribuée. Les commandes et les modules changent indépendamment de l'identité et l'un de l'autre.
- Le parcours monétaire a d'autres exigences que celui des réglages. Les transactions et changements de niveau doivent être écrits immédiatement et audités ; une activation de module peut tolérer une courte fenêtre write-behind (voir [ADR 0008](/fr/adr/0008-caching-and-write-behind-strategy/)).
- L'équipe est composée d'une personne. Chaque service supplémentaire est un binaire de plus à déployer, surveiller et comprendre ; le nombre doit donc être justifié par une vraie frontière, pas par symétrie.

## Décision

Nous séparons la couche de données en quatre contextes délimités, chacun étant un service Go autonome sous `app/`, plus le projector (voir [ADR 0009](/fr/adr/0009-adoption-of-valkey-for-the-settings-projection/)) :

- **`app/db/users`** : comptes (identifiant Twitch comme clé primaire, nom, e-mail, indicateur actif, niveau) et jetons OAuth. Les jetons restent ici car ils font partie du cycle de vie de l'identité ; dans le schéma, ils gardent une vraie relation ent vers l'utilisateur avec suppression en cascade. Ils ne sont stockés que comme texte chiffré Tink AEAD et les données associées lient chaque enveloppe à son propriétaire, son type et sa plateforme ; un texte copié sur une autre ligne échoue à l'authentification au déchiffrement.
- **`app/db/commands`** : commandes de chat personnalisées.
- **`app/db/modules`** : activations des modules et configurations JSON.
- **`app/db/transactions`** : enregistrements de transactions Tebex.

Chaque service possède son schéma MySQL (`bagel_users`, `bagel_commands`, `bagel_modules`, `bagel_transactions`), génère son client ent depuis son propre répertoire `ent/schema` et exécute ses migrations au démarrage. Il n'y a aucune clé étrangère entre services : commandes, modules et transactions référencent l'utilisateur par une simple colonne Twitch ID indexée. Seul le service users peut résoudre cet identifiant en utilisateur.

L'infrastructure partagée sans connaissance du domaine vit dans `pkg/` (fournisseur de base, cache, batcher, bus, crypto, logger, monitoring), et les contrats d'événements partagés dans `internal/domain/event/data`. Chaque dépôt valide ses entrées à la frontière (voir les règles de validation de [Data & State](/fr/data-and-state/)).

Les tests suivent le modèle établi : chaque dépôt est testé avec une base SQLite en mémoire via `enttest`, et un publisher factice capture les événements.

## Conséquences

- Les jointures entre contextes sont impossibles par construction, pas par discipline. Une fonction qui veut les commandes et le statut doit consommer les deux via événements ou interroger chaque service : la frontière fonctionne comme prévu.
- La suppression d'un utilisateur ne cascade plus dans le système par clés étrangères. users supprime ses propres lignes et publie un événement ; les autres services et le projector convergent à partir de celui-ci. Un consommateur qui manque l'événement conserve des lignes orphelines jusqu'à la réconciliation, compromis permanent de la cohérence éventuelle.
- Chaque service embarque sa propre copie du runtime ent généré, ce qui coûte en taille de binaire, pas en correction.
- Le compte et ses jetons restent cohérents transactionnellement dans un schéma ; le parcours de connexion ne nécessite aucune coordination distribuée.
- Cinq binaires au lieu d'un signifient cinq déploiements et cinq éléments à surveiller. `pkg/` partagé garde un câblage uniforme et limite le coût marginal de chaque service.

## Alternatives étudiées

- **Un service par table** (users, tokens, configs, commands, modules, transactions, six services). Isolation maximale sur le papier, mais les jetons sans utilisateurs ne forment pas une vraie frontière : chaque opération de jeton exigerait une vérification distante auprès de users et la connexion deviendrait distribuée. Rejeté comme symétrie pour elle-même.
- **Deux services** (identity avec users, tokens et transactions ; settings avec commands et modules). Tentant puisque les changements de niveau viennent des paiements, mais cela soude le parcours monétaire et celui de l'identité dans un déploiement ; l'ingestion des webhooks Tebex a une disponibilité et un audit différents des connexions. Rejeté pour garder le parcours monétaire dans son petit service indépendant.
- **Conserver le client ent partagé `internal/db`.** Moins de travail aujourd'hui, mais transforme l'isolation par schéma de [l'ADR 0005](/fr/adr/0005-adoption-of-mysql-heatwave/) en convention plutôt qu'en structure ; la première échéance aurait produit la jointure intertables que nous voulons rendre impossible. Rejeté.
