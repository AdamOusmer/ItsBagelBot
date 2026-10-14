---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Contrats RPC
description: "Chaque endpoint NATS request-reply d'ItsBagelBot : sujets, formes JSON des requêtes/réponses, propriétaires et délais."
---

Tous les appels interservices utilisent **NATS request-reply** avec des corps JSON. Les services
s'abonnent avec un **groupe de files** afin que toute réplique puisse répondre et que la charge
se répartisse dans le parc. Cette page répertorie les endpoints principaux ; les sujets peuvent
être remplacés par l'environnement, mais les valeurs ci-dessous sont celles livrées.
Les listes d'imports par compte de `deploy/messaging/accounts.yaml` indiquent quel
service peut appeler quel sujet.

## Conventions

- **Encodage** : JSON, UTF-8. Les corps vides sont autorisés lorsque le tableau indique `{}`.
- **Identifiants** : les identifiants Twitch sont envoyés comme **chaînes décimales**, jamais comme nombres.
- **Erreurs** : les réponses contiennent un champ texte `error`. Il est vide ou omis en cas de succès ; une valeur non vide signifie que l'appel a échoué (le transport a néanmoins abouti). La plupart des réponses des services de données portent aussi un `code` (`invalid`, `not_found`, `forbidden`, `conflict`, `unavailable`, `internal`) défini dans `internal/domain/rpc/code.go`. Les réponses du projector et de la gestion outgress ne portent que `error`.
- **Niveau et statut** : `status` est l'enum brut de la base (`free` | `paid` | `vip`). `tier` est dérivé : `premium` si le statut est paid ou vip, sinon `standard`.
- **Délais** : les appelants fixent une échéance (souvent 5 s) ; les handlers limitent leur travail avec un délai de contexte (2 s par défaut, `pkg/bus/rpcwiring.go` ; indiqué par groupe). Un délai du handler plus court que celui de l'appelant est volontaire afin que ce dernier reçoive une réponse d'erreur.

## Carte des sujets

| Sujet (défaut) | Propriétaire | Type |
|---|---|---|
| `bagel.rpc.dashboard.*` | users | request-reply |
| `bagel.rpc.commands.*` | commands | request-reply |
| `bagel.rpc.admin.user.*` | users | request-reply |
| `bagel.rpc.broadcaster.{status,live,stream_info}.get` | projector | request-reply |
| `bagel.rpc.projector.dashboard.*` | projector | request-reply |
| `bagel.rpc.internal.projection.users.get` | users | request-reply |
| `bagel.rpc.internal.projection.modules.get` | modules | request-reply |
| `bagel.rpc.internal.projection.commands.get` | commands | request-reply |
| `bagel.rpc.internal.tokens.*` | users | request-reply |
| `bagel.rpc.outgress.*` | outgress | request-reply |
| `twitch.ingress.admin.shards.get` | ingress | request-reply |
| `bagel.cache.invalidate.<scope>` | users, projector, autres (pub) | événement (fire-and-forget) |

Autres familles, chacune documentée dans le fichier de contexte de son application sous `app/` :

| Préfixe de sujet | Propriétaire |
|---|---|
| `bagel.rpc.modules.*` (dont `.govee`, `.spotify`) | modules |
| `bagel.rpc.loyalty.*` | loyalty |
| `bagel.rpc.transactions.*`, `bagel.rpc.admin.giveaways.*`, `bagel.rpc.internal.billing.apply` | transactions (billing apply est servi par users) |
| `bagel.rpc.notifications.*`, `bagel.rpc.admin.notifications.*` | notifications |
| `bagel.rpc.delegation.*` | users |
| `bagel.rpc.discord-data.*` | discord-data |
| `bagel.rpc.discord-outgress.*`, `bagel.rpc.dingress.*` (ancien nom conservé pour la compatibilité du protocole) | discord outgress |
| `bagel.rpc.gossip.*` | gossip |
| `bagel.rpc.admin.deploy.*` | deployer |
| `bagel.rpc.health.<service>` | chaque service |

---

## Tableau de bord (users) : `bagel.rpc.dashboard.*`

Libre-service de la chaîne, appelé par le tableau de bord. Groupe de files `users-rpc`.
Délai du handler **3 s**. `broadcaster_user_id` est une chaîne décimale.

| Verbe | Requête | Réponse |
|---|---|---|
| `upsert_user` | `{user_id, username, display_name, email?}` | `{ok:true}` ou `{error}` ; publie une invalidation de statut |
| `grant_save` | `{broadcaster_user_id, access_token, refresh_token}` | `{ok:true}` ou `{error}` ; publie aussi une invalidation du cache |
| `grant_has` | `{broadcaster_user_id}` | `{has_grant:bool}` |
| `active_set` | `{broadcaster_user_id, active:bool}` | `{ok:true}` ou `{error}` ; publie une invalidation du cache |
| `active_get` | `{broadcaster_user_id}` | `{active:bool}` |
| `status_get` | `{broadcaster_user_id}` | `{status, onboarded}` (status est l'enum brut) |
| `state_get` | `{broadcaster_user_id}` | état du compte : `active`, `username`, `display_name`, `status`, `onboarded`, `locale`, `custom_cursor`, `commands_page_hidden`, `creator_code`, plus les champs d'abonnement |
| `login_resolve` | `{login}` | `{user_id, username}` |
| `onboarded_set` | `{broadcaster_user_id, onboarded}` | `{ok:true}` ou `{error}` |
| `locale_set` | `{broadcaster_user_id, locale}` | `{ok:true}` ou `{error}` |
| `cursor_set` | `{broadcaster_user_id, custom_cursor}` | `{ok:true}` ou `{error}` |
| `commands_page_set` | `{broadcaster_user_id, commands_page_hidden}` | `{ok:true}` ou `{error}` ; masque ou affiche la page publique des commandes |
| `delete_self` | `{user_id}` | `{ok:true}` ou `{error}` |

## Commandes : `bagel.rpc.commands.*`

Commandes de chat personnalisées d'une chaîne, appelées par le tableau de bord. Groupe de files
`commands-rpc`. Délai du handler **2 s** (valeur par défaut). `user_id` est une chaîne décimale.

Champs de requête : `user_id`, `name`, `aliases`, `response`, `is_active`,
`stream_online_only`, `perm`, `cooldown`, `user_cooldown`, `allowed_user_id`,
`bump_counter`, `original_name?`, `restore_uses?`. Structure de réponse :

```json
{ "commands": [ /* CommandView */ ], "restored": false, "error": "", "code": "" }
```

| Verbe | Requête | Notes |
|---|---|---|
| `list` | `{user_id}` | renvoie l'ensemble actuel des commandes |
| `upsert` | les champs ci-dessus | write-behind (~2 s). La réponse ne contient pas de liste ; lisez l'ensemble via les sujets du tableau de bord du projector. Si `original_name` est défini et diffère de `name`, la ligne est **renommée sur place** (identité conservée), immédiatement et sans write-behind. Si `restore_uses` est positif et `original_name` vide, une commande supprimée est restaurée avec son nombre d'utilisations et `restored` est activé |
| `delete` | `{user_id, name}` | immédiat ; invalide le cache et publie un événement de suppression |

Sources : `app/db/commands/rpc/dashboard.go`, `internal/domain/rpc/commands/commands.go`.

## Utilisateurs administrateurs : `bagel.rpc.admin.user.*`

Gestion des utilisateurs par le personnel, appelée par la console d'administration. Groupe de files `users-rpc`.
Délai du handler **3 s**. Chaque verbe prend `actor_id` ; le service détermine lui-même le rôle de l'acteur
et refuse avec `forbidden` sous le rôle minimal du verbe. Le champ `actor_role` est fourni par le client et
n'est jamais cru. La console n'ouvre jamais la base ; c'est son seul accès.

Champs de réponse (présents selon le verbe) : `user`, `users`, `stats`, `enrollment`, `token`,
champs de pagination (`page`, `page_size`, `max_pages`, `has_more`), plus `error` et `code`.

| Verbe | Rôle minimal | Résultat |
|---|---|---|
| `get`, `list`, `stats`, `enrollment`, `overview`, `token_status` | modérateur | `user`, `users`, `stats`, `enrollment` ou `token` (présence uniquement, jamais la valeur du jeton) |
| `ban`, `unban` | modérateur | `user` |
| `set_status`, `set_active`, `set_creator_code`, `test.set`, `reset` | admin | `user` |
| `token_set`, `token_clear` | admin | `token` |
| `bot_token_set` | contrôle du grant | `token` ; installe le jeton du compte bot après validation par Twitch de son identité et de ses scopes |
| `delete` | propriétaire | réponse vide ; supprime l'utilisateur en cascade |

La liste du personnel et le journal d'audit se trouvent sous `bagel.rpc.admin.user.auth.*` et
`bagel.rpc.admin.user.audit.*`. Sources : `app/db/users/rpc/admin.go`,
`internal/domain/rpc/users/users.go`.

## Statut de la chaîne : `bagel.rpc.broadcaster.{status,live,stream_info}.get`

Recherches sur le chemin critique, servies par le projector. Groupe de files `projector-rpc`.
Délai du handler **1,5 s** (`app/projector/rpc/`).

Ordre de résolution de `status.get` : cache en processus (TTL 30 s), puis projection Valkey, puis
repli avec chargement différé via le RPC de projection users (résultat mis en cache). Une chaîne
inconnue ou invalide devient `standard` ; un échec de recherche donne aussi `standard`, avec une
chaîne `error`.

| Sujet | Requête | Réponse |
|---|---|---|
| `...status.get` | `{broadcaster_id}` | `{broadcaster_id, tier, banned, error?}` où `tier` vaut `premium` ou `standard` |
| `...live.get` | `{broadcaster_id}` | `{broadcaster_id, live, known, version?, error?}` |
| `...stream_info.get` | `{broadcaster_id}` | `{broadcaster_id, title, game_name, viewer_count, peak_viewers, started_at, ended_at, live, known, error?}` |

Le projector sert aussi le modèle de lecture du tableau de bord sous
`bagel.rpc.projector.dashboard.{commands,modules}.{get,replace}` (délai du handler 2 s).

## Projections internes : `bagel.rpc.internal.projection.*`

Vues read-through utilisées par le projector pour hydrater la projection des réglages.
Délai du handler **2 s**. `user_id` est une chaîne décimale.

| Sujet | Propriétaire / groupe de files | Réponse |
|---|---|---|
| `...projection.users.get` | users / `users-rpc` | `{user_id, status, is_active, banned, locale?, commands_page_hidden?, state_revision?, account_created_at?, error}` |
| `...projection.modules.get` | modules / `modules-rpc` | `{user_id, modules: [ModuleView], error}` |
| `...projection.commands.get` | commands / `commands-rpc` | `{user_id, commands: [CommandView], error}` |
| `...projection.commands.fetches.get` | commands / `commands-rpc` | définitions de fetch personnalisé de l'utilisateur |

Formes des vues (jeu complet de champs dans `internal/domain/rpc/projection/projection.go`) :

```json
CommandView { "name": "...", "aliases": [], "response": "...", "is_active": true, "perm": "everyone", "cooldown": 0 }
ModuleView  { "name": "...", "is_enabled": true, "configs": { /* JSON brut, omis si vide */ }, "revision": 0 }
```

## Jetons internes : `bagel.rpc.internal.tokens.*`

Cycle de vie des jetons utilisateur Twitch. Outgress lit le refresh token lors du renouvellement
et réécrit le jeton renouvelé afin qu'un redémarrage ne ressuscite jamais une valeur obsolète.
**Les jetons en clair transitent par ces sujets** ; l'autorisation NATS limite les abonnés possibles.
Propriétaire users, groupe de files `users-rpc`, délai du handler **2 s**.

| Verbe | Requête | Réponse |
|---|---|---|
| `get` | `{user_id}` | `{access_token, refresh_token, access_token_expires_at?, error, code}` |
| `save` | `{user_id, access_token, refresh_token, access_token_expires_at?}` | `{}` ou `{error, code}` |

## Gestion outgress : `bagel.rpc.outgress.*`

Contrôle opérateur de l'expéditeur et recherches Twitch. Groupe de files `outgress-rpc`.
Délai du handler **1,5 s** ; les recherches qui appellent Twitch (`followage`, `uptime`,
`streaminfo`, `channelcounts`) utilisent **4 s**, et `channel.get` utilise **6 s** pour qu'une vérification en direct tienne.
Chaque sujet tourne sur un pool de workers concurrent borné : un appel lent ne bloque jamais les autres.
Source : `app/twitch/outgress/rpc/manage.go`.

| Sujet | Requête | Réponse |
|---|---|---|
| `...channel.get` | `{broadcaster_id, verify?, repair?}` | `{channel, found, error, verification?}` ; `verify` liste les abonnements Twitch en direct de la chaîne (au plus une fois par 30 s et par chaîne), enregistre `ok` ou `failing`, et `repair` réinscrit en arrière-plan ceux qui manquent ; délai côté appelant **8 s** en vérification |
| `...channel.set` | `{broadcaster_id, enabled?, is_mod?}` | `{channel, found, error}` ; crée la chaîne si elle manque ; une surcharge `is_mod` compte comme vérification |
| `...channel.list` | `{}` | `{channels, error}` |
| `...system.status` | `{}` | `{paused, app_token_expires_in_seconds, has_user_token, error}` |
| `...system.pause` | `{paused:bool}` | `{paused, error}` (kill switch) |
| `...followage.get`, `...accountage.get`, `...uptime.get`, `...streaminfo.get`, `...channelcounts.get` | propre à la recherche | valeurs issues de Twitch pour les commandes et variables de chat |

Les récompenses de points de chaîne et les listes de chatteurs sont servies sous le même préfixe
(`app/twitch/outgress/rpc/channelpoints.go`, `chatters.go`). L'appel d'abonnement d'essai utilise
`bagel.rpc.outgress.trial_subscription` avec son propre groupe de files.

`enabled` et `is_mod` sont **facultatifs** (pointeur/omis) sur `channel.set` ; leur absence
signifie qu'ils restent inchangés. `channel` :

```json
{ "broadcaster_id": "...", "enabled": true, "is_mod": false,
  "mod_checked_at": "...", "updated_at": "...",
  "sub_state": "...", "sub_error": "", "sub_checked_at": "...",
  "blocked_at": "...", "grant_state": "", "grant_checked_at": "..." }
```

`verification` :

```json
{ "expected": 9, "present": 8, "missing": ["channel.cheer"],
  "repairing": false, "throttled": false, "error": "" }
```

## Instantané des shards ingress : `twitch.ingress.admin.shards.get`

Vue en direct du parc de shards EventSub, servie par n'importe quelle réplique ingress (Elixir).
Délai de l'appelant **5 s** (ingress limite le travail par shard à ~2 s). Le corps de la requête est vide.

Le même service répond aussi à `twitch.ingress.admin.shards.scale`,
`twitch.ingress.admin.shards.autoscale`, `twitch.ingress.admin.trials.{list,add,remove,set_enabled}`
et `bagel.rpc.ingress.conduit.get` (`app/twitch/ingress/config/runtime.exs`,
`app/twitch/ingress/lib/ingress/application.ex`).

Réponse (`Snapshot`), abrégée. Les autres champs de premier niveau sont `capacity`,
`desired_count`, `target`, `min_shards`, `max_shards`, `autoscale`, `max_load`,
`max_load_shard_id`, `trial_loads`, `trial_burst_loads` et `trial_sockets`
(`app/twitch/ingress/lib/ingress/admin_rpc.ex`) :

```json
{
  "generated_at": "2026-06-15T00:00:00Z",
  "reporter": "ingress-node1",
  "nodes": ["node1", "node2"],
  "shard_count": 2,
  "conduit_manager": { "state": "...", "node": "...", "conduit_id": "..." },
  "inventory": "available",
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
`backoff`, `unregistered`, `unresponsive`, `unknown`. `conduit_manager` décrit le reconciler du conduit,
singleton du cluster. `inventory` vaut `unavailable` quand la liste des enfants du superviseur
a expiré ; chaque shard sans autre information est alors `unknown`.

## Invalidation du cache : `bagel.cache.invalidate.<scope>`

Ce n'est pas un request-reply : il s'agit d'une publication fire-and-forget. Le suffixe du sujet est
la portée, par exemple `status`, `grant`, `modules`, `delegation`, `notifications` ou la portée de
l'état en direct. Les propriétaires publient après tout changement affectant l'état mis en cache
d'une chaîne (statut, activation, octroi ou suppression de jeton, écritures de projection).
Corps : `{broadcaster_id, keys?}` (`broadcaster_id` est une chaîne décimale,
`internal/domain/invalidate/invalidate.go`). Les abonnés (projector, ingress, sesame et autres)
abandonnent leur vue en cache et la rechargent avec chargement différé.
