---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0004 - Adoption d’Oracle Cloud"
description: "Document de décision d’architecture : adoption d’Oracle Cloud comme hôte principal, avec un droplet DigitalOcean comme second domaine de panne"
---

**Date :** 2026-05-23

## Statut

Accepté

## Contexte

La réécriture en microservices, le passage à Go et le choix de NATS comme couche de communication (voir [ADR 0001](/fr/adr/0001-rewriting-to-microservices/), [ADR 0002](/fr/adr/0002-adoption-of-go-as-primary-service-language/) et [ADR 0003](/fr/adr/0003-adoption-of-nats-as-communication-bridge/)) reposent sur la même hypothèse : le projet doit pouvoir fonctionner avec un budget adapté à une exploitation par une seule personne. Le mainteneur est étudiant, le projet ne génère aucun revenu et l’objectif est de le garder vivant et de l’améliorer sans facture cloud récurrente qui augmenterait avec l’ambition.

Cela change notre manière de choisir un fournisseur. Les critères habituels de l’industrie (présence régionale, niveau de support, services managés, conformité d’entreprise) ne sont pas notre grille de lecture. Nous nous demandons plutôt :

- Que pouvons-nous maintenir à coût nul indéfiniment, sans expiration au bout de douze mois ?
- Où les données peuvent-elles résider légalement, et sous quelles lois ?
- Combien de pièces mobiles pouvons-nous conserver, puisque l’équipe ne compte qu’une personne ?
- Où les charges peuvent-elles fonctionner avec le plus faible coût environnemental, puisque le fonctionnement permanent est la norme ?

Le fournisseur comporte aussi un choix structurel. La plupart des offres « toujours gratuites » offrent une capacité qui peut être répartie entre plusieurs petits nœuds ou concentrée dans un nœud plus grand. La répartition semble plus sûre (domaines de panne, redémarrages progressifs faciles), mais chaque nœud supplémentaire emporte son propre noyau, ses services système, son moteur de conteneurs et ses agents de supervision. Avec un petit budget de niveau gratuit, cette surcharge n’est pas une erreur d’arrondi : elle représente une part réelle du CPU et de la RAM disponibles. Nous préférons consacrer cette capacité aux charges de travail.

## Décision

L’hôte principal du système est **Oracle Cloud Infrastructure (OCI)**, dans la région de **Montréal (ca-montreal-1)**. La flotte comprend trois nœuds répartis entre deux fournisseurs :

- **Nœud Oracle ARM (principal, niveau gratuit).** Une seule instance Ampere A1 (`VM.Standard.A1.Flex`), dimensionnée selon toute l’allocation ARM Always Free (4 OCPU, 24 Go de RAM). Elle exécute **Oracle Linux**, car cette distribution est construite et optimisée par la même entreprise que celle qui conçoit la forme basée sur Ampere : le noyau, les paquets et les réglages par défaut correspondent au matériel, et c’est la pile la plus optimisée que nous puissions exécuter gratuitement sur ce nœud. Le nœud utilise un **volume de démarrage de 100 Go provisionné à 120 VPU/Go** (niveau Ultra High Performance d’Oracle). À ce niveau de VPU, le plafond d’IOPS et de débit du volume dépasse la bande passante réseau disponible pour cette forme ; le disque cesse donc d’être le goulot d’étranglement de tout ce que nous exécutons : toute pression d’E/S apparaîtra sur le réseau ou le CPU avant d’apparaître sur le volume. Toutes les charges centrales (maître Valkey, concentrateur NATS, ...) vivent ici. Nous concentrons volontairement l’allocation ARM en un seul nœud plutôt qu’en deux, afin de ne payer les coûts du système et des agents qu’une seule fois et de laisser davantage de marge aux charges elles-mêmes.
- **Droplet DigitalOcean Intel (crédits étudiants, secours et repli AMD64).** Un droplet Intel Premium de 2 vCPU / 4 Go de RAM sous Ubuntu. Il se trouve chez un autre fournisseur et sur un autre réseau, ce qui nous donne un véritable second domaine de panne, et fournit un hôte x86 pour ce qui n’est pas encore compatible ARM (binaires tiers, outils de débogage occasionnels). C’est le seul poste qui ne nous est pas garanti de façon permanente, et il est dimensionné pour tenir jusqu’à l’expiration des crédits restants.
- **Nœud Oracle AMD micro (niveau gratuit, rôle de sentinelle).** Une instance x86 `VM.Standard.E2.1.Micro` de l’allocation AMD Always Free, sous Ubuntu. Son rôle est limité : agir comme **sentinelle Valkey**, afin de permettre l’obtention du quorum. Le garder petit et spécialisé correspond à son rôle et évite de placer une vraie charge sur un nœud à 1/8 de CPU.

**Topologie réseau.** Les deux nœuds Oracle (ARM et AMD micro) résident dans le même Virtual Cloud Network OCI, sur un sous-réseau privé, avec la base managée qui soutient le système. Garder la base sur un sous-réseau privé la retire de l’Internet public et élimine une catégorie d’exposition que nous n’avons aucune raison d’accepter. Les charges qui ont besoin de la base lui parlent via le VCN, sans NAT, sans entrée publique et sans règles de pare-feu par instance qui devraient suivre les adresses IP autorisées à atteindre la base aujourd’hui.

Le droplet DigitalOcean ne partage pas ce VCN et ne peut donc pas atteindre directement le sous-réseau privé. Nous le relions avec **Tailscale** : les deux nœuds Oracle exécutent le daemon Tailscale comme routeurs de sous-réseau en annonçant la plage privée du VCN, et le droplet DigitalOcean rejoint le même tailnet comme client. Depuis le droplet, le point d’accès à la base est joignable sur son IP privée à travers le tailnet chiffré, les nœuds Oracle agissant comme relais.

C’est aussi la seconde raison d’avoir deux nœuds Oracle. Ils assurent la haute disponibilité du chemin vers le nœud DigitalOcean. Comme les deux nœuds annoncent la même route de sous-réseau, la fonction de relais du droplet DigitalOcean est elle-même hautement disponible : si un nœud devient inaccessible, Tailscale fait passer le trafic par l’autre. La haute disponibilité de la base et celle du chemin de relais se fondent dans la même configuration à deux nœuds. DigitalOcean peut ainsi fonctionner temporairement seul pendant une maintenance ou si le nœud principal tombe.

Pourquoi Oracle Cloud précisément :

- **Coût.** Le niveau Always Free couvre l’essentiel de nos besoins sans expiration au bout de douze mois. Pour un projet étudiant, c’est la différence entre « le projet continue de fonctionner » et « le projet s’arrête à la fin de l’essai ».
- **Capacité ARM Always Free.** L’allocation ARM Always Free d’Oracle (4 OCPU, 24 Go de RAM sur Ampere A1) est de loin la capacité de calcul gratuite la plus généreuse proposée par un grand cloud. Aucune offre comparable ne s’en approche.
- **Base de données managée.** Les bases Always Free d’Oracle sont exceptionnellement généreuses. Elles proposent une base HeatWave avec un moteur MySQL disposant de 8 Go de RAM et 50 Go de stockage, ainsi qu’un accélérateur IA disposant de 16 Go de RAM. Elles proposent aussi deux bases Oracle managées de 20 Go de stockage chacune et encore davantage de stockage avec d’autres services. Cela suffit largement à la charge prévue et nous laisse le temps de grandir avant de nous inquiéter des limites de stockage.
- **Disponibilité à Montréal.** La capacité ARM Always Free dépend de la région et est souvent épuisée. Montréal a été l’une des régions les plus fiables pour obtenir effectivement une capacité Ampere A1 au niveau gratuit, ce qui a transformé ce plan théorique en déploiement fonctionnel. Pour éviter la récupération des instances, nous avons converti le compte en compte Pay-As-You-Go, ce qui permet de contourner les limites de capacité du niveau gratuit et d’accéder à un pool d’instances plus large.
- **Juridiction canadienne.** Héberger à Montréal maintient les charges et les données opérationnelles au Canada, sous le droit canadien. C’est le cadre juridique dans lequel nous voulons fonctionner, compte tenu du lieu de résidence du mainteneur, et cela évite les questions de données transfrontalières auxquelles nous ne souhaitons pas répondre.
- **Empreinte écologique.** Le réseau électrique québécois est parmi les plus propres d’Amérique du Nord (principalement hydroélectrique). Une petite flotte constamment allumée a ici une empreinte carbone nettement inférieure à celle de régions alimentées par le gaz ou le charbon, ce qui compte lorsque les charges fonctionnent en continu.

Pourquoi un grand nœud ARM plutôt que deux petits nœuds ARM : chaque nœud possède une surcharge fixe (noyau, systemd, moteur de conteneurs, agent de supervision, mémoire de base de NATS ou de la base). À notre échelle, cette surcharge est suffisamment importante par rapport au budget total pour que la doubler coûte plus de capacité qu’elle n’apporte de résilience. La résilience vient de la sentinelle AMD et du droplet DigitalOcean chez un autre fournisseur, pas d’une seconde copie de la même surcharge ARM sur le même hôte.

Pourquoi Oracle Linux sur ARM et Ubuntu ailleurs : Oracle Linux est le choix le plus optimisé pour la forme Ampere, car le fournisseur du système d’exploitation est aussi celui du matériel et du cloud. Sur le micro AMD et le droplet DigitalOcean, cet avantage n’existe pas ; Ubuntu offre une meilleure disponibilité des paquets et correspond mieux à notre expérience opérationnelle.

Le facteur dominant dans tout ce qui précède est le coût pour une exploitation étudiante par une seule personne. Tous les autres critères (région, juridiction, système d’exploitation, nombre de nœuds) ont été décidés après que ce filtre a réduit les options.

## Conséquences

- La capacité principale repose sur des ressources éligibles au niveau gratuit, facturées via un compte Pay-As-You-Go. La facture reste à 0 $ tant que nous restons dans les formes Always Free, et PAYG élimine les mécanismes de récupération en cas d’inactivité et de suspension que peuvent appliquer les comptes strictement gratuits.
- Un seul nœud principal signifie qu’une panne de l’hôte ou de l’hyperviseur arrête les charges principales. Le micro-nœud AMD et le droplet DigitalOcean sont les seuls éléments qui nous évitent une panne complète dans ce cas ; leurs rôles doivent donc rester limités et bien compris.
- La flotte utilise plusieurs architectures (ARM64 sur Oracle, AMD64 sur le micro-nœud et sur le DigitalOcean Intel x86). Les images de conteneurs doivent être construites pour plusieurs architectures, et nous devons savoir quelles charges peuvent fonctionner sur quel hôte. C’est surtout un sujet de CI : les matrices de build doivent cibler les deux architectures.
- La flotte utilise plusieurs systèmes (Oracle Linux et Ubuntu). Les gestionnaires de paquets, les valeurs par défaut des services et le rythme des mises à jour diffèrent. Tout ce qui n’est pas conteneurisé doit être installé deux fois avec deux recettes légèrement différentes. Nous gardons cet ensemble aussi réduit que possible. Jusqu’ici, il comprend l’agent k3s et le daemon Tailscale. Pour le nœud AMD, nous avons un déploiement Valkey sentinel directement sur le matériel.
- **Tailscale est une dépendance structurante.** La joignabilité entre le droplet DigitalOcean et la base dépend de la santé du tailnet et du fait qu’au moins un nœud Oracle annonce la route du sous-réseau. Nous l’acceptons en échange du fait de ne pas exposer la base publiquement et de ne pas construire notre propre VPN. La configuration à deux nœuds Oracle signifie qu’une seule panne de relais ne rompt pas ce chemin.
- **La base n’est joignable que depuis le tailnet ou le VCN.** La perte simultanée des deux nœuds Oracle met la base hors ligne quel que soit l’état du droplet DigitalOcean, car le droplet est un client de la base et non une copie. La configuration à deux nœuds Oracle limite ce risque à la panne simultanée de deux hôtes du niveau gratuit.
- DigitalOcean est la seule dépense récurrente. Elle reste volontairement faible pour tenir dans un budget étudiant, mais c’est le poste à surveiller si le projet évolue dans une direction qui lui demande davantage.
- Il existe un certain verrouillage vis-à-vis d’Oracle au niveau de la flotte (conditions du niveau gratuit, compte, région), mais les charges elles-mêmes sont des services Go conteneurisés reposant sur des primitives courantes (NATS, base de données, Linux), ce qui borne le coût d’un transfert vers un autre fournisseur.
- Le mainteneur est le titulaire de la facturation. Si sa situation change, le compte change avec lui ; la procédure d’amorçage de toute la flotte (compte fournisseur, région, demande de capacité, construction des images, secrets) fait donc partie de la documentation du projet et non d’un savoir tribal.

## Alternatives étudiées

- **AWS Free Tier.** La limite de douze mois contredit l’exigence centrale : fonctionner en permanence sans que l’horloge arrive à son terme. Après l’essai, une capacité équivalente dépasse largement ce qu’un projet étudiant peut absorber. Montréal (ca-central-1) est également l’une des régions les plus coûteuses pour un usage payant.
- **Google Cloud Free Tier.** Le `e2-micro` Always Free est beaucoup trop petit pour les charges (1 à 2 vCPU en burst, 1 Go de RAM, une seule zone) et il n’existe pas d’équivalent ARM dans le niveau gratuit. Utile pour une expérience, mais pas pour une flotte.
- **Azure Free.** Offre une forme similaire à AWS : crédits limités dans le temps, aucun niveau Always Free d’une taille utile et une tarification après l’essai incompatible avec le budget.
- **hetzner / OVH / vultr.** Tarification bon marché et transparente, mais paiement dès le premier jour. Même quelques dollars par nœud et par mois s’accumulent dans une flotte et franchissent la limite d’une soutenabilité indéfinie avec un budget étudiant. Ces fournisseurs restent de bonnes options si le financement du projet évolue.
- **DigitalOcean pour tout.** C’est opérationnellement le plus simple (un fournisseur, une console, une facturation), mais toute la flotte sur DigitalOcean représente une vraie facture mensuelle. Nous conservons un droplet DigitalOcean pour sa valeur (autre fournisseur, autre domaine de panne, hôte AMD64) sans lui faire porter tout le système.
- **Hébergement sur du matériel résidentiel.** Pas de facture cloud, mais les FAI résidentiels interdisent souvent les services permanents, la fiabilité de la liaison montante ne correspond pas aux besoins de production et les coûts d’électricité et de refroidissement ne sont pas nuls. Le coût initial doit également être pris en compte pour un étudiant.
- **Répartir l’allocation ARM Oracle entre deux petits nœuds ARM.** Cette option a été étudiée pour les domaines de panne, mais la surcharge du système et des agents consommerait une part notable du niveau gratuit, et le gain de résilience serait limité puisque les deux nœuds resteraient dans la même région Oracle. Le nœud AMD et le droplet DigitalOcean offrent un véritable second domaine de panne à moindre coût en capacité.
