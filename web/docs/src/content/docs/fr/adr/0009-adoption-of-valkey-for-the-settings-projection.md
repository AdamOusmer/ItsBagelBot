---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0009 - Adoption de Valkey pour la projection des réglages"
description: "ADR : adoption de Valkey comme projection de lecture des réglages et du niveau, alimentée par un projector dédié."
---

**Date :** 2026-06-09

## Statut

Accepté.

## Contexte

Le hot path de tout le système consiste à réagir à un message de chat. Pour cela, un worker ingress (voir [ADR 0006](/fr/adr/0006-adoption-of-elixir-for-twitch-ingress/)) a besoin des réglages de la chaîne : modules activés, configuration de chacun et niveau du streamer. Cette lecture peut avoir lieu pour chaque message de chaque chaîne et ne peut pas toucher MySQL : l'instance HeatWave gratuite de [l'ADR 0005](/fr/adr/0005-adoption-of-mysql-heatwave/) est dimensionnée pour la source de vérité, pas pour des lectures par message.

Les caches en processus de [l'ADR 0008](/fr/adr/0008-caching-and-write-behind-strategy/) protègent chaque service de données contre ses propres lecteurs, mais n'aident pas un consommateur d'un autre processus, a fortiori écrit dans un autre langage. Les workers ingress ne doivent pas chacun maintenir une hiérarchie de cache contre quatre services et réimplémenter l'invalidation en Elixir.

Le côté lecture veut un emplacement où l'état complet des réglages d'un utilisateur puisse être obtenu en un aller-retour, et qui reste à jour grâce au système plutôt qu'au lecteur.

Nos exigences sont les suivantes :

- une lecture clé-valeur renvoie tout ce dont le hot path a besoin pour une chaîne : niveau, indicateur actif, chaque activation de module et chaque configuration ;
- la mise à jour vient des événements de changement, sans que les lecteurs sachent où vivent les données ;
- la reconstruction depuis zéro est possible : la projection est un cache, jamais la source de vérité ;
- une solution open source, légère, avec des clients mûrs en Go et en Elixir ;
- aucune lecture des schémas des services de données, ce qui briserait l'isolation de [l'ADR 0005](/fr/adr/0005-adoption-of-mysql-heatwave/).

## Décision

Nous adoptons **Valkey** comme magasin de projection des réglages, écrit par un service **projector** dédié (`app/projector`) et lu par tout ce qui se trouve sur le hot path.

**Organisation.** Un hash par utilisateur, lisible par un unique `HGETALL` :

```
settings:<user_id>
  status                  free | paid | vip
  active                  0 | 1
  module:<name>:enabled   0 | 1
  module:<name>:config    raw JSON
```

Les lecteurs n'analysent que le blob de configuration du module qu'ils utilisent réellement. Les noms de modules sont validés strictement à la frontière d'écriture (alphanumériques minuscules, underscore et tiret), car le nom est inclus dans le champ ; un deux-points pourrait sinon forger un autre champ.

**Le projector est un consommateur pur d'événements.** Il s'abonne aux événements de changement des utilisateurs et modules avec un queue group durable, afin que chaque événement soit plié dans Valkey exactement une fois et que le consommateur conserve sa position après redémarrage. Chaque gestionnaire remplace l'état complet porté par l'événement, ce qui rend redelivery et replay inoffensifs. Le projector ne consulte jamais le schéma d'un autre service.

**Les démarrages à froid utilisent une poignée de main de reproject.** Au démarrage, le projector publie `data.reproject.request`. Chaque service de données répond depuis son propre groupe durable (une seule instance par service rejoue), en republiant ses lignes courantes comme événements de changement ordinaires, par pages afin de ne jamais charger une table entière. Une projection Valkey neuve ou effacée converge sans franchir de frontière de schéma et, puisque les écritures remplacent l'état, un replay concurrent de changements live converge vers l'état le plus récent.

**Pourquoi Valkey plutôt que Redis.** Même protocole, même modèle de données et mêmes clients ; la différence est la gouvernance et la licence. Valkey est le fork de la Linux Foundation resté BSD après le passage de Redis à une licence restrictive, ce qui compte pour un projet susceptible d'être auto-hébergé indéfiniment sur le nœud ARM Oracle.

## Conséquences

- Le hot path lit un hash Valkey et ne touche jamais MySQL. La base sert les écritures et les reconstructions à froid.
- La projection est éventuellement cohérente. Un changement de réglage devient visible après la fenêtre write-behind de [l'ADR 0008](/fr/adr/0008-caching-and-write-behind-strategy/) et le saut d'événement, bien dans ce qu'un utilisateur du dashboard perçoit comme immédiat, mais sans garantie read-your-write pour un lecteur externe.
- Valkey entre dans l'histoire de disponibilité du hot path. Sa perte ne fait pas perdre les données (la projection se reconstruit à partir d'une demande de reproject), mais les lecteurs doivent se dégrader délibérément pendant l'indisponibilité.
- Un événement manqué au-delà de la rétention JetStream laisse la projection obsolète pour l'utilisateur concerné jusqu'au prochain changement ou reproject. Nous l'acceptons selon la rétention de [l'ADR 0003](/fr/adr/0003-adoption-of-nats-as-communication-bridge/), et la poignée de main de reproject sert aussi d'outil de réconciliation.
- Les commandes ne sont volontairement pas projetées. Elles sont lues par le service commands sur un chemin plus lent et servies depuis son cache en processus ; les projeter grossirait chaque hash pour des données dont le hot path n'a pas besoin à chaque message.

## Alternatives étudiées

- **Redis.** Fonctionnellement identique aujourd'hui, mais la direction de sa licence ne convient pas à l'auto-hébergement du projet et Valkey offre une gouvernance ouverte. Rejeté par principe, sans coût technique.
- **NATS JetStream KV.** Déjà disponible et évitant un nouveau système, mais la projection veut un hash par utilisateur, des écritures par champ et une lecture unique de nombreux champs ; le modèle plat de KV ne le permettrait pas sans inventer un encodage. Les lectures par message appartiennent à un serveur de structures de données, pas au substrat de coordination. Rejeté.
- **Chaque consommateur met en cache les services.** Pas de nouvelle infrastructure, mais chaque consommateur (y compris l'ingress Elixir) réimplémenterait cache et invalidation et les services absorberaient les cold misses de chacun. Le projector centralise ce travail une fois. Rejeté.
- **Request-reply vers les services sur le hot path.** Correct et toujours frais, mais place quatre services et la base dans le budget de latence de chaque message de chat. C'est précisément le couplage que la projection doit supprimer. Rejeté.
