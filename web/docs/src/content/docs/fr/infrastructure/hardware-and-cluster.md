---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Matériel et cluster
description: Nœuds ARM, orchestration K3s, limites de ressources et organisation du cluster.
---

Le cluster est volontairement petit, orienté ARM et autohébergé. Cette page décrit son organisation physique, le choix de la distribution Kubernetes et le modèle de ressources commun à toutes les charges de travail.

## Organisation physique

L'ensemble des nœuds utilise plusieurs architectures : **`node1`** est **ARM** (aarch64) et **`node2`** est **Intel** (x86_64). Toutes les charges sont réparties entre les deux nœuds pour assurer la haute disponibilité (HA) ; chaque nœud exécute l'image compilée nativement pour son architecture.

Le trafic des pods et des Services passe par le WireGuard natif de K3s, directement entre les adresses des nœuds. Tailscale est réservé à SSH et aux routes privées explicites : administration Kubernetes, accès privé à l'interface d'administration et base de données externe. NATS, Valkey, les RPC, les flux, la télémétrie et l'autoscaling restent sur le réseau natif des pods. Voir [Réseau →](/fr/infrastructure/networking/).

## Distribution Kubernetes : K3s

Nous utilisons [K3s](https://k3s.io/) plutôt que Kubernetes amont pour les raisons suivantes :

- **Installation avec un seul binaire.** Une commande suffit pour amorcer un nouveau nœud ARM ; reconstruire un nœud est une opération documentée d'environ 15 minutes.
- **Faible consommation de ressources.** Le processus serveur K3s héberge le plan de contrôle dans environ 512 Mo de mémoire résidente ; Kubernetes amont consommerait une part importante du nœud avant même le lancement des charges.
- **Stockage interne reposant sur SQLite.** Le plan de contrôle n'est pas hautement disponible ; un serveur unique convient à une charge destinée à un seul streamer. Si plusieurs plans de contrôle deviennent nécessaires, K3s documente la migration vers etcd embarqué.
- **Composants par défaut désactivés.** Nous désactivons Traefik, remplacé par Cloudflare Tunnel pour l'entrée dans le cluster, ainsi que ServiceLB, puisque Tailscale assure l'équilibrage fondé sur l'identité.

Options K3s importantes (`/etc/rancher/k3s/config.yaml`) :

```yaml
disable:
  - traefik
  - servicelb
flannel-backend: wireguard-native
flannel-external-ip: true
disable-network-policy: false
write-kubeconfig-mode: "0640"
node-label:
  - "node.bagelbot.io/role=worker"
```

Le kubeconfig local d'un nœud est réservé au groupe d'exploitation `bagelbot`. Celui distribué aux appareils des opérateurs cible l'adresse de l'interface Tailscale du serveur, pas son adresse LAN.

## Modèle de ressources

Chaque charge **doit** déclarer ses demandes et ses limites de ressources. Un `LimitRange` par namespace et une politique d'admission imposent cette règle en rejetant les spécifications de pods sans limites CPU ou mémoire.

### Valeurs par défaut

```yaml
# applied to any container that doesn't specify
resources:
  requests:
    cpu: "100m"
    memory: "64Mi"
  limits:
    cpu: "500m"
    memory: "256Mi"
```

Ces valeurs sont des **points de départ**, pas des objectifs : chaque service doit les adapter à sa charge mesurée.

### Pourquoi les limites sont strictes

Les binaires natifs rendent des limites serrées réalistes ([ADR-0002](/adr/0002-native-compilation/)). Un service Go traitant un flux Twitch normal reste aisément sous 50 Mo de mémoire résidente ; le service Kotlin compilé avec GraalVM reste sous 150 Mo. Des valeurs généreuses gaspilleraient une part importante d'un nœud de 8 Go.

Les arrêts OOM sont considérés comme un signal de charge plutôt que comme des pannes : si un service est arrêté pendant un train de la hype, on augmente sa limite au lieu d'ajouter de la marge partout.

### Catégorie de charge → namespace

| Namespace | Catégorie | Qualité de service attendue |
| --- | --- | --- |
| `bagelbot-system` | Services du cluster (cloudflared, tailscale-operator, cert-manager) | Guaranteed ; jamais évincés. |
| `bagelbot-data` | Services avec état (Postgres, Redis) | Guaranteed ; affectés aux nœuds avec SSD local. |
| `bagelbot-app` | Services du bot | Burstable ; éviction possible sous pression mémoire. |
| `bagelbot-build` | Constructions d'images et tâches ponctuelles | Best-effort ; facilement préemptées. |

Les classes de priorité des pods suivent ces attentes : `system-cluster-critical` pour le système, la classe personnalisée `bagelbot-data-critical` pour les données et la classe par défaut pour les applications.

## Sélection des nœuds et tolérances

- **Les charges de stockage**, notamment la persistance Postgres et Redis, portent un `nodeSelector` exigeant `node.bagelbot.io/storage=true`. Cette étiquette est attribuée aux nœuds équipés de SSD.
- **Le déploiement cloudflared** applique une règle d'anti-affinité pour répartir les réplicas entre les nœuds ; la perte d'un nœud ne doit pas couper l'accès public.
- **GPU et accélération :** aucun dans le parc actuel. Un futur nœud doté d'un Coral ou d'un équipement similaire recevrait son propre taint et des tolérances explicites pour les charges concernées.

## Ce qui ne s'exécute pas sur ce cluster

- **Les constructions CI.** GitHub Actions les exécute sur des runners ARM hébergés ; le cluster télécharge uniquement les images. Voir [CI/CD →](/infrastructure/cicd-pipeline/).
- **Les sauvegardes à long terme.** Les snapshots sont envoyés hors du cluster vers [à définir : destination de sauvegarde, par exemple Backblaze B2 avec restic]. Le calcul est considéré comme éphémère ; seuls les volumes de données doivent être préservés.
- **Le stockage des journaux et de l'observabilité.** Journaux et métriques sont envoyés vers [à définir : Grafana Cloud externe ou installation autohébergée hors cluster]. Garder l'observabilité à l'extérieur permet de diagnostiquer une panne du cluster.

## Marge de capacité

Le cluster vise **environ 50 % d'utilisation en régime stable**, afin qu'une pointe liée à un train de la hype ou la perte d'un nœud ne provoque pas immédiatement des évictions. Si l'utilisation soutenue dépasse environ 65 %, la réponse est d'ajouter un nœud plutôt que de réduire les limites.

## Pour aller plus loin

- **[Réseau →](/fr/infrastructure/networking/)** : détails du maillage Tailscale et de Cloudflare Tunnel.
- **[Pipeline CI/CD →](/infrastructure/cicd-pipeline/)** : parcours des images, du `git push` jusqu'aux nœuds.
- **[ADR-0001 →](/adr/0001-zero-trust-network/)** : décision relative au réseau Zero Trust.
- **[ADR-0002 →](/adr/0002-native-compilation/)** : influence des binaires natifs sur le modèle de ressources.
