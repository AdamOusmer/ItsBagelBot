---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Variables de modules dans les commandes personnalisées
description: Lire les valeurs publiques des modules dans les réponses des commandes personnalisées avec la syntaxe module:variable et les exigences d'activation des modules.
---

Les commandes personnalisées peuvent lire les valeurs publiques des modules avec
`{module:variable}`. Le séparateur est le deux-points. Ces valeurs proviennent des
mêmes champs publics que les réponses du module ; elles n'exposent ni sa
configuration privée ni ses identifiants d'accès.

## Créer une commande de rang Valorant

1. Ouvrez **Modules → Stats Valorant**, activez le module et configurez le compte
   Riot et la région qu'il demande.
2. Créez une commande personnalisée nommée `rank` dans **Commandes**.
3. Définissez sa réponse sur `Mon rang Valorant est {valorant:tier} ({valorant:rr} RR).`
4. Enregistrez la commande. Les spectateurs peuvent maintenant saisir `!rank`.

Le module Valorant doit être activé. Créer une commande ou insérer ses variables
n'active pas le module. Cette exigence s'applique à chaque espace de noms de
module. Les modules activés par défaut conservent leur comportement habituel ;
les modules facultatifs doivent être activés. Les restrictions Premium ou bêta
continuent de s'appliquer. Gamble et Duels exigent également que leur module
parent **Points de fidélité** soit activé.

Si le module est désactivé ou indisponible, la variable reste visible dans la
réponse et le bot ne récupère pas les données de ce module. Une valeur de secours
ne contourne pas ce contrôle. Pour un module activé, une valeur de secours comme
`{valorant:tier|rang indisponible}` fournit du texte lorsque la valeur demandée
est vide.

Le sélecteur **Variables de modules** de l'éditeur présente les syntaxes
disponibles et indique quel module doit être activé. L'aperçu utilise des
valeurs d'exemple ; il ne vérifie ni l'état du module sur la chaîne ni le compte
connecté.

Les minuteurs peuvent également lire les variables des modules. Aucun spectateur
ne les déclenche, donc les champs propres à un spectateur sont vides. Configurez
explicitement un compte pour les fournisseurs qui le déduisent normalement de
l'identifiant du diffuseur ; les minuteurs ne transmettent pas cet identifiant.
Les exigences Premium et d'activation des modules continuent de s'appliquer.

## Autres espaces de noms de modules

| Espace de noms | Exemple | Module |
| --- | --- | --- |
| `accountage` | `{accountage:accountage}` | Âge du compte |
| `alerts` | `{alerts:user}` | Alertes de chat |
| `channelpoints` | `{channelpoints:reward}` | Points de chaîne |
| `clashroyale` | `{clashroyale:trophies}` | Stats Clash Royale |
| `clip` | `{clip:clip}` | Clip |
| `codm` | `{codm:rank}` | Profil CODM |
| `commercial` | `{commercial:length}` | Publicité |
| `duel` | `{duel:winner}` | Duels |
| `followage` | `{followage:followage}` | Ancienneté du suivi |
| `fortnite` | `{fortnite:wins}` | Stats Fortnite |
| `gamble` | `{gamble:points}` | Gamble |
| `game` | `{game:game}` | Jeu |
| `govee` | `{govee:color}` | Lumières Govee |
| `loyalty` | `{loyalty:points}` | Points de fidélité |
| `marker` | `{marker:user}` | Marqueur |
| `mcsr` | `{mcsr:elo}` | MCSR Ranked |
| `personality` | `{personality:user}` | Personnalité du bot |
| `queue` | `{queue:size}` | File d'attente |
| `quotes` | `{quotes:text}` | Citations |
| `raffle` | `{raffle:entrants}` | Tirage au sort |
| `shoutout` | `{shoutout:raider}` | Shoutout automatique |
| `songqueue` | `{songqueue:title}` | Demandes de chansons |
| `stream` | `{stream:uptime}` | Gestion du direct |
| `tags` | `{tags:tags}` | Tags |
| `time` | `{time:time}` | Heure locale |
| `title` | `{title:title}` | Titre |
| `triggers` | `{triggers:user}` | Mots déclencheurs |
| `uptime` | `{uptime:uptime}` | Durée du direct |
| `urchin` | `{urchin:wins}` | Stats Bedwars |
| `valorant` | `{valorant:tier}` | Stats Valorant |

Seules les variables publiées sont disponibles. Un module sans champs publics de
réponse ni valeurs de commande lisibles n'a aucune variable à insérer.

Lorsqu'un champ existe dans plusieurs vues de réponse, la syntaxe courte lit la
première vue publiée. Utilisez `{module:view:variable}` pour choisir une vue
précise, par exemple `{valorant:rank:tier}` pour la vue du rang. Le sélecteur
présente les deux formes. Les variables de commande existantes sans espace de
noms conservent leur syntaxe. Les éditeurs de réponses de modules affichent les
champs avec leur espace de noms et convertissent les anciens champs reconnus
lors de l'enregistrement.

Certains champs décrivent un événement, comme l'auteur d'un raid ou la saisie
lors de l'échange d'une récompense. Une commande personnalisée ordinaire n'a pas
d'événement correspondant, donc ces champs sont vides ; elle ne récupère pas un
événement précédent et ne déclenche pas d'échange. Lire les valeurs de jeu, de
file d'attente, de tirage au sort ou de fidélité n'exécute ni la commande du module
ni ses actions. Les recherches de session conservent le comportement habituel
du fournisseur pour leur point de référence. Consultez la référence des variables
et le sélecteur pour voir tous les champs disponibles.

## Modèles de modules enregistrés

Les éditeurs de modules affichent les anciens champs enregistrés avec la nouvelle
syntaxe d'espace de noms. Par exemple, une réponse Valorant enregistrée contenant
`{tier|unranked}` s'ouvre sous la forme `{valorant:tier|unranked}`. La modification
est enregistrée avec la réponse éditée ; ouvrir simplement un éditeur ne modifie
pas la configuration stockée. Les variables inconnues, les variables dynamiques,
le texte des valeurs de secours et les branches des conditions sont préservés.
Les modèles utilisant déjà les espaces de noms restent inchangés. Pendant la
transition, le moteur accepte les anciens champs de modules.
