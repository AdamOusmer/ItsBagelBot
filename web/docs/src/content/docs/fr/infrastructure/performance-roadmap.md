---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Feuille de route des performances et du nettoyage
description: Mesures de latence, qualification NATS R3, optimisation Valkey et capacité de 20 000 chaînes après la migration du plan de données natif.
---

## Objectif

Certifier le bot pour 20 000 chaînes sans masquer une régression de latence derrière un chiffre de débit. Le travail suit l'ordre des mesures : rendre le parcours complet explicable, qualifier NATS R3, ajuster les commandes Valkey réellement utilisées, puis réaliser un test prolongé représentatif de la production. Le partitionnement et l'ajout de matériel dépendent des besoins de capacité ; ce ne sont pas des prérequis.

## Mesures de référence à ne pas confondre

| Parcours | Résultat vérifié | Signification |
| --- | ---: | --- |
| Ingress JetStream R1 | 123 834 événements/s, 3 M acquittés, aucune erreur | Plafond de débit actuel sans déduplication et avec publication directe vers le leader |
| JetStream R3 | 11 996,6 événements/s pendant 60 s, p99 de 13,681 ms sur le parc | Stable au débit testé, sans qualification de latence pour l'ancien objectif JetStream de 2 ms |
| Calibration R3 `s2_fast` | Environ 75 000 événements/s, aucun retard final | Meilleure calibration conservée à forte charge ; ne qualifie ni 90 k pendant 30 minutes ni un p99 de 2 ms |
| Moteur Sesame et entrée, sortie mémoire | 20 074 commandes/s, p95 de 67 µs | Le moteur de commandes n'est pas le goulot d'étranglement actuel |
| Parc Sesame avec sorties confirmées | 22 971 commandes/s | Cas défavorable où chaque entrée émet une sortie ; la confirmation NATS domine |
| Lectures Valkey sur le nœud local | p99 de 0,247 à 0,790 ms | Déjà conformes au SLO de lecture de 2 ms sur chaque nœud |
| Écritures sur le primaire Valkey | p99 de 2,416 à 10,207 ms | La distance physique au primaire élu détermine la latence de queue |

R1 et R3 sont deux produits différents. Le résultat R1 de 123 k ne doit jamais être présenté comme une capacité R3 ; réussir un test de débit R3 n'annule jamais l'échec d'un critère de latence.

## Phase 1 : attribution de la latence du parcours complet

Instrumenter des noms d'étapes à cardinalité bornée sur tout le parcours interne d'une commande :

1. attente dans le dispatcher d'ingress ;
2. filtrage et routage ;
3. encodage JSON ;
4. publication NATS et acquittement JetStream ;
5. attente de livraison au consommateur ;
6. décodage Sesame et exécution du moteur ;
7. temps passé dans les dépendances Valkey et RPC ;
8. encodage de la sortie et publication confirmée ;
9. traitement d'outgress.

Propager les en-têtes de trace distribuée dans les RPC core NATS et les messages JetStream. Les noms agrégés ne peuvent contenir que des sujets configurés, des étapes, des voies, des opérations et des résultats issus d'ensembles finis. Les identifiants restent des attributs de traces ou de journaux échantillonnés, jamais des noms de métriques ou des facettes de tableaux de bord.

La phase 1 est validée lorsqu'une charge contrôlée fournit p50, p95, p99, minimum et maximum pour chaque étape, sans reconstruire le parcours à partir des journaux bruts.

## Phase 2 : qualification de NATS R3

L'architecture reste fixe pendant cette phase :

- les hubs portent JetStream et les feuilles ne servent que les RPC ;
- la déduplication du broker reste désactivée ;
- les producteurs se connectent au Service du hub local au nœud ;
- chaque membre du hub est un réplica équivalent et peut devenir leader ;
- aucun partitionnement ni sharding des flux ;
- les flux de production restent inchangés pendant la qualification isolée.

Tester le même flux isolé avec différentes combinaisons R1/R3, leader local ou distant, taille de message, mode de publication, taille de cohorte et nombre de connexions. Capturer les percentiles PubAck avec le CPU serveur, les octets en attente sur les routes, le retard des suiveurs, les reconnexions, les retransmissions et les changements de leader. Ne modifier qu'un réglage serveur à la fois et ne le conserver que si une nouvelle exécution confirme l'amélioration.

Le point de fonctionnement souhaité est de 90 000 événements/s pendant au moins 30 minutes, sans erreur ni retard final des suiveurs. Il ne peut être promu sans acceptation explicite de son objectif de latence sous charge. Le p99 historique de 2 ms reste le SLO JetStream à charge normale. Le p99 R3 proche de 14 ms, autrefois traité comme un plancher physique, avait été mesuré avec un quorum comprenant un nœud sur Wi-Fi domestique et deux machines moins puissantes. Ce matériel est retiré et la couche applicative est désormais uniforme : il faut remesurer avant de citer cette valeur comme une limite.

Si le débit de 90 k et la latence acceptée sont incompatibles, enregistrer le plafond stable inférieur. Ne pas assouplir un contrôle automatisé uniquement pour faire réussir le test.

## Phase 3 : commandes Valkey du chemin critique

Mesurer les commandes exactes de Sesame, du projector et d'outgress plutôt qu'un trafic GET/SET générique. Séparer les lectures sur le réplica du même nœud des écritures sur le primaire routées par Sentinel.

Priorités :

1. conserver des connexions TLS persistantes et mutualisées ;
2. supprimer de Valkey les réservations liées aux rejeux de transport, tout en conservant les cooldowns, timers, données de fidélité, état du direct et réputation ;
3. regrouper en pipeline les lectures à froid indépendantes ;
4. réunir les états liés à une limite de débit dans une seule opération atomique côté serveur ;
5. servir les états globaux fréquemment lus depuis un snapshot local versionné, réparé par Valkey et l'invalidation NATS ;
6. mesurer les pauses de snapshot et de basculement avant de modifier la persistance.

L'objectif p99 de 2 ms pour les lectures locales est déjà atteint. Les écritures sur le primaire sont évaluées selon un objectif tenant compte de la topologie : une latence d'écriture distante ne justifie pas d'autoriser des écritures sur les réplicas qui pourraient disparaître pendant une resynchronisation.

## Phase 4 : certification pour 20 000 chaînes

Après les phases 1 à 3, exécuter un test isolé de six heures avec des proportions réalistes de chaînes en direct, des débits de chat, des rafales de commandes, des raids, le redémarrage d'un membre NATS, un basculement Valkey et le remplacement d'un Sesame. Ingress reste à un pod par nœud NATS et n'ajoute pas de shards WebSocket en dessous de 75 % d'occupation.

La capacité se calcule à partir du débit mesuré, pas du nombre de chaînes :

```text
safe events/s = verified sustained ceiling * 0.70
safe channels = safe events/s / measured peak events/s per channel
```

Avec un plafond vérifié de 90 k, le budget opérationnel est de 63 k événements/s. Vingt mille chaînes tiennent tant que le pic simultané mesuré reste inférieur à 3,15 événements/s par chaîne.

## Critères d'expansion

Ne pas partitionner NATS, ajouter un serveur, remplacer Valkey ou migrer le CNI tant que l'enveloppe vérifiée n'est pas utilisée à plus de 70 % pendant 15 minutes, ou qu'un SLO de latence n'échoue pas après les réglages applicatifs et serveur. Toute proposition d'expansion doit préciser la télémétrie déclenchante, la marge attendue, le coût, le retour arrière et un test d'acceptation reproductible.

## Nettoyage associé aux phases

- transférer l'identifiant OAuth de l'opérateur Tailscale sous la gestion Doppler/Flux et le renouveler ;
- retirer la documentation obsolète sur Watermill, Linkerd, le plan de données Tailscale et la déduplication des rejeux dans Valkey ;
- faire nettoyer par chaque benchmark ses flux, consommateurs, pods, ACL temporaires et objets de résultat après réussite, échec ou interruption ;
- rendre les valeurs Helm et les demandes de ressources en CI pour détecter les clés de chart ignorées avant la production ;
- conserver des résultats datés et immuables, avec un profil de capacité courant faisant autorité.
