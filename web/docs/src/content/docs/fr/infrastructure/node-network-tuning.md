---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Réglages réseau des workers
description: "Configuration à huit files, affinité IRQ et XPS, offloads, persistance et limites mesurées sur node1–3."
docType: reference
---

Les workers utilisent huit paires de files RX/TX combinées sur `eth0` et `eth1`. NetworkManager conserve les réglages des interfaces; un service systemd oneshot applique l’affinité IRQ, XPS et RPS au démarrage. Aucun hook de dispatcher, watchdog ou contrôle de réparation périodique n’est installé.

Cette page décrit la configuration appliquée le **2026-11-09** à `node1`, `node2` et `node3` : des machines virtuelles AlmaLinux 10.2 avec douze vCPU en ligne et des interfaces virtio. `cp1` était hors périmètre. Les réglages et mesures décrivent cette flotte à cette date, sans constituer un profil universel d’optimisation.

## Modifications et raisons

| Élément | État précédent | État final et raison |
| --- | --- | --- |
| Persistance des files eth0 | Huit files actives, mais le script de démarrage supposait une seule file | Enregistrer huit files dans le profil NetworkManager et remplacer la politique IRQ obsolète |
| Interruptions eth0 | Plusieurs files recevaient sur un CPU et terminaient les émissions sur un autre | Affecter chaque paire RX/TX au même vCPU, comme sur eth1 |
| Distribution en réception | L’ancienne politique eth0 aurait activé RPS sur tous les CPU | Mettre les masques RPS et les nombres d’entrées RFS à zéro pour éviter un transfert logiciel supplémentaire |
| Sélection en émission | eth0 utilisait les masques CPU initiaux du pilote | Appliquer les masques XPS d’eth1 aux deux interfaces, avec correspondance pour les vCPU 2–9 |
| Longueur de file TX eth0 | `1000` | `5000`, comme eth1, pour absorber les microbursts |
| Transfert UDP GRO eth0 | Désactivé | Activé, avec GRO list désactivé |
| Maximum IPv4 GRO eth0 | `65536` octets après recréation | `196608` octets, réconciliés par la politique BIG TCP existante de Cilium |
| Persistance | Scripts qui se chevauchaient, dont l’hypothèse d’une file unique | Profils enregistrés et un service IRQ au démarrage; désactiver les anciens services adaptatif et propre à eth1 |
| Essai de limite FQ | Feuilles actives à `100` paquets par flux | Conserver `100`; l’essai à `1000` n’a pas démontré de bénéfice |

L’objectif était d’utiliser les huit files en gardant leur travail près du CPU affecté. Aligner IRQ et XPS peut réduire les transferts inutiles entre CPU; cela ne fixe pas les threads applicatifs et ne prouve pas la disparition des invalidations de cache. Le [guide de distribution réseau Linux](https://www.kernel.org/doc/html/latest/networking/scaling.html) explique les rôles de RSS, RPS, RFS et XPS.

## Responsabilités et persistance

| Responsable | Configuration | Moment d’application |
| --- | --- | --- |
| NetworkManager | Files, anneaux, offloads de checksum et segmentation, UDP GRO et longueur TX | Activation des profils de connexion existants |
| `fleet-irq-affinity.service` | Cibles CPU des IRQ, masques XPS, désactivation de RPS/RFS | Une fois au démarrage, après NetworkManager et avant k3s |
| Politique sysctl existante | Buffers de sockets, backlog, BBR, FQ par défaut et autres réglages | Démarrage de l’hôte; voir l’instantané daté ci-dessous |
| Cilium | BIG TCP, pacing FQ, routage natif et chemin BPF | Initialisation de l’agent et réconciliation normale de Cilium |

[`configure-network-profiles.sh`](https://github.com/AdamOusmer/ItsBagelBot/blob/main/deploy/k8s/network-tune/configure-network-profiles.sh) sélectionne l’UUID de la connexion active existante de chaque interface et enregistre ses paramètres avec `nmcli connection modify`. Il ne coupe pas le lien et ne réactive pas la connexion. Les valeurs ont été appliquées à chaud, puis les profils enregistrés ont été relus depuis le disque et vérifiés.

[`fleet-irq-affinity.service`](https://github.com/AdamOusmer/ItsBagelBot/blob/main/deploy/k8s/network-tune/fleet-irq-affinity.service) utilise `Type=oneshot` et `RemainAfterExit=yes`. Il exécute `/usr/local/sbin/fleet-irq-affinity.sh` après `network-online.target` et `NetworkManager-wait-online.service`, avant `k3s.service` et `k3s-agent.service`. Il est activé sous `multi-user.target`.

`irqbalance.service` reste masqué. `bagelbot-network-performance.service` et `fleet-eth1-tuning.service` sont désactivés. Le hook NetworkManager proposé a été retiré. La vérification manuelle est distincte de la configuration et n’a aucune unité planifiée.

## Réglages des interfaces

Les deux interfaces annoncent au maximum huit paires combinées, 1024 descripteurs RX et 256 descripteurs TX. Ces maxima étaient déjà actifs; les profils les rendent explicites et persistants.

| Réglage | Propriété NetworkManager | eth0 | eth1 |
| --- | --- | --- | --- |
| Files combinées | `ethtool.channels-combined` | `8` | `8` |
| Descripteurs RX | `ethtool.ring-rx` | `1024` | `1024` |
| Descripteurs TX | `ethtool.ring-tx` | `256` | `256` |
| Checksum RX | `ethtool.feature-rx` | `on` | `on` |
| Checksum TX | `ethtool.feature-tx` | `on` | `on` |
| Scatter/gather | `ethtool.feature-sg` | `on` | `on` |
| Segmentation TCP | `ethtool.feature-tso` | `on` | `on` |
| Segmentation générique | `ethtool.feature-gso` | `on` | `on` |
| Réception générique | `ethtool.feature-gro` | `on` | `on` |
| Transfert UDP GRO | `ethtool.feature-rx-udp-gro-forwarding` | `on` | `on` |
| GRO list | `ethtool.feature-rx-gro-list` | `off` | `off` |
| Longueur de file TX | `link.tx-queue-length` | `5000` | `5000` |
| MTU du lien, préservé | Configuration existante | `1500` | `9000` |

`eth0` porte le chemin public; `eth1` porte le trafic privé des workers et le routage direct de Cilium. Le MTU public n’a pas été augmenté à une taille jumbo. GRO matériel était aussi activé sur les deux interfaces, mais l’installateur ne définit pas explicitement cette option.

Il s’agit de **files virtio de la machine virtuelle**. L’hyperviseur n’expose pas de table d’indirection RSS configurable : `ethtool -x` indique que l’opération est indisponible pour la table et la clé. Huit files actives ne donnent pas le contrôle de la table RSS physique ni des threads vhost de l’hôte.

## IRQ, XPS et distribution en réception

La même répartition s’applique aux deux interfaces sur les trois workers. Les numéros désignent les vCPU de la machine virtuelle.

| File | CPU cible IRQ RX | CPU cible fin TX | Masque hexadécimal `xps_cpus` | CPU utilisant cette file XPS |
| --- | --- | --- | --- | --- |
| 0 | 2 | 2 | `005` | 0, 2 |
| 1 | 3 | 3 | `008` | 3 |
| 2 | 4 | 4 | `010` | 4 |
| 3 | 5 | 5 | `020` | 5 |
| 4 | 6 | 6 | `040` | 6 |
| 5 | 7 | 7 | `080` | 7 |
| 6 | 8 | 8 | `500` | 8, 10 |
| 7 | 9 | 9 | `a00` | 9, 11 |

Chaque file RX a `rps_cpus=0` et `rps_flow_cnt=0`. XPS fondé sur la réception, `xps_rxqs`, était à zéro; la configuration utilise XPS fondé sur les CPU. Le CPU 1 n’a aucune entrée explicite dans les masques XPS et utilise la sélection d’émission de repli du noyau. Les CPU 0, 10 et 11 partagent les files indiquées avec leur CPU de fin d’émission. Huit paires pour douze vCPU ne permettent pas une paire exclusive par vCPU.

[`fleet-irq-affinity.sh`](https://github.com/AdamOusmer/ItsBagelBot/blob/main/deploy/k8s/network-tune/fleet-irq-affinity.sh) identifie les périphériques via sysfs et recherche les noms tels que `virtioN-input.3` et `virtioN-output.3`. Les numéros d’IRQ ne sont pas codés en dur. Il exige au moins douze CPU en ligne et exactement huit files RX et TX par interface. Les IRQ du balloon ciblent le CPU 0, celles du stockage le CPU 11. Une IRQ de stockage gérée par le noyau est acceptée seulement si son CPU effectif est déjà 11.

Il s’agit d’affinité des interruptions, sans isolation des CPU. Les threads applicatifs peuvent utiliser tout vCPU autorisé par l’ordonnanceur. Aucun `cpuset` applicatif, allocation Kubernetes CPU Manager, épinglage physique ou affinité vhost n’a été ajouté. La machine virtuelle annonce un domaine L3 partagé; le placement réel des caches et des nœuds NUMA de l’hôte reste non vérifié.

## Routage Cilium, pacing et BIG TCP

La politique existante a été préservée dans [`deploy/cilium/values.yaml`](https://github.com/AdamOusmer/ItsBagelBot/blob/main/deploy/cilium/values.yaml) et [`node-configs.yaml`](https://github.com/AdamOusmer/ItsBagelBot/blob/main/deploy/cilium/node-configs.yaml) :

| Réglage | Valeur |
| --- | --- |
| `bandwidthManager.enabled` / `bandwidthManager.bbr` | `true` / `true` |
| `bpf.hostRouting` / `bpf.masquerade` | `true` / `true` |
| `enableIPv4BIGTCP` / `pmtuDiscovery.enabled` | `true` / `true` |
| `routingMode` / `autoDirectNodeRoutes` | `native` / `true` |
| `ipv4NativeRoutingCIDR` et pool des pods | `10.42.0.0/16` |
| `kubeProxyReplacement` / `loadBalancer.serviceTopology` | `true` / `true` |
| Interfaces workers / routage direct | `eth0 eth1 tailscale0` / `eth1` |
| MTU Cilium des workers | `9000`; le MTU du lien public eth0 reste `1500` |
| Événements BPF trace / verdict de politique / rejet | Désactivés / désactivés / activés |
| Hubble / proxy L7 / Envoy | Désactivés / désactivé / désactivé |
| Mise à jour progressive Cilium | `maxUnavailable: 1` |

Chaque interface a une racine `mq` et huit feuilles `fq`. Les paramètres observés étaient `limit=10000`, `flow_limit=100`, `buckets=32768`, `horizon=2s` et `horizon_drop`. Le contrôle de congestion TCP de l’hôte reste `bbr`; le busy polling reste désactivé.

Après le redémarrage progressif des agents Cilium des workers, les deux interfaces avaient `gro_ipv4_max_size=196608` et `gso_ipv4_max_size=65536`. La carte annonce `tso_max_size=65536`. La limite GRO supérieure regroupe le travail de réception dans la pile; elle n’agrandit pas les paquets publics. Voir les [conseils Cilium sur BIG TCP](https://docs.cilium.io/en/stable/operations/performance/tuning/).

## Politique réseau existante des hôtes

Le déploiement des files n’a **pas** remplacé la politique sysctl globale. Voici le contenu observé dans `/etc/sysctl.d/99-fleet-network-tuning.conf` pendant l’audit. Il décrit les valeurs configurées, sans prétendre que chaque entrée a été comparée indépendamment à toutes les surcharges ultérieures. L’ancien exemple [`90-bagelbot-net.conf`](https://github.com/AdamOusmer/ItsBagelBot/blob/main/deploy/k8s/network-tune/90-bagelbot-net.conf) contient des buffers et un backlog inférieurs; il n’a pas remplacé la politique de la flotte.

```ini
net.core.rmem_max = 67108864
net.core.wmem_max = 67108864
net.core.rmem_default = 212992
net.core.wmem_default = 212992
net.core.optmem_max = 2097152
net.ipv4.tcp_rmem = 4096 131072 67108864
net.ipv4.tcp_wmem = 4096 16384 67108864
net.ipv4.udp_rmem_min = 16384
net.ipv4.udp_wmem_min = 16384
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535
net.core.netdev_max_backlog = 65536
net.ipv4.tcp_abort_on_overflow = 0
net.core.netdev_budget = 1200
net.core.netdev_budget_usecs = 8000
net.core.dev_weight = 64
net.core.gro_normal_batch = 16
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr
net.ipv4.tcp_slow_start_after_idle = 0
net.ipv4.tcp_notsent_lowat = 131072
net.ipv4.tcp_mtu_probing = 1
net.ipv4.tcp_no_metrics_save = 1
net.ipv4.tcp_window_scaling = 1
net.ipv4.tcp_sack = 1
net.ipv4.tcp_dsack = 1
net.ipv4.tcp_fastopen = 3
net.ipv4.ip_local_port_range = 10240 65535
net.ipv4.ip_local_reserved_ports = 30000-32767
net.ipv4.tcp_tw_reuse = 1
net.ipv4.tcp_max_tw_buckets = 65536
net.ipv4.tcp_fin_timeout = 15
net.ipv4.tcp_synack_retries = 3
net.core.busy_read = 0
net.core.busy_poll = 0
net.ipv4.tcp_autocorking = 1
net.core.rps_sock_flow_entries = 32768
net.ipv4.neigh.default.gc_thresh1 = 4096
net.ipv4.neigh.default.gc_thresh2 = 8192
net.ipv4.neigh.default.gc_thresh3 = 16384
net.ipv4.conf.eth1.arp_ignore = 1
net.ipv4.conf.eth1.arp_announce = 2
net.ipv4.conf.eth1.rp_filter = 2
net.ipv4.conf.eth1.accept_redirects = 0
net.ipv4.conf.eth1.send_redirects = 0
net.netfilter.nf_conntrack_max = 393216
net.netfilter.nf_conntrack_tcp_timeout_fin_wait = 30
net.netfilter.nf_conntrack_tcp_timeout_time_wait = 30
```

Les buffers sont en octets : maxima de sockets de 64 Mio, backlog de 65536 paquets et budget softirq de 1200 paquets/8000 microsecondes. La table RFS globale reste à `32768`, tandis que chaque file a zéro entrée RFS. La présence de cette table ne signifie pas que RFS est activé sur les files. Le filtrage de chemin inverse d’eth0 a retrouvé sa valeur stricte initiale, `1`, après les tests.

## Validation et limites mesurées

L’[audit daté](https://github.com/AdamOusmer/ItsBagelBot/blob/main/docs/operations/2026-11-09-node-network-queues.md) et les [résultats lisibles par machine](https://github.com/AdamOusmer/ItsBagelBot/blob/main/docs/operations/2026-11-09-node-network-queues.json) conservent l’environnement, la méthode, les exclusions et les résultats. Il y a eu 101 commandes de mesure terminées. Les tests couvraient chaque paire ordonnée sur les deux interfaces, un et huit flux TCP, CUBIC, BBR bidirectionnel, le stress UDP et des processus UDP épinglés.

| Observation | Résultat |
| --- | --- |
| TCP eth0 final, huit flux | `6.49–8.67 Gbps` selon le sens |
| TCP eth1 après alignement IRQ, huit flux | `8.70–8.98 Gbps` |
| BBR bidirectionnel eth1 vérifié | Aller `8.34–8.69 Gbps`, retour `7.03–8.58 Gbps` simultanément |
| Interruptions après alignement | `50,889,863`, aucune hors du CPU affecté à la paire |
| Pertes NIC, qdisc et softnet dans les mesures TCP | Zéro |
| UDP eth0 épinglé, 1 Gbps demandé | `1.00 Gbps` reçu, environ `0.044%` de pertes |
| UDP eth1 épinglé, 4 Gbps demandés | `4.00 Gbps` reçus, aucune perte signalée |

L’alignement IRQ n’a pas démontré une amélioration constante du débit TCP. Les échantillons de défauts de cache à l’échelle de l’hôte n’ont pas non plus démontré de gain; ils ne prouvent pas la disparition des invalidations L1/L2/L3. Le résultat vérifié est le placement des files et la persistance de la configuration.

Le stress UDP à environ 10 Gbps produisait des pertes. Avec un processus par file, node1 → node2 recevait environ 3.19 Gbps sur eth0 et 5.84 Gbps sur eth1. L’émetteur privé n’avait aucune perte qdisc TX, tandis que l’interface du pair recevait beaucoup moins d’octets. Une partie des pertes se situe donc avant l’interface de la machine virtuelle destinataire, sans identifier l’hyperviseur, le commutateur virtuel, le lien physique ou le policer responsable.

L’essai `flow_limit=1000` recevait 2.98/5.73 Gbps sur eth0/eth1 avec UDP épinglé, contre 3.19/5.84 à `100`. La limite supérieure a été abandonnée. L’essai CUBIC n’a pas non plus justifié de remplacer BBR. Les mesures ne démontrent pas une capacité de 10 Gbps sans pertes.

## Vérification manuelle et opérations

Exécutez ces commandes de lecture en root **sur un worker**, avec `ethtool`, `iproute`, NetworkManager et Python 3 installés. Elles examinent l’état courant et les profils enregistrés; elles ne forment pas une boucle de surveillance.

```sh
ethtool -l eth0
ethtool -l eth1
ethtool -g eth0
ethtool -g eth1
ethtool -k eth0
ethtool -k eth1
ip -d link show eth0
ip -d link show eth1
tc -s qdisc show dev eth0
tc -s qdisc show dev eth1
systemctl is-enabled fleet-irq-affinity.service
systemctl is-active fleet-irq-affinity.service
/usr/local/sbin/verify-net-queues
```

[`verify-queues.py`](https://github.com/AdamOusmer/ItsBagelBot/blob/main/deploy/k8s/network-tune/verify-queues.py), installé sous `/usr/local/sbin/verify-net-queues`, vérifie les réglages actifs et enregistrés, les masques IRQ demandés et effectifs, XPS, RPS/RFS, les feuilles FQ, les offloads et les services. Son option `--allow-pending-irq` sert uniquement à l’installation : la migration effective peut attendre l’interruption suivante. Après du trafic, la validation doit réussir sans cette option.

Le déploiement n’a exigé aucun redémarrage de nœud. Les agents Cilium ont été redémarrés un worker à la fois; tous les workers et agents étaient Ready ensuite. Aucun redémarrage de nœud n’a servi à valider la persistance. Les fichiers initiaux et masques restent dans `/var/tmp/bagelbot-netqueues-20261109/` sur chaque worker. L’ancien script contient l’hypothèse d’une file unique; `before-runtime.json` conserve les masques multiqueues réellement actifs avant la modification.

Les ports de benchmark, règles de pare-feu limitées aux sources, modifications temporaires du filtrage de chemin inverse, timers de nettoyage et pods de diagnostic privilégiés ont été retirés. Les grands compteurs historiques de pertes UDP proviennent de ces tests et ne prouvent pas des pertes de production persistantes.

Poursuivez avec la [configuration d’exécution](/fr/infrastructure/runtime-configuration/) et la [mesure des performances](/fr/infrastructure/performance/). Le [répertoire de configuration](https://github.com/AdamOusmer/ItsBagelBot/tree/main/deploy/k8s/network-tune) contient l’installateur, la politique IRQ, l’unité et le validateur manuel.
