---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Matériel et cluster
description: Nœud de plan de contrôle, nœuds de travail, orchestration K3s, classes de priorité et organisation du cluster.
---

Le cluster est volontairement petit et autohébergé. Cette page décrit son organisation physique, le choix de la distribution Kubernetes et le modèle d'ordonnancement commun à toutes les charges de travail.

## Organisation physique

Le cluster compte un nœud de plan de contrôle et un groupe de nœuds de travail.

- **`cp1`** est un nœud ARM à 2 cœurs qui exécute le serveur K3s. Il porte une taint : les charges de travail first-party ne s'y planifient pas. Les étiquettes de nœud utilisent la clé `role`, et `cp1` porte `role=cp`.
- Les **nœuds de travail** portent `role=node` et exécutent toutes les charges. NATS exécute un membre par nœud de travail (trois), et la plupart des services tournent en trois réplicas répartis sur ces nœuds. Les manifestes des services de production excluent les nœuds dont le `role` vaut `cp` ou `worker` (blocs `affinity` de `deploy/k8s/*.yaml`).

Les charges choisissent leurs nœuds par `role`, jamais par nom d'hôte (`deploy/k8s/workloads_test.go`, `TestNoWorkloadSelectsNodesByHostname`).

Les images sont compilées nativement pour Intel et ARM sur des runners GitHub (`.github/workflows/publish-images.yml`) : chaque nœud exécute l'image de sa propre architecture.

Le réseau des pods et des Services repose sur Cilium en mode de routage natif, avec remplacement de kube-proxy (`deploy/cilium/values.yaml`). Tailscale est réservé à l'accès privé : l'adresse du serveur d'API Kubernetes dans les valeurs Cilium est une adresse du tailnet, et le namespace `tailscale` porte les proxys privés. NATS, Valkey, les RPC et la télémétrie restent sur le réseau des pods. Voir [Réseau →](/fr/infrastructure/networking/).

## Distribution Kubernetes : K3s

Nous exécutons [K3s](https://k3s.io/) plutôt que Kubernetes standard, pour les raisons suivantes :

- **Installation en un seul binaire.** Amorcer un nœud tient en une commande.
- **Empreinte légère.** Le plan de contrôle est assez petit pour partager un nœud à 2 cœurs.
- **Datastore SQLite.** Le plan de contrôle est un serveur unique, pas un plan de contrôle HA. Le datastore est la base SQLite de K3s (kine), et `deploy/k8s/backup-k3s.yaml` en fait un instantané avec `sqlite3 .backup`, le vérifie, puis l'envoie hors du cluster.

## Modèle d'ordonnancement

Chaque Deployment first-party déclare ses requêtes et ses limites dans son manifeste, et chaque pod first-party porte l'une des quatre classes de priorité (`deploy/k8s/priorityclasses.yaml`) :

| Classe de priorité | Valeur | Utilisée par |
|---|---|---|
| `bagel-data-plane` | 900000 | Valkey et NATS |
| `bagel-service` | 800000 | twitch-ingress, sesame, outgress, projector, gossip et les services adossés à une base de données |
| `bagel-edge` | 700000 | Les consoles dashboard et admin |
| `bagel-operator` | 600000 | L'outillage du cluster, comme KEDA et clusterops |

L'ordre est voulu : les services dépendent du plan de données, qui est donc évincé en dernier. Une console brièvement non planifiable coûte à l'opérateur un rafraîchissement de page, alors qu'évincer un service d'exécution coupe du trafic de chat en direct.

Un arrêt par manque de mémoire (OOM) est traité comme un signal de charge, pas comme une panne : on relève la limite du service concerné au lieu d'ajouter de la marge partout.

### Namespaces

| Namespace | Contenu |
|---|---|
| `app` | twitch-ingress, sesame, outgress, les services Discord, gossip, les consoles |
| `db` | Les services adossés à une base (users, commands, modules, loyalty, transactions, notifications, discord-data), le projector et les jobs de sauvegarde |
| `cache` | Valkey avec Sentinel |
| `messaging` | Les hubs NATS et les nœuds leaf |
| `ops` | clusterops |
| `flux-system` | Les contrôleurs source et kustomize de Flux |
| `operators` | cloudflared et l'opérateur Doppler |
| `networking` | Traefik |
| `tailscale`, `keda`, `observability` | Les proxys Tailscale, KEDA et les agents New Relic |

## Répartition et placement

- **Répartition stricte.** Les services à plusieurs réplicas utilisent une répartition de topologie sur `kubernetes.io/hostname` avec `DoNotSchedule`, limitée par `pod-template-hash`, sans `minDomains`. Ces deux choix viennent d'incidents consignés dans `TestSpreadIsHardWithoutMinDomains`.
- **NATS et Valkey** tournent en StatefulSets avec anti-affinité obligatoire : la perte d'un nœud coûte un membre, jamais le quorum (`deploy/messaging/nats.yaml`, `deploy/cache/valkey.yaml`).
- **cloudflared** tourne en DaemonSet sur chaque nœud hors plan de contrôle (`deploy/operators/cloudflared.yaml`), si bien que la perte d'un nœud ne coupe pas l'entrée publique.
- **Les volumes épinglés** sont évités. Un volume local-path abandonne son pod quand le nœud meurt ; les membres avec état se reconstruisent donc par réplication.

## Ce qui ne s'exécute pas sur ce cluster

- **Les builds CI.** GitHub Actions compile les images et les pousse vers GHCR. Le cluster ne fait que les tirer. Voir [Démarrage](/fr/guides/getting-started/) pour le flux de travail local.
- **La base de données relationnelle.** MySQL HeatWave est un service géré ([ADR 0005](/fr/adr/0005-adoption-of-mysql-heatwave/)). Le job `backup-mysql` du namespace `db` en fait une copie toutes les six heures vers un stockage objet.
- **Le stockage d'observabilité.** Les journaux et métriques partent vers New Relic ([ADR 0010](/fr/adr/0010-adoption-of-new-relic-for-observability/)), ce qui garde une panne du cluster diagnostiquable.

## Marge de capacité

Les décisions d'extension suivent les seuils de la [feuille de route de performance](/fr/infrastructure/performance-roadmap/) : pas de nouveau serveur, de partitionnement ou de remplacement tant que l'enveloppe d'exploitation vérifiée ne dépasse pas 70 % pendant 15 minutes ou qu'un objectif de latence échoue après réglage.

## Pour aller plus loin

- **[Réseau →](/fr/infrastructure/networking/)** : détails sur Cilium, Tailscale et Cloudflare Tunnel.
- **[ADR 0004 →](/fr/adr/0004-adoption-of-oracle-cloud/)** : la décision d'hébergement.
- **[ADR 0002 →](/fr/adr/0002-adoption-of-go-as-primary-service-language/)** : pourquoi les services Go façonnent le modèle de ressources.
