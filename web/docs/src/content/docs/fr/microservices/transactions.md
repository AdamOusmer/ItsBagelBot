---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Transactions
description: Gère les webhooks et le paiement Tebex, et exécute les tirages et les attributions des concours.
---

Le service Transactions (`app/db/transactions/`) gère le flux de facturation et de concours. Tebex est le marchand officiel : ce service conserve donc un audit des webhooks vérifiés, et non un registre de paiements.

## Architecture

- **Webhooks** : Tebex envoie ses requêtes à `/tebex` ou `/webhooks/tebex` en HTTPS (`webhooks.itsbagelbot.com` y mène). Le service vérifie la signature, enregistre l’événement dans sa table d’audit et applique les événements de facturation acceptés de façon synchrone par le service [Users](/fr/microservices/users/) (`bagel.rpc.internal.billing.apply`). Si Users ne peut pas appliquer un événement, le webhook échoue pour que Tebex réessaie.
- **Paiement** : `bagel.rpc.transactions.basket_create` crée un panier Tebex. Il refuse d’en créer un lorsque le compte a déjà une couverture future ou une attribution en attente.
- **Concours** : les administrateurs prévisualisent et figent un ensemble de candidats à partir de l’instantané d’éligibilité de Users, puis tirent les gagnants. Un tirage enregistre les attributions choisies dans la même transaction que le travail qui les livre. Un worker prépare ensuite l’attribution Premium dans Users et la valide, protège la facturation Tebex récurrente si nécessaire, puis envoie les e-mails aux gagnants.
- **Propriété** : il possède son propre schéma MySQL et n’ouvre jamais la base de Users. Les avis dans l’application passent par [Notifications](/fr/microservices/notifications/), et les e-mails par l’adaptateur d’envoi Resend.

Une attribution Premium promotionnelle n’est pas un abonnement Tebex : Users garde les deux séparés.
