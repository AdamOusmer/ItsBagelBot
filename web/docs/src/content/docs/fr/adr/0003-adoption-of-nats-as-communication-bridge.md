---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0003 - Adoption de NATS comme passerelle de communication"
description: "Décision d’architecture : NATS (Core, JetStream et KV) comme passerelle entre services pour les événements, les RPC et la coordination des shards."
---

**Date :** 2026-05-23

## Statut

Accepté

## Contexte

À la suite de notre [décision de réécrire en microservices](/fr/adr/0001-rewriting-to-microservices/) et de notre [adoption de Go comme langage principal](/fr/adr/0002-adoption-of-go-as-primary-service-language/), les services doivent pouvoir communiquer sans se coupler par des appels HTTP directs. Un maillage synchrone placerait chaque service sur le chemin critique des autres ; les modes de défaillance de la v1 ont déjà montré la fragilité de cette approche lorsqu’un composant se bloque.

La valeur de ce système réside dans le temps réel. Un message de chat, un suivi, un abonnement ou une récompense de chaîne comptent dans les secondes qui suivent leur apparition et perdent presque toute leur valeur peu après. Nous ne construisons pas une plateforme d’analyse fondée sur un journal d’événements, et nous ne journalisons ni ne collectons de données utilisateur au-delà de ce qui est nécessaire pour réagir à un événement en cours. Cette position exclut les journaux à long terme : rejouer des mois d’historique n’est pas un objectif.

Au-delà du flux asynchrone d’événements, l’architecture présente deux autres besoins de communication faciles à sous-estimer.

Le premier est la requête-réponse entre services. Certains appels sont intrinsèquement synchrones : « cet utilisateur est-il modérateur de cette chaîne ? », « quel est le budget actuel de limitation pour ce jeton ? », « quelle est la configuration active de ce locataire ? ». Le réflexe habituel est gRPC, puissant mais accompagné de ses propres coûts : un transport supplémentaire, une chaîne de génération de code, une solution de découverte des services (DNS, sidecars ou mesh) et des répartiteurs de charge par service. Pour nos véritables schémas d’appels, cela représente beaucoup de composants à maintenir pour une équipe d’une personne.

Le second est la coordination des shards. Plusieurs éléments du système portent un état propre à une chaîne ou une session WebSocket : connexion de chat Twitch, session EventSub, limiteur par chaîne, file de travail par locataire. Chacun doit appartenir à exactement une instance à la fois, avec réattribution rapide en cas de panne. La réponse habituelle est un service de coordination (etcd, Consul, ZooKeeper), donc un autre système avec état, son quorum, ses instantanés et ses procédures d’exploitation.

Empiler gRPC, un service de coordination et un bus d’événements revient à exploiter, surveiller, sécuriser et restaurer trois infrastructures indépendantes. Les contraintes des ADR précédents restent valables : montée en charge à coût faible ou nul sur du matériel modeste, petite équipe et périmètre d’exploitation gérable par une personne. Nous voulons un socle unique, pas trois.

Nos exigences :

- Faible consommation mémoire et CPU, comparable à celle de nos services Go.
- Événements asynchrones avec livraison au moins une fois et acquittements explicites, pour ne pas perdre les changements importants lors d’une brève indisponibilité d’un consommateur.
- Persistance de courte durée pour ces événements : assez pour traverser un redémarrage ou un redéploiement, sans créer un journal à long terme.
- Primitive synchrone de requête-réponse sans transport distinct, génération de code ni découverte externe des services.
- Primitive de coordination (écriture atomique, TTL, surveillance) utilisable pour la propriété des shards et l’élection d’un responsable, sans service de quorum dédié.
- Client Go mature et bien maintenu.
- Exploitation simple : idéalement un seul binaire, sans JVM ni service externe de coordination.
- Routage par sujets pouvant évoluer avec notre domaine.

## Décision

Au vu de ces exigences, NATS JetStream devient le socle de communication de tout le système. Nous l’utilisons dans trois modes, servis par le même cluster.

**Core NATS pour la requête-réponse, à la place de gRPC.** Un service publie une requête sur un sujet tel que `chat.mod.check` ; toute instance abonnée peut répondre, et l’appelant reçoit une seule réponse ou une expiration du délai. Les groupes de file fournissent la répartition de charge : toutes les instances d’un service rejoignent le même groupe et NATS remet chaque requête à exactement l’une d’elles. Le sujet devient le contrat, les structures de requête et de réponse résident dans un paquet Go partagé, et aucun listener, port ou sidecar supplémentaire n’est nécessaire. gRPC reste envisageable si un chemin d’appel exige ensuite du streaming, des schémas stricts ou un débit extrême, mais ce n’est plus le choix par défaut.

**NATS JetStream pour le bus d’événements.**

- Empreinte : le serveur NATS est un binaire Go statique unique, exploité comme nos services. Au repos, sa consommation reste de l’ordre de quelques dizaines de mégaoctets sur le matériel visé.
- Garanties de livraison : JetStream propose une livraison au moins une fois avec acquittements explicites, relivraison à l’expiration du délai et gestion des messages en échec par limite du nombre de livraisons. C’est exactement le filet de sécurité voulu pendant la courte indisponibilité possible d’un consommateur.
- Persistance courte : les streams ont une rétention limitée dans le temps et en volume. Le bus agit comme un tampon bref, sans devenir un historique. Nous gagnons en résilience sans nous imposer un problème de stockage inutile.
- Routage : les sujets sont hiérarchiques (`twitch.chat.message`, `twitch.eventsub.follow`, etc.) et les jokers permettent aux consommateurs de choisir la granularité de leur abonnement.

**JetStream KV pour la propriété et la coordination des shards.** Les buckets KV reposent sur JetStream et fournissent la création atomique si absente, un TTL par clé et des observateurs. C’est la primitive de bail recherchée. Un service souhaitant posséder un shard crée `ingress.shard.<shard_id>` avec son identifiant d’instance et un TTL court, renouvelle le bail tant qu’il fonctionne et le laisse expirer en cas de panne. Une autre instance peut surveiller le bucket et reprendre le shard libéré dans la fenêtre du TTL. Le même mécanisme couvre l’élection d’un responsable pour les composants à instance unique. Tout provient du cluster déjà utilisé pour les événements et RPC : la haute disponibilité consiste à maintenir NATS en état, plutôt que NATS, etcd et une couche de découverte.

**Client Go.** `github.com/nats-io/nats.go` est maintenu par l’équipe du serveur ; Core, JetStream et KV sont des fonctionnalités de premier ordre de la même bibliothèque, et non des ajouts rapportés.

**Hub-Leaf.** Ce modèle permet de maintenir facilement la haute disponibilité du broker, tout en laissant les nœuds feuilles fonctionner de façon autonome si le hub tombe, afin d’assurer temporairement un service, même dégradé.

Précisons ce que nous n’adoptons pas : JetStream ne sert pas de journal historique ; nous ne prévoyons pas de rejouer des semaines d’événements et n’en faisons pas notre stockage de référence. Nous ne prétendons pas non plus que la requête-réponse NATS remplace entièrement gRPC dans tous les cas : c’est le choix par défaut, pas l’unique option. Si ces positions changent, cet ADR devra être réexaminé.

## Conséquences

- Une infrastructure critique se trouve désormais sur le chemin de toute interaction interservices. Sa disponibilité entre dans le SLO global, et une panne NATS a un périmètre plus large qu’avec trois systèmes distincts : elle arrête simultanément les événements, RPC et réattributions de shards. Nous acceptons ce compromis pour n’avoir qu’une stratégie de haute disponibilité à gérer.
- Les consommateurs doivent être idempotents. La livraison au moins une fois peut entraîner plusieurs traitements d’un même événement après relivraison ; les doubles traitements silencieux doivent être exclus dès la conception.
- Les fenêtres de rétention doivent rester courtes et être surveillées. Une rétention longue transformerait discrètement le bus en stockage de données, contrairement à notre position sur la collecte et la journalisation des données utilisateur.
- La hiérarchie des sujets est un contrat public, tant pour les événements que les RPC. Renommer un sujet après l’apparition de consommateurs ou d’appelants est incompatible ; le choix initial des noms mérite donc du soin. Les types de requête et de réponse partagés dans un paquet Go doivent être versionnés aussi soigneusement qu’un schéma protobuf.
- Sans stubs générés, nous perdons la sécurité des types échangés à la compilation qu’offre gRPC. Nous limitons ce risque avec des types partagés dans les paquets internes et de fines fonctions typées autour de l’appel NATS brut.
- Nous acceptons qu’un événement manqué au-delà de sa rétention soit perdu. C’est le bon compromis pour un système temps réel, mais chaque consommateur doit être conçu avec cette hypothèse, sans attendre un rattrapage illimité.
- La latence interservices passe par le bus au lieu de rester dans un processus : c’est le coût déjà accepté lors du passage aux microservices.

## Alternatives étudiées

### Pour le bus d’événements

- RabbitMQ : broker mature, mais son empreinte d’exploitation (VM Erlang, gestion de plugins) est plus lourde que ce qu’une personne devrait assumer, et son modèle de routage est plus rigide que les sujets à mesure que le domaine grandit.
- Kafka : référence des journaux d’événements, mais construit autour d’un historique durable et rejouable de chaque événement. C’est précisément ce que nous ne construisons pas. Le coût en ressources (JVM, KRaft ou ZooKeeper, réglages du stockage dédié) financerait une capacité et une complexité inutilisées.
- Redis Pub/Sub : léger, mais sans garantie de réception. Un message publié pendant la brève déconnexion d’un consommateur est perdu, sans acquittement ni relivraison. Même dans un système temps réel, perdre un callback EventSub ou une commande de chat parce qu’un pod redémarre est inacceptable. Redis Streams comblerait partiellement ce manque, mais reviendrait à reconstruire ce que JetStream fait déjà, avec des garanties plus faibles pour la relivraison et les acquittements.

### Pour la requête-réponse interservices

- gRPC : typé, rapide, bien outillé et choix évident si le RPC était le seul besoin. Son coût réside dans la pile qu’il entraîne (génération de code, découverte et répartition de charge par service) pour quelques appels internes sans besoin de streaming ni de schémas stricts. Choisir NATS ne ferme pas la porte à gRPC : un chemin qui en aurait besoin pourrait l’adopter sans changer le reste.
- HTTP/JSON direct entre services : simple, mais réintroduit exactement le maillage synchrone et le problème de découverte que nous cherchons à éviter, avec un listener supplémentaire par service.

### Pour la propriété et la coordination des shards

- etcd : éprouvé et conçu précisément pour cela, avec cohérence forte et sémantique de bail bien comprise. L’obstacle est opérationnel : un service avec état supplémentaire, son cluster Raft, ses instantanés et ses procédures de restauration, en plus de NATS.
- Verrous en base de données (verrous consultatifs Postgres, Redis Redlock) : réutilisent un stockage éventuellement déjà exploité, mais lient la propriété des shards à sa disponibilité et sa latence. Nous préférons coordonner sur le même socle que les événements et RPC concernés.
