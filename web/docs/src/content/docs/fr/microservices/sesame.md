---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Sesame
description: Moteur principal et processeur de commandes d'ItsBagelBot.
---

Sesame est le moteur principal d'ItsBagelBot. Il consomme les événements d'ingress, évalue les conditions, distribue les commandes, exécute les gestionnaires d'événements des modules et achemine les sorties vers le service outgress.

## Architecture

Sesame fonctionne comme un consommateur NATS JetStream fortement concurrent. Il tire les messages des sujets `twitch.ingress.event.*` sur deux voies (Premium et Standard) et les vide dans un pool partagé qui réserve une capacité aux événements Premium.

### Le pipeline

Chaque message passe par `engine.Pipeline`, une étape de décodage et de distribution sans allocation (hors émissions) :
1. **Décodage** : le payload JSON est désérialisé dans un `lane.Envelope` mis en pool.
2. **Éligibilité** : les événements non pris en charge et les enveloppes sans broadcaster valide sont écartés avant toute projection ou commande.
3. **Vues des modules** : si l'événement requiert un comportement configurable, le `Projector` est interrogé pour obtenir l'ensemble `ModuleView` du broadcaster.
4. **Distribution de commande** : si l'événement est un message de chat (`channel.chat.message`), le pipeline consulte le registre des commandes. Les commandes intégrées sont évaluées selon leurs permissions, leurs cooldowns et leurs conditions d'exécution uniquement en direct.
5. **Gestionnaires d'événements** : les gestionnaires non liés aux commandes enregistrés par les modules sont exécutés dans l'ordre d'enregistrement.
6. **Émission** : les gestionnaires de modules ne publient pas directement sur NATS. Ils produisent une structure `Output` pour un callback `Emit`, qui construit le contrat filaire `outgress.Message` et le publie sur `twitch.outgress.premium` ou `twitch.outgress.standard`.

La relecture du transport ne revendique aucune clé Valkey. Les sorties dérivées d'une entrée possédant un identifiant d'événement stable utilisent un identifiant de publication stable et attendent la confirmation NATS avant que l'entrée soit acquittée. Valkey reste l'état du domaine, et non un second système d'acquittement du transport.

### Création des modules

Les fonctionnalités de Sesame sont écrites dans le paquet `module`. Pour conserver une excellente testabilité et une surface d'auteur totalement dépourvue de câblage d'exécution (Valkey, Projector ou NATS), les fonctionnalités sont déclarées avec un **motif de builder fluent**.

Un module est instancié, ses commandes et gestionnaires d'événements sont chaînés, puis il renvoie un `Module` immuable consommé au démarrage par `engine.Registry`.

#### Le builder fluent

Voici un exemple de création d'un module avec le builder :

```go
func MyModule() module.Module {
    // 1. Initialize with name and kind (Core, Default, Opt-In)
	m := module.NewModule("my_feature", module.KindDefault)

    // 2. Register non-command EventSub handlers
	m.On("channel.chat.message", handleChat)
    m.On("stream.online", handleStreamOnline)

    // 3. Declare commands with chained gates
	m.Command("ping").Everyone().Run(pingRun)
	m.Command("announce").Mod().Cooldown(10 * time.Second).Run(announceRun)
    m.Command("shoutout").Aliases("so").Mod().LiveOnly().Run(soRun)

    // 4. Validate and build the immutable artifact
	return m.Build()
}
```

#### Types de modules

Les modules sont déclarés avec l'un des trois types suivants, qui déterminent leur logique d'activation :
- **Core** : toujours activé, jamais basculé, et sans lecture de projection.
- **Default** : activé par défaut, mais désactivable depuis le dashboard.
- **Opt-In** : désactivé par défaut, et doit être activé explicitement depuis le dashboard.

#### Conditions de commande

La méthode `.Command("name")` renvoie un `CmdBuilder` qui permet d'enchaîner les conditions d'exécution avant la finalisation avec `.Run()`.
- **Permissions** : `.Everyone()`, `.Sub()`, `.VIP()`, `.Mod()`, `.Broadcaster()`
- **État** : `.LiveOnly()`, `.Cooldown(time.Duration)`
- **Routage** : `.Aliases("trigger")`, `.NumericSuffix()` (absorbe les chiffres finaux directement ; par exemple `!clip30` devient `clip`).

Au démarrage, `engine.Registry` indexe ces modules construits en créant un index plat et insensible à la casse des commandes ainsi qu'une table de routage des types d'événements.

### Variables et modèles

Sesame fournit un système de modèles de chaînes rapide et économe en allocations pour les réponses de commandes dynamiques. Les fonctions `module.Expand` (et `module.ExpandString`) analysent les chaînes à la recherche de jetons `{key}` et les résolvent avec un callback fourni.

Le système peut laisser passer les jetons littéraux `{key}` si le callback ne les reconnaît pas, au lieu de les supprimer silencieusement.

En outre, l'assistant `module.ParseDynamic` prend en charge les variables dynamiques génériques :
- `{random}` : génère un nombre aléatoire entre 1 et 100.
- `{random:min-max}` : génère un nombre aléatoire entre `min` et `max`.
- `{choice:a,b,c}` : choisit une chaîne aléatoire dans une liste séparée par des virgules.

### État et cache

Sesame conserve un débit élevé en évitant les lectures de base de données sur le chemin critique :
- **Cache de projection** : `projection.Reader` fournit un cache mémoire des paramètres du broadcaster (modules, utilisateurs et commandes personnalisées), avec repli vers le RPC NATS (`bagel.rpc.internal.projection.*`) et écoute des diffusions `bagel.cache.invalidate.*`.
- **Stockage live** : `ValkeyLiveStore` vérifie si un broadcaster est actuellement en direct. Il met les résultats en cache localement, se replie sur Valkey et peut déclencher une vérification de la voie outgress système vers Twitch si la clé est absente.
- **État du domaine** : les cooldowns, minuteries, vues live de fidélité, fenêtres de salutations et état de réputation sont stockés dans Valkey. La relecture du transport ne l'est pas.

### Déduplication des alertes de follow

Twitch renvoie `channel.follow` lors de chaque nouveau follow ; une boucle unfollow/refollow pourrait donc déclencher l'alerte de chat aussi vite qu'un spectateur peut cliquer. Le module d'alertes revendique `alert:follow:<broadcaster>:<follower>` pendant 72 heures avec la même idiome `SET key 1 NX PX` qu'un cooldown de commande, et ne publie le remerciement que s'il remporte la revendication. Les deux moitiés de la clé sont des identifiants Twitch numériques : un changement de nom ne rouvre donc pas la fenêtre et la revendication d'une chaîne n'en supprime jamais une autre. La revendication est faite après la lecture de l'activation ; une chaîne dont les alertes de follow sont désactivées ne consomme jamais une fenêtre qu'elle voudrait utiliser dès leur réactivation.

Seuls les follows sont soumis à cette règle. Les abonnements, cadeaux, cheers et raids coûtent chacun quelque chose à l'émetteur, tandis que les pauses publicitaires sont l'événement propre à la chaîne.

Trois propriétés de la flotte rendent sûre la conservation d'une fenêtre de plusieurs jours dans Valkey :

- **La revendication arrive toujours sur le primaire.** `SET ... NX` est une écriture ; le retard d'un réplica ne peut donc pas faire croire à deux réplicas qu'ils ont tous deux remporté la fenêtre.
- **La fenêtre survit aux redémarrages.** L'espace de clés utilise AOF avec `appendfsync everysec` sur un volume persistant (`save ""`, sans snapshots RDB). L'AOF enregistre les expirations comme des horodatages absolus ; un redémarrage de pod ou de toute la flotte restaure donc chaque revendication avec son temps *restant*, et non avec 72 heures renouvelées. La seule fenêtre de perte est la fraction de seconde d'écritures qu'un arrêt brutal peut perdre, plus les quelques millisecondes d'écritures non répliquées supprimées par un basculement Sentinel. Dans les deux cas, le coût maximal est une alerte en double.
- **L'ensemble de clés reste réduit.** À 5 000 follows par jour, une chaîne conserve environ 15 000 revendications d'environ 120 octets chacune : moins de 2 Mio sur le budget `maxmemory` de 512 Mio. La fenêtre pourrait être portée à une semaine avec le même ordre de coût. La pression à surveiller n'est pas la taille mais la politique : `maxmemory-policy volatile-lru` évince d'abord les clés portant un TTL, et ces revendications sont écrites une fois et rarement lues, ce qui en fait les clés les plus froides de l'espace. Une pression mémoire prolongée les évincerait et rouvrirait les alertes.

La condition échoue en mode permissif. Si Valkey est inaccessible, l'alerte est tout de même publiée et l'absence est journalisée, car perdre un remerciement est pire qu'un doublon, et l'abus contre lequel la condition existe ne doit pas provoquer la panne.
