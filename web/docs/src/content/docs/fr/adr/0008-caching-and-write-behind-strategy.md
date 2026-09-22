---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0008 - Stratégie de cache et de write-behind"
description: "ADR : cache en processus avec protection contre les stampedes, regroupement write-behind et invalidation portée par les événements via NATS."
---

**Date :** 2026-06-09

## Statut

Accepté.

## Contexte

Les services de données résident sur l'instance HeatWave gratuite de [l'ADR 0005](/fr/adr/0005-adoption-of-mysql-heatwave/) : 8 Go de RAM partagés par tous les schémas. Deux modes d'accès la menacent.

En lecture, le hot path du bot est le chat. Une chaîne populaire peut demander des centaines de fois par minute « quels sont les modules de cet utilisateur ? », alors que la réponse change rarement. Sans cache, chaque message de chat devient une requête ; avec un cache naïf, chaque expiration provoque un stampede puisque toutes les requêtes manquées au même instant exécutent la même requête. Les entrées écrites ensemble expirent aussi ensemble, transformant une chaîne populaire en troupeau synchronisé.

En écriture, les réglages sont modifiés depuis un dashboard. Un streamer qui bascule cinq fois une option en deux secondes, ou itère sur le texte d'une commande, produirait une écriture autocommit par clic. Multiplié par les tenants, la base consacre sa capacité à persister des états obsolètes avant la fin de la transaction.

Une troisième force existe : les services évoluent horizontalement. Un cache en processus sur l'instance A ne sait rien d'une écriture passée par B ; l'invalidation doit donc circuler entre instances, et [l'ADR 0003](/fr/adr/0003-adoption-of-nats-as-communication-bridge/) fournit déjà le bus pour cela.

Nos exigences sont donc :

- lectures servies par la mémoire du processus, avec la garantie stricte que N absences concurrentes sur une clé coûtent une seule requête ;
- aucune expiration synchronisée entre les entrées ;
- écritures de la même clé dans une courte fenêtre regroupées en une écriture de ligne et une rafale déposée dans une seule transaction, pas N allers-retours ;
- le parcours monétaire (transactions Tebex, changements de niveau) et les jetons ne doivent jamais attendre dans un buffer write-behind ;
- l'invalidation doit atteindre chaque instance d'un service et le projector doit voir chaque changement exactement une fois ;
- les consommateurs doivent tolérer la redelivery, car le bus livre au moins une fois.

## Décision

**Cache en processus avec protection contre les stampedes (`pkg/cache`).** Une map TTL shardée (16 shards, un verrou par shard, hachage FNV-1a des clés sans allocation). Les absences passent par `singleflight` : autant d'absences concurrentes que nécessaire sur une clé sont regroupées dans un seul appel au loader et les attenteurs partagent le résultat. Les TTL comportent un jitter aléatoire allant jusqu'à 10 %, pour que les entrées n'expirent jamais toutes ensemble. Les erreurs ne sont jamais mises en cache. L'invalidation oublie aussi tout chargement en cours pour la clé, afin qu'un vol obsolète ne repeuple pas le cache après coup.

**Regroupement write-behind des réglages (`pkg/batch`).** Les écritures de modules et de commandes passent par un batcher de coalescence : les écritures d'une même clé pendant la fenêtre de flush (2 secondes ou 256 clés en attente, selon la première limite atteinte) sont réduites à la dernière valeur et toute la fenêtre arrive dans une seule transaction. Un flush échoué est réessayé lors de la fenêtre suivante sans écraser les écritures plus récentes, et chaque flush a une échéance stricte afin qu'une base qui accepte les connexions sans répondre ne bloque pas la goroutine du batcher pendant que les écritures s'accumulent. Cinq clics sur la même option coûtent une écriture de ligne.

Le service users fait passer par la même classe d'état de préférences renvoyable (activation, locale, curseur personnalisé, onboarded, code créateur), en fusionnant les champs en attente d'un utilisateur dans une mise à jour de ligne et en annonçant chaque utilisateur modifié une fois par fenêtre. Le compromis est inscrit dans le type : une valeur reste en mémoire au plus pendant l'intervalle de flush ; seul l'état qu'un utilisateur peut soumettre à nouveau y passe. Les transactions, changements de niveau, bannissements et jetons écrivent immédiatement ; les niveaux parce qu'ils concernent l'argent, les bannissements parce qu'ils appliquent une modération et ne doivent pas attendre.

**Invalidation portée par les événements sur le bus NATS natif (`pkg/bus`).** Les événements ne sont publiés qu'après le commit de base et portent l'état complet ; les consommateurs se mettent à jour à partir de l'événement seul et ne lisent jamais le schéma d'un autre service. Les contrats `Publisher`, `Subscriber` et `Message` de la flotte enveloppent `nats.go` sur le cluster JetStream de [l'ADR 0003](/fr/adr/0003-adoption-of-nats-as-communication-bridge/). L'adaptateur possède le rythme ack/nack, les bindings durables, les abonnements fan-out, les métadonnées de trace, le cycle de vie des connexions et les doublures de test. Deux formes d'abonnement sont intentionnelles :

- l'invalidation du cache est une diffusion : aucun queue group, afin que chaque instance supprime ses clés lorsqu'une instance écrit ;
- le projector consomme dans un queue group durable, afin que chaque événement soit plié dans la projection exactement une fois et que le consommateur conserve sa position après redémarrage.

**Les messages empoisonnés sont supprimés, pas nacked.** JetStream redélivre par défaut sans limite les messages non acquittés ; un payload mal formé qui serait nacké indéfiniment bloquerait le flux. Les consommateurs valident chaque payload ; ce qui échoue à la validation ou au décodage est journalisé puis acquitté et écarté.

## Conséquences

- Les lectures ont au pire la staleness du TTL du cache (5 minutes), et bien moins en pratique puisque les événements de changement invalident avant l'expiration. Un service qui nécessite read-your-own-write invalide son cache local de façon synchrone à l'écriture, ce que les dépôts font déjà.
- Les écritures de réglages peuvent être perdues dans une fenêtre au plus égale à l'intervalle de flush si le processus meurt. Nous l'acceptons pour les options et commandes que l'utilisateur peut soumettre de nouveau, et les transactions et jetons contournent explicitement le batcher. L'arrêt vide la fenêtre en attente.
- Chaque consommateur doit rester idempotent. Les payloads sont des remplacements d'état complet, donc la redelivery est naturellement inoffensive ; cette propriété doit être conservée lors de l'évolution des contrats.
- Les contrats d'événement de `internal/domain/event/data` font maintenant partie de la surface publique et demandent le même soin de versionnement que les sujets de l'ADR 0003.
- La mémoire de chaque instance croît avec l'ensemble de vues utilisées. Les caches stockent de petites structures de vue, jamais les jetons, et le sweeper en arrière-plan récupère les entrées expirées.

## Alternatives étudiées

- **Valkey read-through pour tout.** Un cache partagé simplifierait l'invalidation, mais ajoute un aller-retour réseau à chaque lecture du hot path, ce qui annule le cache en processus. Valkey a un autre rôle : la projection de réglages interservices (voir [ADR 0009](/fr/adr/0009-adoption-of-valkey-for-the-settings-projection/)).
- **Écrire chaque changement immédiatement.** Simple et durable, mais c'est précisément le martèlement par clic que la base ne doit pas absorber ; le dashboard serait limité par les allers-retours MySQL.
- **Debounce dans le frontend.** Utile pour le dashboard, mais ne protège aucun autre consommateur ; une autre API ou un futur ingress pourrait encore écrire à chaque événement. La frontière qui possède les données doit posséder la protection.
- **groupcache ou cache distribué en processus.** Résout les stampedes interinstances, mais apporte découverte des pairs et mesh HTTP, en contradiction avec le choix de substrat de [l'ADR 0003](/fr/adr/0003-adoption-of-nats-as-communication-bridge/). singleflight par instance et invalidation événementielle couvrent le même risque sans nouvelle infrastructure.
- **Watermill sur `nats.go`.** Fournit une mécanique générique de messages et de cycle de vie, mais ajoute une couche objet/marshalling en masquant les contrôles NATS spécifiques de batch, d'acquittement et de binding durable. L'adaptateur possédé est petit, testable directement et conserve le chemin de débit NATS 2.14 sans exposer les types de transport aux services.
