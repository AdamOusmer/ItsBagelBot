---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0011 - Transactions possède les gagnants des concours"
description: "ADR : l'historique des gagnants appartient à Transactions, avec la protection de facturation et les e-mails transactionnels."
---

**Date :** 2026-09-15

## Statut

Accepté et implémenté. Les avantages promotionnels non récurrents ont un interrupteur de fulfillment indépendant. Les intervalles d'abonnement et mutations du fournisseur restent bloqués jusqu'à vérification du comportement de facturation Tebex.

## Contexte

Un gagnant de concours doit recevoir Premium même si la protection du renouvellement Tebex ou l'envoi de l'e-mail est en attente. Enregistrer uniquement le statut Premium actuel d'un utilisateur ne conserve pas cette obligation et ne permet pas de savoir si le prix a été livré. Users possède l'identité et l'accès ; Transactions gère déjà Tebex et les e-mails transactionnels via Resend.

## Décision

Transactions possède les enregistrements de référence des gagnants et leur historique de fulfillment. Les e-mails sont envoyés par l'intégration Resend de Transactions. Users reste l'autorité pour l'identité et l'accès Premium effectif.

La conception conserve la campagne, le tirage et les gagnants dans Transactions afin qu'un tirage validé ait un registre unique. Transactions demande les changements d'accès via les contrats Users idempotents en utilisant l'identité du gain ; il n'écrit pas directement dans la base Users. Cela suit les frontières de services de [l'ADR 0007](/fr/adr/0007-adoption-of-per-schema-data-microservices/).

## Conséquences

La sélection du gagnant, la protection de facturation en attente et l'avancement de l'e-mail sont traçables depuis un seul enregistrement Transactions. Un échec d'e-mail ou un délai de facturation n'efface pas le gagnant. Le fulfillment doit récupérer après les pannes de service, car l'enregistrement du gagnant et l'accès Premium sont deux opérations de base distinctes.

## Alternatives étudiées

- **Users possède l'historique :** pratique avec les droits d'accès, mais sépare l'obligation du prix du workflow Transactions de facturation et d'e-mail.
- **Les deux services possèdent des enregistrements modifiables :** crée des historiques concurrents et une responsabilité de reprise ambiguë. Users conserve plutôt les droits liés à l'identité du gain Transactions.
