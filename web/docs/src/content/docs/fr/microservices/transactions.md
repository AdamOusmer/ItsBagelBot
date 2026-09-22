---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Transactions
description: Traite et enregistre les achats Tebex.
---

Le service Transactions (`app/db/transactions/`) gère les données financières et les achats du bot.

## Architecture

Il consomme les événements `data.transactions.recorded` du bus de messages. Sa responsabilité principale est de conserver le registre historique des achats Tebex. Il possède son propre schéma MySQL isolé et n'expose pas ces données au hot path du bot : il sert de registre immuable et de couche de validation pour les promotions de niveau Premium.
