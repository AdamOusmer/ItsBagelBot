---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "Notifications"
description: "Gère les notifications du tableau de bord et les annonces administratives."
---

Le service Notifications (`app/db/notifications/`) gère les alertes persistantes, les annonces système et les cloches de notification du tableau de bord pour les diffuseurs et les opérateurs.

## Architecture

- **Schéma** : base MySQL utilisant `ent` pour le schéma à l’exécution et les migrations.
- **Service RPC** : communique exclusivement par requête-réponse NATS. Il ne consomme ni ne publie aucun événement JetStream.
  - **RPC utilisateur** (`bagel.rpc.notifications.*`) : sert le tableau de bord avec `list`, `mark_read` et `mark_peeked`, suit l’état de lecture (aperçu ou lecture complète) et le nombre de notifications non lues.
  - **RPC admin** (`bagel.rpc.admin.notifications.*`) : `send`, `list` et `delete`. Les opérateurs peuvent envoyer des annonces à tout le système ou des notifications ciblées par identifiant ou par nom d’utilisateur (résolu par une recherche auprès de Users). Un `request_id` répété ne crée pas de seconde notification.

## Cycle de vie des données et TTL

Les notifications sont temporaires pour éviter une croissance illimitée de la base. Le service applique plusieurs durées de vie (TTL) lors du nettoyage :

- **TTL par défaut** (`NOTIF_DEFAULT_TTL`, 90 jours) : limite maximale globale.
- **TTL après lecture complète** (`NOTIF_FULL_READ_TTL`, 1 jour) : dès qu’un utilisateur lit entièrement une notification, elle cesse de lui être affichée après un jour.
- **TTL après aperçu** (`NOTIF_PEEK_TTL`, 7 jours) : ouvrir le menu de la cloche marque les notifications comme vues et les masque pour cet utilisateur après un délai plus long.

Ces seuils par utilisateur masquent une notification pour une seule personne. La ligne n’est supprimée que lorsque son expiration globale est atteinte.

### Nettoyage planifié

Les TTL ne sont pas évalués à la volée. Le service s’abonne à une opération interne de maintenance (`bagel.rpc.internal.notifications.cleanup`). Un CronJob Kubernetes (`notifications-cleanup`, chaque jour à 03 h 17) lance le binaire du service en mode `cleanup`, qui appelle ce RPC pour supprimer les enregistrements expirés globalement. Le processus de nettoyage n’ouvre pas MySQL lui-même.
