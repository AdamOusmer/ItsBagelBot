---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Constructeur de modules Sesame
description: L'API fluide de création des modules et commandes Sesame, avec chaque méthode du constructeur et l'ordre des contrôles appliqué par le moteur.
---

Une fonctionnalité Sesame est une fonction qui renvoie une valeur `module.Module`.
Le constructeur de `app/twitch/sesame/module/builder.go` assemble cette valeur ;
le moteur de `app/twitch/sesame/engine/` l'indexe et l'exécute.

Le paquet du constructeur ne contient **aucun câblage d'exécution** (ni NATS,
ni Valkey, ni projection ni pipeline). Un module reçoit `engine.Deps` et ses
handlers capturent leurs dépendances par fermeture ; l'API reste ainsi compacte
et testable unitairement sans cluster.

## Module minimal

```go
func Ping(d engine.Deps) module.Module {
    m := module.NewModule("", module.KindCore)
    m.Command("ping").Everyone().Run(
        func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
            emit(&module.Output{
                Type:          outgress.TypeChat,
                BroadcasterID: c.Env.BroadcasterUserID,
                Text:          "pong",
            })
            return nil
        })
    return m.Build()
}
```

Ajoutez ensuite une ligne dans `app/twitch/sesame/modules/all.go`. C'est toute
l'étape d'enregistrement.

## API au niveau du module

| Appel | Rôle |
|---|---|
| `module.NewModule(name, kind)` | Démarre un module. Renvoie `*Builder`. |
| `b.Command(name)` | Commence une commande de chat. Renvoie `*CmdBuilder` pour enchaîner les contrôles. Le nom est converti en minuscules. |
| `b.On(eventType, fn)` | Enregistre un handler hors commande pour un type EventSub. Si le même type est enregistré deux fois, le dernier handler est conservé. |
| `b.Build()` | Valide et renvoie le `Module` immuable. Provoque une **panique** en cas d'erreur de programmation. |
| `b.Validate()` | Effectue les mêmes contrôles sans provoquer de panique. Utile pour les tests. |

Un `Builder` ne s'utilise qu'une fois et n'est pas sûr en cas d'accès concurrent.

## Types

| Type | Nom | Comportement |
|---|---|---|
| `KindCore` | facultatif | Toujours actif. Jamais listé pour les chaînes, jamais activé ou désactivé, sans configuration. Le moteur ignore entièrement pour lui la lecture de projection `ModuleView`. |
| `KindDefault` | obligatoire | Nommé, livré **activé**. S'exécute sauf si la chaîne le désactive. |
| `KindOptIn` | obligatoire | Nommé, livré **désactivé**. S'exécute uniquement si la chaîne l'active. |

`Build` impose cette association : un type nommé avec un nom vide échoue.

Le fait que Core ignore la récupération de projection est une propriété du chemin critique.
Un module nommé coûte une recherche `ModuleView` par message pour décider s'il s'exécute ;
Core ne coûte rien. Un module Core qui souhaite malgré tout un contrôle par chaîne effectue
sa propre vérification différée dans `Run` (voir `moduleEnabled` dans `modules/followage.go`).

## Contrôles des commandes

Chaque méthode ci-dessous renvoie le même `*CmdBuilder`, donc elles s'enchaînent. `Run` est
terminale et ne renvoie rien.

### Permission

Chaque méthode définit le rôle **minimal** ; les rôles supérieurs passent également.

| Méthode | Rôle minimal |
|---|---|
| `.Everyone()` | toute personne du chat (par défaut) |
| `.Sub()` | abonné |
| `.VIP()` | VIP |
| `.Mod()` | modérateur |
| `.Broadcaster()` | propriétaire de la chaîne uniquement |

Ordre de la hiérarchie : `RoleEveryone` → `RoleSubscriber` → `RoleVIP` → `RoleModerator` →
`RoleLeadModerator` → `RoleBroadcaster`.

**Il n'existe pas de méthode `.LeadMod()` sur le constructeur**, même si
`RoleLeadModerator` figure dans la hiérarchie. Cette entrée sert à l'*inclusion*,
pas au contrôle : Twitch fournit le badge `lead_moderator` **à la place** de
`moderator`, donc sans rang supérieur à moderator chaque lead mod échouerait au
contrôle `.Mod()` faute de badge reconnu. `.Mod()` inclut donc déjà les lead
moderators et les rôles supérieurs.

Ce niveau est également pris en charge partout : `permRoles` associe `"lead_mod"`,
`internal/domain/validate` l'accepte, le schéma ent de `commands` le stocke et le
tableau de bord propose « Lead moderators & up ». Une commande **personnalisée**
écrite par une chaîne peut donc être réservée aux lead mods (le moteur contrôle
les commandes intégrées et personnalisées via le même `gate()`), alors qu'un
module **intégré** ne peut actuellement pas exprimer « lead mods et supérieurs,
à l'exclusion des modérateurs simples ». Ajouter `.LeadMod()` ne demanderait
qu'un setter d'une ligne dans `CmdBuilder` si un module intégré en avait besoin.

| Méthode | Rôle |
|---|---|
| `.AllowUser(id)` | Limite à un seul identifiant de chatter. **Remplace entièrement le contrôle de rôle.** |

### Conditions

| Méthode | Rôle |
|---|---|
| `.Cooldown(d)` | Fenêtre partagée par commande. Zéro (par défaut) signifie aucune fenêtre. |
| `.LiveOnly()` | S'exécute uniquement lorsque la chaîne est en direct. |

### Correspondance

| Méthode | Rôle |
|---|---|
| `.Aliases(a, b, ...)` | Déclencheurs supplémentaires qui résolvent vers cette commande. Convertis en minuscules. Les doublons sont rejetés par `Validate`. |
| `.NumericSuffix()` | Le déclencheur absorbe les chiffres finaux saisis directement : `!clip30` résout `clip` avec `c.Num == "30"`. Les chiffres sont retirés et ne font **pas** partie de la chaîne d'arguments. |

### Terminaison

| Méthode | Rôle |
|---|---|
| `.Run(fn)` | Définit le handler et termine la commande. Ne renvoie rien, afin qu'une déclaration ne puisse pas continuer après lui. |

## Ordre des contrôles

Le moteur applique les contrôles de manière centralisée dans `engine/dispatch.go` :

```go
func (p *Pipeline) gate(ctx, c, r gateRule) (bool, error) {
    if !permits(c, r.allowedUserID, r.perm) { return false, nil }          // 1
    if ok, err := p.liveOK(ctx, c, r.liveOnly); !ok { return false, err }  // 2
    return p.cooldownOK(ctx, c.BroadcasterID, r.name, r.cooldown)          // 3
}
```

1. permission
2. uniquement en direct
3. cooldown

**Le cooldown est volontairement en dernier.** `cooldownOK` *réserve* la fenêtre lorsqu'il réussit.
S'il s'exécutait en premier, un chatter qui échoue au contrôle de permission consommerait quand même
le cooldown de toutes les autres personnes.

Les commandes intégrées et les commandes personnalisées écrites par la chaîne construisent la même
`gateRule`, afin que la sémantique des permissions reste identique entre les
commandes intégrées et les commandes utilisateur.

## Ce que reçoit `Run`

```go
type RunFunc func(ctx context.Context, c *Context, args string, emit Emit) error
```

- `args` est la chaîne nettoyée **après** le nom de la commande.
- `c.Env` est l'enveloppe de chat : `BroadcasterUserID`, `ChatterUserID`,
  `ChatterUserLogin`, `ChatterName()`, badges.
- `c.Chatter()` résout le rôle du chatter, analysé une fois puis mis en cache.
- `c.Locale` est la langue de la chaîne. Utilisez `i18n.T(c.Locale, "key")`.
- `c.Config` est le blob de configuration brut du module ; `c.Decode(&out)` le
  désérialise. Il est vide pour les modules Core et une configuration absente
  n'est pas une erreur.
- `c.Num` est le suffixe numérique inline, défini uniquement pour une commande
  `NumericSuffix`.

### Règles de mutualisation

Ces deux règles corrompront d'autres messages si elles sont ignorées.

- **Ne conservez pas `c` après l'appel.** `Context` est mutualisé et réinitialisé
  entre les messages.
- **Ne conservez pas le `Output` transmis à `emit`.** Le moteur peut le recycler
  dès le retour de `Emit`.

Copiez tout ce qui doit survivre à l'appel.

## Émission

```go
emit(&module.Output{
    Type:          outgress.TypeChat,
    BroadcasterID: c.Env.BroadcasterUserID,
    Text:          "hello",
})
```

`Output.Type` sélectionne l'action (`outgress.TypeChat`, `TypeAnnounce`, clip,
shoutout, modération, résolution de récompense). Les champs propres à chaque type
sont documentés sur la structure de `module/module.go` ; les champs des autres
types sont ignorés. `BatchID`/`Items` transportent une réponse à plusieurs
messages comme un seul travail de file outgress, exécuté dans l'ordre de la slice.

## Validation

`Build` provoque une panique au lieu de renvoyer une erreur, car il s'agit de
mauvaises configurations au démarrage plutôt que de données d'exécution, et un
échec explicite au boot vaut mieux qu'un comportement silencieusement incorrect
en production. Il rejette :

- un type nommé avec un nom vide
- un type inconnu
- un nom de commande ou un alias vide
- un déclencheur dupliqué **dans le même module**
- une commande sans `Run` (signalée par « chain .Run to finish it »)

Une commande que vous oubliez de terminer existe tout de même comme entrée
incomplète, car `Command()` l'ajoute avant tout chaînage ; c'est précisément ainsi
que `Validate` la détecte.

## Collisions entre modules

Les doublons *dans* un module provoquent une panique lors de la construction. Les doublons *entre* modules sont
**le premier gagne**, avec un avertissement dans les logs
(`engine: duplicate command trigger ignored`).

L'ordre dans `all.go` est donc déterminant : les modules Core sont listés en premier afin que
leurs déclencheurs réservés l'emportent sur ceux d'un module nommé en conflit.

## Liste de contrôle pour un nouveau module

1. Créez `app/twitch/sesame/modules/<name>.go`.
2. Déclarez `func <Name>(d engine.Deps) module.Module`.
3. Appelez `module.NewModule(name, kind)`.
4. Déclarez les commandes avec `.Command(...).<gates>.Run(fn)` et les événements avec
   `.On(type, fn)`.
5. Écrivez `return m.Build()`.
6. Ajoutez une ligne dans `all.go`.
7. Localisez les réponses avec `i18n.T(c.Locale, key)`.
8. Protégez les services facultatifs (`d.X != nil`) et choisissez délibérément
   la politique d'échec ; les modules existants échouent **ouvertement**, afin
   qu'un incident temporaire de projection n'absorbe pas une commande.
9. Ajoutez `<name>_test.go`. `Validate()` s'exécute sans cluster.
