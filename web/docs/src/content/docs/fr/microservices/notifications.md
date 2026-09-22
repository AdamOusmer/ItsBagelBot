---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "Notifications"
description: "Gère les notifications du tableau de bord et les annonces administratives."
---

Le service Notifications (`app/db/notifications/`) gère les alertes persistantes, les annonces système et les cloches de notification du tableau de bord pour les diffuseurs et les opérateurs.

## Architecture

- **Schéma** : base MySQL utilisant `ent` pour le schéma à l’exécution et les migrations.
- **Service RPC** : communique exclusivement par requête-réponse NATS.
  - **RPC utilisateur** (`bagel.rpc.notifications.*`) : sert le tableau de bord de la console, suit l’état de lecture (aperçu ou lecture complète) et récupère le nombre de notifications non lues.
  - **RPC admin** (`bagel.rpc.admin.notifications.*`) : permet aux opérateurs d’envoyer des annonces à tout le système ou des notifications ciblées par identifiant utilisateur, ou par nom d’utilisateur via une recherche interservices sur `bagel.rpc.admin.user.get`.

## Cycle de vie des données et TTL

Les notifications sont temporaires pour éviter une croissance illimitée de la base. Le service applique plusieurs durées de vie (TTL) lors du nettoyage :

- **TTL par défaut** (`NOTIF_DEFAULT_TTL`, 90 jours) : limite maximale globale.
- **TTL après lecture complète** (`NOTIF_FULL_READ_TTL`, 1 jour) : dès qu’un utilisateur lit entièrement une notification, sa suppression rapide est planifiée.
- **TTL après aperçu** (`NOTIF_PEEK_TTL`, 7 jours) : ouvrir le menu de la cloche marque les notifications comme vues et déclenche une suppression à moyen terme.

### Nettoyage planifié

Les TTL ne sont pas évalués à la volée. Le service s’abonne à une opération interne de maintenance (`bagel.rpc.internal.notifications.cleanup`). Un CronJob Kubernetes appelle périodiquement ce RPC pour supprimer par lots les enregistrements expirés.
