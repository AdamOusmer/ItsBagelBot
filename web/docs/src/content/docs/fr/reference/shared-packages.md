---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Paquets partagés
description: "Où se trouve le comportement commun de l’infrastructure."
docType: reference
---

## Portée

Le code des services utilise les paquets partagés pour accéder à l'infrastructure. Si une opération manque, ajoutez-la au paquet partagé au lieu d'émettre des commandes client brutes ou d'ajouter des scripts dans `app/`. Le fichier `AGENTS.md` du dépôt demande aussi d'utiliser les constructeurs existants afin de conserver leurs délais, métriques et contrôles d'identité.

## Carte des paquets

| Paquet | Point de départ pour |
| --- | --- |
| `pkg/bus` | Connexions NATS, publication, consommateurs et request-reply |
| `pkg/db` | Connexion, configuration, pool et aides de réessai de la base |
| `pkg/valkey` | Opérations du client Valkey et primitives atomiques |
| `pkg/ratelimit` | Coordination et stockage des limites de débit |
| `pkg/cache` | Caches locaux, chargement et invalidation |
| `pkg/batch` | Regroupement en mémoire et cycle de flush |
| `pkg/svcboot` | Démarrage commun et câblage de l'infrastructure |
| `pkg/health` et `pkg/monitor` | Santé et surveillance |

## Contrats d’échec

Ces paquets n'ont pas tous le même contrat d'échec. Par exemple, `pkg/batch` garde les mises à jour en mémoire et réessaie les flushs échoués; une écriture acceptée n'est pas automatiquement durable. Un délai NATS peut signifier qu'aucune réponse n'a été reçue, sans prouver que l'opération distante n'a pas eu lieu. Lisez la fonction, ses tests et le service appelant avant de choisir une stratégie de réessai ou de signaler une réussite.

Les tests sont à côté des implémentations. Le câblage propre à chaque service et les règles du domaine restent dans `app/` et `internal/`; utilisez la couche partagée pour les mécanismes et le service propriétaire pour les décisions métier.
