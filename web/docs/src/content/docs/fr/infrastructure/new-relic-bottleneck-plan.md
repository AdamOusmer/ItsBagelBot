---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Plan de diagnostic des goulots d'étranglement avec New Relic
description: Un plan privilégiant l'efficacité pour les traces et la télémétrie de capacité entre Twitch ingress, NATS, le projector, Valkey et les services propriétaires des bases.
---

## Objectif

Permettre à New Relic d'expliquer un événement lent ou une surcharge depuis Twitch ingress, à travers NATS et le projector, en distinguant le travail applicatif de l'attente liée au pool de connexions, à la limite de concurrence, au broker, à Valkey, au CPU et au réseau. Le plan réutilise volontairement les agents déjà en production et borne la cardinalité comme le volume de télémétrie ingérée.

Les services propriétaires de bases concernés sont `users`, `modules`, `commands` et `transactions`. Le projector n'interroge pas MySQL : il utilise Valkey.

## Instrumentation existante

Conserver l'instrumentation actuelle et s'appuyer dessus :

- `pkg/monitor` démarre une application APM Go par service, active les traces distribuées et corrèle les journaux zap.
- `pkg/bus` crée une transaction par livraison JetStream et propage les en-têtes de trace via le producteur NATS Go natif.
- `nrmysql` crée des segments de stockage pour les requêtes ent portant le contexte de transaction.
- Les opérations Valkey de projection créent des segments de stockage compatibles Redis.
- Ingress produit déjà des compteurs `Custom/Ingress/*` à faible volume et des événements de cycle de vie `IngressEvent`.
- Le bundle New Relic du cluster collecte déjà Kubernetes, les journaux, les données Prometheus, les métriques de l'exporteur NATS et celles de l'intégration Valkey.

Les informations manquantes concernent surtout l'attente et la saturation ; un collecteur généraliste supplémentaire n'est pas nécessaire.

## Règles d'efficacité

1. **Réutiliser APM pour mesurer le code et les agents existants pour l'infrastructure.** Ne pas ajouter OpenTelemetry, Pixie, un second collecteur Prometheus ou un serveur de métriques applicatives au premier déploiement.
2. **Émettre des snapshots d'état toutes les 30 secondes, pas des événements personnalisés par message.** Les détails par message appartiennent aux transactions et spans APM échantillonnés. Les snapshots de capacité appartiennent à un événement `ServiceCapacitySample`.
3. **Garder des noms issus d'ensembles finis.** Les noms de métriques, transactions et spans ne peuvent contenir que des opérations énumérées, sujets normalisés, étapes, voies et résultats. Les identifiants d'utilisateur, de message, de flux ou de pod, les erreurs brutes et les sujets NATS arbitraires ne doivent jamais entrer dans un nom ou une facette utilisés par les alertes.
4. **Préférer les deltas et ratios aux compteurs dupliqués.** Les métriques de livraison NATS et de ressources Kubernetes existent déjà ; les tableaux de bord doivent les interroger au lieu de les republier depuis le code applicatif.
5. **Instrumenter d'abord les frontières.** N'ajouter un span interne que si une frontière demeure opaque après exposition du temps global. Cela limite le travail sur le chemin critique d'ingress.
6. **Conditionner les intégrations optionnelles aux mesures.** N'ajouter la collecte côté serveur MySQL que si les mesures du pool et des requêtes côté client n'expliquent pas la latence observée.

## Modèle des signaux

### Attributs des traces

Utiliser ces attributs bornés dans les transactions Go et Elixir :

| Attribut | Valeurs |
| --- | --- |
| `messaging.system` | `nats` |
| `messaging.operation` | `publish`, `request`, `process`, `reply` |
| `messaging.destination` | Sujet configuré normalisé, jamais un sujet contenant un identifiant développé |
| `event.type` | Ensemble fini des types d'événements Twitch et métier |
| `event.lane` | `premium`, `standard`, `stream` |
| `result` | `ok`, `error`, `timeout`, `dropped`, `deferred`, `invalid` |
| `dependency` | `nats`, `valkey`, `mysql`, `twitch` |
| `cache.result` | `hit`, `miss`, `negative_hit`, `error` |
| `prewarm.branch` | `users`, `modules`, `commands` |

Les identifiants de message peuvent figurer dans les traces échantillonnées et les journaux corrélés pour le diagnostic, jamais dans les événements agrégés, métriques, tableaux de bord ou alertes. Ne pas envoyer les charges utiles Twitch, le texte du chat, les valeurs des paramètres SQL, les identifiants secrets ou les jetons.

### Événement de capacité

Ajouter aux wrappers de supervision Go et Elixir un petit utilitaire produisant toutes les 30 secondes un événement de cette forme :

```text
ServiceCapacitySample
  service                 projector | ingress | users | modules | commands | transactions
  component               dispatcher | db_pool | db_gate
  capacity                configured maximum
  inUse                   current active work
  queued                  current waiting work
  waitCountDelta           waits since the previous sample
  waitMsDelta              cumulative wait milliseconds since the previous sample
  timeoutCountDelta        timeouts since the previous sample
  droppedCountDelta        drops since the previous sample
```

Ne renseigner que les champs pertinents pour le composant. Un intervalle de 30 secondes représente deux événements par composant et par pod chaque minute, un volume faible par rapport à la télémétrie par message, et fournit directement des signaux d'utilisation et de taux d'attente exploitables par les alertes.

## Phase 1 : rétablir la continuité des traces

Cette phase offre la plus forte valeur diagnostique et doit être livrée en premier.

### Requête/réponse core NATS

Modifier `pkg/bus/rpc.go` :

1. Dans `RequestJSON`, créer un `nats.Msg`, appeler `InsertDistributedTraceHeaders` depuis la transaction de `ctx`, copier ces valeurs dans `Msg.Header` et utiliser `RequestMsgWithContext`.
2. Dans `QueueSubscribeJSON`, copier `msg.Header` dans un `http.Header` et appeler `AcceptDistributedTraceHeaders(newrelic.TransportQueue, headers)` avant de décoder la requête.
3. Ajouter un segment client couvrant l'attente de requête/réponse. Normaliser son nom sur le sujet RPC configuré.
4. Ajouter des tests prouvant l'injection et l'acceptation des en-têtes W3C/New Relic sans compte New Relic actif.

Cela comble les ruptures de traces RPC entre ingress/projector et les services de données, ainsi qu'entre le projector et outgress.

### Transaction de notification d'ingress

Modifier `Ingress.Dispatcher`, `Ingress.Pipeline`, `Ingress.Nats` et `Ingress.BroadcasterStatus` :

1. Enregistrer `enqueued_at` avec l'élément du dispatcher.
2. Démarrer une transaction `NewRelic.other_transaction("Ingress", "Notification")` dans la tâche supervisée. Enregistrer l'attente du dispatcher comme attribut et métrique personnalisée ; démarrer dans la tâche évite de contaminer le processus de shard à longue durée de vie.
3. N'ajouter initialement que quatre spans : `route`, `broadcaster_status.request` lors d'un défaut de cache, `encode` et `nats.publish`.
4. Passer `NewRelic.distributed_trace_headers(:other)` dans l'option `headers:` existante de Gnat pour les publications et les requêtes.
5. Classer le résultat et enregistrer le type d'événement fini et la voie. Signaler les erreurs sans considérer le filtrage normal des messages de chat comme une erreur.

Gnat 1.15.1 accepte déjà les en-têtes pour `pub` et `request`, et l'agent Elixir installé expose les API de transaction et d'en-têtes de traces distribuées : aucune dépendance ne doit changer.

### Préchauffage du projector

Les trois goroutines de préchauffage utilisent actuellement `context.Background()` : leur travail RPC et Valkey est donc invisible dans la trace de l'événement de flux. Ne pas garder ouverte la transaction du consommateur d'origine jusqu'à cinq secondes uniquement pour rattacher ce travail non garanti.

À la place :

1. Extraire les en-têtes distribués de la transaction de l'événement de flux avant de lancer les goroutines.
2. Démarrer une transaction d'arrière-plan pour chaque branche `users`, `modules` et `commands`, accepter les en-têtes du parent et placer la transaction enfant dans le contexte de la branche.
3. Terminer chaque transaction lorsque son RPC et son écriture Valkey sont achevés.
4. Ajouter `prewarm.branch` et `result` ; garder l'identifiant utilisateur uniquement dans le contexte de trace ou de journal.

Cela préserve le lancement sans attente tout en produisant trois transactions enfants liées causalement.

### Acceptation de la phase 1

- Une notification ingress synthétique produit une transaction ingress suivie de la bonne transaction de consommateur Go.
- Un défaut du cache de diffuseur inclut le répondant RPC users et son segment MySQL dans la même trace distribuée.
- Une trace de passage en direct relie le consommateur projector aux trois branches de préchauffage.
- Les tests locaux existants fonctionnent toujours sans clé de licence et sans appel réseau.

## Phase 2 : exposer la saturation applicative

### Dispatcher et cache d'ingress

Étendre `Ingress.Metrics` avec un échantillonneur de 30 secondes et des compteurs monotones peu coûteux. Rapporter :

- `running`, la longueur de file, les limites configurées, l'attente, les abandons et les fins anormales de tâches du dispatcher ;
- succès, défauts et succès négatifs du cache de diffuseur, erreurs de chargement et nombre d'entrées courant ;
- résultats et délais dépassés des publications et requêtes NATS ;
- reconnexions des shards, expirations de délai zombie et nombres de notifications sous forme de compteurs existants, pas d'événements personnalisés par notification.

Lire la longueur de file dans le GenServer du dispatcher et la taille du cache via `:ets.info(table, :size)`. Ne parcourir aucune de ces structures pour produire la télémétrie.

### Pool et limite de concurrence de base de données

Ajouter `monitor.ObserveDBPool(ctx, app, service, driver.DB(), 30*time.Second)` aux points d'entrée des quatre services de données. L'observateur échantillonne les valeurs standard de `sql.DBStats` :

- `OpenConnections`, `InUse`, `Idle` et `MaxOpenConnections` ;
- deltas de `WaitCount`, `WaitDuration`, `MaxIdleClosed`, `MaxIdleTimeClosed` et `MaxLifetimeClosed`.

Étendre `pkg/db/gate.go` avec des compteurs atomiques pour les détenteurs courants, les attentes courantes et cumulées, leur durée et les délais d'acquisition dépassés. Les atomiques évitent un second verrou à chaque requête. Le même observateur de 30 secondes rapporte le pool et la limite comme deux composants `ServiceCapacitySample`.

Conserver `nrmysql` comme source de durée d'exécution des requêtes. N'ajouter des segments au niveau repository que pour les opérations dont les multiples appels au stockage restent ambigus dans les traces ; ne pas envelopper chaque méthode ent générée.

### Débit du projector

Ne pas dupliquer les métriques de consommateur de l'exporteur NATS. N'ajouter que les informations applicatives inconnues du broker :

- résultat et durée du handler par sujet d'événement normalisé ;
- abandons de charges utiles invalides ;
- résultat des écritures et invalidations Valkey ;
- résultat et durée des branches de préchauffage.

Utiliser les séries Prometheus NATS existantes pour les graphiques d'attente durable, d'acquittements en attente, de relivraison et de débit de livraison.

### Acceptation de la phase 2

- Réduire le pool d'un service de quatre connexions à une fait augmenter séparément l'utilisation et l'attente du pool/de la limite, sans les confondre avec l'exécution MySQL.
- Saturer le dispatcher d'ingress rend visibles la profondeur et l'attente de la file avant l'augmentation des abandons.
- Suspendre la consommation du projector augmente le retard durable du broker sans signaler à tort des écritures Valkey lentes.
- L'échantillonneur de capacité s'arrête rapidement à l'annulation du contexte de son service, sans fuite de goroutine.

## Phase 3 : vues et alertes New Relic versionnées

Créer un petit module Terraform `deploy/newrelic/` seulement après avoir observé les nouveaux attributs en production. Garder l'identifiant de compte et les clés utilisateur/API hors de git. Gérer quatre tableaux de bord :

1. **Parcours d'un événement :** acceptation/publication ingress, débit et retard NATS, débit et erreurs du projector, durée Valkey.
2. **Pression d'ingress :** utilisation/attente du dispatcher, abandons, taux de succès du cache, latence RPC, reconnexions des shards, CPU et mémoire.
3. **Pression du projector :** retard durable/relivraisons, percentiles des handlers, segments Valkey, branches de préchauffage, CPU et mémoire.
4. **Pression des bases :** utilisation et attente des pools/limites par service, à côté de la durée MySQL et des ressources du service.

Commencer par les alertes indiquant une perte ou une saturation forte :

| Signal | Avertissement | Critique |
| --- | --- | --- |
| Utilisation du dispatcher ingress | >70 % pendant 10 min | >90 % pendant 5 min |
| Abandons du dispatcher ou de publication NATS | Sans objet | >0 pendant 2 min |
| Utilisation du pool DB avec attentes | >80 % pendant 10 min | >95 % pendant 5 min |
| Délais d'acquisition dépassés de la limite DB | Sans objet | >0 pendant 5 min |
| Retard durable du projector | Croissance positive pendant 10 min | L'élément le plus ancien dépasse l'objectif de fraîcheur |
| Taux d'erreurs RPC/préchauffage | >2 % pendant 10 min | >5 % pendant 5 min |
| Télémétrie requise | Sans objet | Absence de signal pendant 5 min |

Après deux semaines de référence, remplacer les seuils de latence estimés par des conditions d'anomalie ou des seuils dérivés des p95/p99 observés. Ne pas déclencher d'astreinte pour des défauts de cache, le filtrage normal du chat ou des échecs isolés de préchauffage non garanti.

## Phase 4 : télémétrie serveur MySQL conditionnée aux mesures

La télémétrie cliente doit indiquer si le temps est passé avant une connexion, à attendre la limite du processus ou à exécuter une requête. N'ajouter la télémétrie serveur que si l'exécution des requêtes reste le segment dominant inexpliqué.

Ordre préféré :

1. Activer l'intégration OCI vers New Relic pour les métriques du service HeatWave géré si l'intégration OCI existante expose les signaux de connexion, CPU, stockage et HeatWave nécessaires sans charge dans le cluster.
2. Si les données de verrous, de buffer pool, de threads ou de requêtes lentes restent nécessaires, déployer un seul runner distant `nri-mysql` avec un compte de supervision en lecture seule et TLS. Ne pas configurer la même cible distante dans chaque DaemonSet d'infrastructure kubelet, ce qui dupliquerait quatre fois les données.
3. N'activer que les ensembles de métriques étendues nécessaires à l'enquête en cours, puis mesurer l'ingestion avant de les conserver.

## Ordre de déploiement

Utiliser quatre changements pouvant être annulés indépendamment :

1. **Propagation des traces :** RPC Go partagés, transactions/en-têtes ingress et liens de préchauffage du projector.
2. **Échantillons de capacité :** dispatcher/cache ingress et observateurs de pools/limites DB Go.
3. **New Relic versionné :** tableaux de bord, alertes de perte/saturation et une charge regroupant les six services.
4. **Intégration de base optionnelle :** seulement lorsque les mesures de la phase 2 satisfont les critères précédents.

Déployer d'abord sur un pod ou un service de données lorsque l'organisation du déploiement le permet. Comparer pendant 24 heures avant de généraliser :

- CPU et mémoire des services ;
- durées p50/p95 des transactions ;
- volume ingéré par New Relic, par type de données ;
- erreurs internes des agents et spans/événements perdus ;
- continuité des traces à travers NATS.

Poursuivre si la régression CPU est inférieure à 2 %, la croissance mémoire à 10 Mio par pod, la régression p95 du chemin critique à 2 %, et si l'ingestion mensuelle projetée reste dans le budget du compte. Sinon, augmenter les intervalles d'échantillonnage, retirer les spans internes les moins utiles ou ne conserver que les traces aux frontières.

## Critères de fin

L'intégration est terminée lorsqu'un test contrôlé peut créer indépendamment chaque goulot suivant et que les vues New Relic identifient la bonne frontière sans consulter les journaux bruts :

- saturation de la file du dispatcher ingress ;
- retard du projector dans NATS/JetStream ;
- latence Valkey du projector ;
- attente à la limite de concurrence du processus DB ;
- attente dans le pool de connexions DB ;
- latence d'exécution MySQL ;
- limitation CPU ou pression mémoire du service ;
- délai de dépendance dépassé ou perte de télémétrie.

La fin exige aussi des noms et facettes bornés, aucune capture de données sensibles, une responsabilité documentée pour les NRQL/tableaux de bord, des tests sans licence réussis et un surcoût mesuré respectant les limites de déploiement.
