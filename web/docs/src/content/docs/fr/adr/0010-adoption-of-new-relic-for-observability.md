---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0010 - Adoption de New Relic pour l'observabilité"
description: "ADR : adoption de New Relic comme plateforme d'observabilité, avec transactions par message et traçage distribué sur le bus."
---

**Date :** 2026-06-09

## Statut

Accepté.

## Contexte

[L'ADR 0001](/fr/adr/0001-rewriting-to-microservices/) avait décrit honnêtement le coût : le débogage interservices et le besoin d'une pile d'observabilité deviennent le prix quotidien des microservices. Cette facture est arrivée. Un changement de réglage traverse un dépôt, un batcher write-behind, une transaction de base, un événement JetStream et un projector avant d'atterrir dans Valkey ; lorsque cette chaîne se comporte mal, les journaux de cinq services ne sont pas une réponse.

Les contraintes sont classiques. L'équipe est composée d'une personne, la flotte fonctionne sur une capacité gratuite ou peu coûteuse ([ADR 0004](/fr/adr/0004-adoption-of-oracle-cloud/)) et les services sont volontairement petits ; la pile d'observabilité ne peut donc pas devenir plus grosse que ce qu'elle observe. Auto-héberger un pipeline de métriques, traces et journaux (Prometheus, Tempo, Loki, Grafana et leurs stockages) est un vrai projet opérationnel, qui consommerait le même nœud ARM que les workloads.

Deux besoins propres au système ressortent :

- le travail est piloté par les messages, pas par les requêtes. L'unité de traçage est « un événement consommé » et la trace doit survivre au saut NATS du service publieur au consommateur ;
- la surveillance doit être facultative à l'exécution. Le développement local et les tests fonctionnent sans identifiants ni collecteur, et le code ne peut pas être rempli de tests nil pour y parvenir.

## Décision

Nous adoptons **New Relic** comme plateforme d'observabilité, sur son niveau gratuit (100 Go d'ingestion par mois, un utilisateur complet), câblé par un paquet partagé unique (`pkg/monitor`).

- **Un bootstrap par service.** `monitor.New` lit la clé de licence dans l'environnement. Sans clé, il renvoie une application nil et l'agent Go traite les receivers nil comme des no-ops ; développement et CI s'exécutent donc sans surveillance, sans branche dans le code appelant. La configuration au-delà du nom vient des variables `NEW_RELIC_*`, jamais du code.
- **Une transaction par message consommé.** Le consommateur du bus ouvre une transaction APM pour chaque message, signale l'erreur avant le nack en cas d'échec et expose la transaction via le contexte du message.
- **Traçage distribué sur le bus.** Les publishers injectent les en-têtes de trace dans les métadonnées du message natif et les consommateurs les acceptent, si bien qu'une trace suit une bascule de module depuis la transaction de flush, à travers JetStream, jusqu'à l'écriture Valkey du projector.
- **Bords instrumentés.** Le pool MySQL s'ouvre via le wrapper `nrmysql` ; chaque requête ent apparaît donc comme segment datastore de la transaction portée par le contexte. Le projector signale chaque commande Valkey comme segment datastore (sous le produit Redis, exact au niveau filaire). Les flushes batchés s'exécutent comme transactions d'arrière-plan, puisqu'ils sont détachés d'une requête. Les journaux sont envoyés une fois par le pipeline Fluent Bit du cluster ; les agents APM Go désactivent leur transmission de journaux pour éviter une double ingestion de stdout.

## Conséquences

- Traces, métriques, journaux et erreurs arrivent au même endroit, avec le saut d'événement cousu dans une trace. La question « pourquoi cette option n'est-elle pas dans Valkey ? » devient une recherche de trace plutôt que cinq fichiers de logs.
- Nous prenons une dépendance fournisseur, mais elle reste confinée : `pkg/monitor`, le bus et le fournisseur de base importent l'agent ; les dépôts ne portent qu'une référence de transaction pour le travail en arrière-plan. Passer à OpenTelemetry plus tard signifie remplacer ces points de couture, pas réécrire les services.
- Le niveau gratuit est un budget. 100 Go semblent beaucoup jusqu'à ce que la transmission des journaux rencontre une chaîne de chat active ; sampling et limites de journaux de l'agent doivent être surveillés, et le sampling zap de production plafonne déjà le pire.
- L'agent ajoute une goroutine d'arrière-plan et des cycles de collecte par service. C'est faible face à l'empreinte des services, mais non nul ; c'est pourquoi la surveillance reste désactivable par environnement.
- La télémétrie quitte l'infrastructure vers un tiers. Nous ne journalisons déjà pas le contenu utilisateur ([ADR 0003](/fr/adr/0003-adoption-of-nats-as-communication-bridge/)) et les jetons n'apparaissent jamais dans les événements ou logs ; ce qui part est donc constitué de métadonnées opérationnelles.

## Alternatives étudiées

- **OpenTelemetry avec une pile Grafana auto-hébergée.** Réponse correcte selon les standards et meilleure porte de sortie, mais elle transforme une personne en opérateur d'un pipeline (collector, Tempo, Loki, Prometheus, Grafana, stockage et mises à jour) sur le matériel nécessaire aux workloads. À réévaluer lorsque la flotte ou l'équipe grandira.
- **SDK OpenTelemetry vers un backend hébergé.** Garde l'instrumentation neutre vis-à-vis du fournisseur, mais le SDK Go est plus lourd à câbler à la main (providers, processors et exporters par signal), et les backends hébergés utiles ne sont pas plus gratuits que le niveau New Relic. Le compromis pragmatique va à l'agent intégré ; les points de couture gardent la sortie ouverte.
- **Prometheus et Grafana, métriques seulement.** Léger et autonome, mais les métriques répondent à « est-ce lent ? » et non à « où ? », alors que le saut interservices est précisément la partie qui nécessite des traces.
- **Ne rien faire pour l'instant.** Gratuit financièrement, mais coûteux la première nuit où quelque chose cesse silencieusement de se projeter. Le but de payer consciemment la taxe des microservices n'était pas de sauter cette ligne.
