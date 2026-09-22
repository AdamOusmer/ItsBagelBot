---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Contrats RPC
description: "Chaque endpoint NATS request-reply d'ItsBagelBot : sujets, formes JSON des requêtes/réponses, propriétaires et délais."
---

Tous les appels interservices utilisent **NATS request-reply** avec des corps JSON. Les services
s'abonnent avec un **groupe de files** afin que toute réplique puisse répondre et que la charge
se répartisse dans le parc. Cette page répertorie tous les endpoints actifs ; les sujets peuvent
être remplacés par l'environnement, mais les valeurs ci-dessous sont celles livrées.

## Conventions

- **Encodage** : JSON, UTF-8. Les corps vides sont autorisés lorsque le tableau indique `{}`.
- **Identifiants** : les identifiants Twitch sont envoyés comme **chaînes décimales**, jamais comme nombres.
- **Erreurs** : les réponses contiennent un champ texte `error`. Il est vide ou omis en cas de succès ; une valeur non vide signifie que l'appel a échoué (le transport a néanmoins abouti).
- **Niveau et statut** : `status` est l'enum brut de la base (`free` | `paid` | `vip`). `tier` est dérivé : `premium` si le statut est paid ou vip et actif, sinon `standard`.
- **Délais** : les appelants fixent une échéance (souvent 5 s) ; les handlers limitent leur travail avec un délai de contexte (indiqué par groupe). Un délai du handler plus court que celui de l'appelant est volontaire afin que ce dernier reçoive une réponse d'erreur.

## Carte des sujets

| Sujet (défaut) | Propriétaire | Type |
|---|---|---|
| `bagel.rpc.dashboard.*` | users | request-reply |
| `bagel.rpc.commands.*` | commands | request-reply |
| `bagel.rpc.admin.user.*` | users | request-reply |
| `bagel.rpc.broadcaster.status.get` | projector | request-reply |
| `bagel.rpc.internal.projection.users.get` | users | request-reply |
| `bagel.rpc.internal.projection.modules.get` | modules | request-reply |
| `bagel.rpc.internal.projection.commands.get` | commands | request-reply |
| `bagel.rpc.internal.tokens.*` | users | request-reply |
| `bagel.rpc.outgress.{channel,system}.*` | outgress | request-reply |
| `twitch.ingress.admin.shards.get` | ingress | request-reply |
| `bagel.cache.invalidate.broadcaster` | users (pub) | événement (fire-and-forget) |

---

## Tableau de bord (users) — `bagel.rpc.dashboard.*`

Libre-service de la chaîne, appelé par le tableau de bord. Groupe de files `users-rpc`.
Délai du handler **3 s**. `broadcaster_user_id` est une chaîne décimale.

| Verbe | Requête | Réponse |
|---|---|---|
| `upsert_user` | `{user_id, username, display_name}` | `{ok:true}` ou `{error}` |
| `grant_save` | `{broadcaster_user_id, access_token, refresh_token}` | `{ok:true}` ou `{error}` ; publie aussi une invalidation du cache |
| `grant_has` | `{broadcaster_user_id}` | `{has_grant:bool}` |
| `active_set` | `{broadcaster_user_id, active:bool}` | `{ok:true}` ou `{error}` ; publie une invalidation du cache |
| `active_get` | `{broadcaster_user_id}` | `{active:bool}` |
| `status_get` | `{broadcaster_user_id}` | `{status}` (enum brut) |

## Commandes — `bagel.rpc.commands.*`

Commandes de chat personnalisées d'une chaîne, appelées par le tableau de bord. Groupe de files
`commands-rpc`. Délai du handler **2 s**. `user_id` est une chaîne décimale.

Structure de réponse commune :

```json
{ "commands": [ {"name": "...", "response": "...", "is_active": true} ], "error": "" }
```

| Verbe | Requête | Notes |
|---|---|---|
| `list` | `{user_id}` | renvoie l'ensemble actuel des commandes |
| `upsert` | `{user_id, name, response, is_active, original_name?}` | write-behind (~2 s) ; la réponse est une liste fusionnée **optimiste**. Une erreur de validation est renvoyée avec la liste inchangée. Si `original_name` est défini et diffère de `name`, la ligne est **renommée sur place** (champ name mis à jour, identité conservée) — immédiatement, sans write-behind — au lieu d'une suppression puis recréation |
| `delete` | `{user_id, name}` | immédiat ; invalide le cache, donc la liste renvoyée est à jour |

## Utilisateurs administrateurs — `bagel.rpc.admin.user.*`

Gestion des utilisateurs par les opérateurs, appelée par la console d'administration ou l'ancien outil.
Groupe de files `users-rpc`. Délai du handler **3 s**. L'outil n'ouvre jamais la base ; c'est son seul accès.

Structure de réponse commune (champs présents selon le verbe) :

```json
{
  "user":  {"id": 1, "username": "x", "is_active": true, "status": "paid", "updated_at": "..."},
  "users": [ /* même forme */ ],
  "stats": {"total_users": 0, "active_users": 0, "premium_users": 0, "vip_users": 0, "paid_users": 0},
  "token": {"present": true},
  "error": ""
}
```

| Verbe | Requête | Résultat |
|---|---|---|
| `get` | `{user_id}` ou `{username}` | `user` |
| `list` | `{limit}` (1–100, défaut 20) | `users`, du plus récemment modifié au plus ancien |
| `stats` | `{}` | `stats` |
| `set_status` | `{user_id, status}` (`free`/`paid`/`vip`) | `user` ; crée la ligne si elle est inconnue ; invalide le cache |
| `reset` | `{user_id}` | `user` ; efface les jetons de l'utilisateur |
| `token_set` | `{user_id, access_token, refresh_token}` | `token` ; crée la ligne si elle est inconnue (installation du jeton du compte bot) |
| `token_status` | `{user_id}` | `token` (présence uniquement, jamais la valeur du jeton) |
| `token_clear` | `{user_id}` | `token` ; supprime le jeton stocké et invalide le cache |
| `delete` | `{user_id}` ou `{username}` | réponse vide ; supprime l'utilisateur en cascade |

## Niveau de la chaîne — `bagel.rpc.broadcaster.status.get`

Recherche du niveau sur le chemin critique, servie par le projector. Groupe de files `projector-rpc`.
Délai du handler **1,5 s**. Ordre de résolution : cache en processus (TTL 30 s) → projection Valkey
→ repli avec chargement différé via le RPC de projection users (résultat mis en cache). Un utilisateur
manquant ne provoque jamais d'erreur : une valeur inconnue ou invalide devient `standard`.

| Requête | Réponse |
|---|---|
| `{broadcaster_id}` | `{broadcaster_id, tier}` où `tier` vaut `premium` ou `standard` |

## Projections internes — `bagel.rpc.internal.projection.*.get`

Vues read-through utilisées par le projector pour matérialiser la projection des réglages lors du
stream-online. Délai du handler **2 s**. `user_id` est une chaîne décimale.

| Sujet | Propriétaire / groupe de files | Réponse |
|---|---|---|
| `...projection.users.get` | users / `users-rpc` | `{user_id, status, is_active, error}` |
| `...projection.modules.get` | modules / `modules-rpc` | `{user_id, modules: [ModuleView], error}` |
| `...projection.commands.get` | commands / `commands-rpc` | `{user_id, commands: [CommandView], error}` |

Formes des vues :

```json
CommandView { "name": "...", "response": "...", "is_active": true }
ModuleView  { "name": "...", "is_enabled": true, "configs": { /* JSON brut, omis si vide */ } }
```

## Jetons internes — `bagel.rpc.internal.tokens.*`

Cycle de vie des jetons Twitch du compte bot. Outgress lit le refresh token du bot lors du renouvellement
et réécrit le jeton renouvelé afin qu'un redémarrage ne ressuscite jamais une valeur obsolète.
**Les jetons en clair transitent par ces sujets** ; l'autorisation NATS limite les abonnés possibles.
Propriétaire users, groupe de files `users-rpc`, délai du handler **3 s**.

| Verbe | Requête | Réponse |
|---|---|---|
| `get` | `{user_id}` | `{access_token, refresh_token, error}` |
| `save` | `{user_id, access_token, refresh_token}` | `{}` ou `{error}` |

## Gestion outgress — `bagel.rpc.outgress.*`

Contrôle opérateur de l'expéditeur. Groupe de files `outgress-rpc`. Délai du handler **1,5 s**.

| Sujet | Requête | Réponse |
|---|---|---|
| `...channel.get` | `{broadcaster_id}` | `{channel, found, error}` |
| `...channel.set` | `{broadcaster_id, enabled?, is_mod?}` | `{channel, found, error}` ; crée la chaîne si elle manque ; une surcharge `is_mod` compte comme vérification |
| `...channel.list` | `{}` | `{channels, error}` |
| `...system.status` | `{}` | `{paused, app_token_expires_in_seconds, has_user_token, error}` |
| `...system.pause` | `{paused:bool}` | `{paused, error}` (kill switch) |

`enabled` et `is_mod` sont **facultatifs** (pointeur/omis) sur `channel.set` ; leur absence
signifie qu'ils restent inchangés. `channel` :

```json
{ "broadcaster_id": "...", "enabled": true, "is_mod": false,
  "mod_checked_at": "...", "updated_at": "..." }
```

## Instantané des shards ingress — `twitch.ingress.admin.shards.get`

Vue en direct du parc de shards EventSub, servie par n'importe quelle réplique ingress (Elixir).
Délai de l'appelant **5 s** (ingress limite le travail par shard à ~2 s). Le corps de la requête est vide.

Réponse (`Snapshot`) :

```json
{
  "generated_at": "2026-06-15T00:00:00Z",
  "reporter": "ingress-node1",
  "nodes": ["node1", "node2"],
  "shard_count": 2,
  "conduit_manager": { "state": "...", "node": "...", "conduit_id": "..." },
  "shards": [
    {
      "shard_id": 0, "state": "connected", "node": "node1",
      "session_id": "...", "bound": true, "handshake_in_flight": false,
      "keepalive_ms": 10000, "attempts": 0,
      "bound_at": "...", "last_frame_at": "..."
    }
  ]
}
```

`state` du shard vaut l'une des valeurs suivantes : `connected`, `migrating`, `binding`, `connecting`,
`backoff`, `unregistered`, `unresponsive`. `conduit_manager` décrit le reconciler du conduit,
singleton du cluster.

## Invalidation du cache — `bagel.cache.invalidate.broadcaster`

Ce n'est pas un request-reply : il s'agit d'une publication fire-and-forget. Elle est émise par le
service users après tout changement affectant l'état mis en cache d'une chaîne (statut, activation,
octroi ou suppression de jeton). Corps : `{broadcaster_id}` (chaîne décimale). Les abonnés (projector,
ingress) abandonnent leur vue en cache et la rechargent avec chargement différé.
