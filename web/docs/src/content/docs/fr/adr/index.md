---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Décisions d’architecture
description: Journal des décisions d’architecture.
docType: reference
---

Les décisions conservent leur date et leur statut d’origine. Les descriptions résument les choix et leurs compromis à la lumière du code actuel. Les décisions remplacées restent dans le journal. Les anciennes décisions propres aux fournisseurs d’hébergement et d’observabilité ont été retirées lors de la réorganisation; elles restent accessibles dans l’historique Git.

| Décision | Date | Statut |
| --- | --- | --- |
| [0001 - Réécrire en services](/fr/adr/0001-rewrite-as-services/) | 2025-12-25 | Remplacée en partie par l’[ADR 0005](/fr/adr/0005-elixir-twitch-ingress/); la réécriture et les frontières entre services demeurent. |
| [0002 - Go pour les services](/fr/adr/0002-go-for-services/) | 2026-01-04 | Acceptée; précisée par l’[ADR 0005](/fr/adr/0005-elixir-twitch-ingress/). |
| [0003 - NATS pour la communication entre services](/fr/adr/0003-nats-service-communication/) | 2026-05-23 | Acceptée. |
| [0004 - MySQL pour les données durables](/fr/adr/0004-mysql-durable-state/) | 2026-05-23 | Acceptée. |
| [0005 - Elixir pour l’ingress Twitch](/fr/adr/0005-elixir-twitch-ingress/) | 2026-05-23 | Acceptée; remplace en partie l’[ADR 0001](/fr/adr/0001-rewrite-as-services/). |
| [0006 - Propriété des données par service](/fr/adr/0006-data-service-ownership/) | 2026-06-09 | Acceptée. |
| [0007 - Cache et écriture différée](/fr/adr/0007-cache-and-write-behind/) | 2026-06-09 | Acceptée. |
| [0008 - Projection de lecture partagée](/fr/adr/0008-shared-read-projection/) | 2026-06-09 | Acceptée. |
| [0009 - Transactions possède les prix de concours](/fr/adr/0009-transactions-own-giveaway-awards/) | 2026-09-15 | Acceptée et mise en œuvre; les chemins d’attribution ont des contrôles distincts. |
| [0010 - Kit possède l’inventaire public des variables](/fr/adr/0010-kit-variable-inventory/) | 2026-09-18 | Acceptée et mise en œuvre. |

Pour ajouter une décision, lancez `bun run adr:new "Titre court"` depuis `web/docs/`, puis ajoutez sa traduction dans `fr/adr/` avec le même numéro et le même nom de fichier. Réservez les ADR aux choix difficiles à inverser, non évidents et fondés sur un compromis réel.
