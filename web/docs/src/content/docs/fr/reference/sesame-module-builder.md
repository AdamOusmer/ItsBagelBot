---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Constructeur de modules Sesame
description: "Définir et enregistrer les modules et commandes Sesame."
docType: how-to
---

## Résultat et prérequis

Le constructeur dans `app/twitch/sesame/module/builder.go` crée un `module.Module`; le moteur indexe et exécute le résultat. Les constructeurs de modules sont enregistrés dans `app/twitch/sesame/modules/all.go`. Les modules existants dans ce dossier montrent le câblage des dépendances et les gestionnaires.

## Déclarer un module

```go
m := module.NewModule("example", module.KindOptIn)
m.Command("hello").Everyone().Run(handler)
return m.Build()
```

Ici `handler` doit respecter `module.RunFunc`. `Command` met le nom en minuscules; `Run` termine sa définition. Un module peut aussi enregistrer un gestionnaire d'événement avec `On`. `KindCore` sert au comportement de base, `KindDefault` aux modules nommés activés par défaut et `KindOptIn` aux modules nommés activés explicitement. Les types nommés exigent un nom.

Les constructeurs de commandes offrent notamment les rôles (`Everyone`, `Sub`, `VIP`, `Mod`, `LeadMod`, `Broadcaster`), `Cooldown`, `LiveOnly`, `Aliases`, `When` et `AllowUser`. Le constructeur enregistre ces déclarations; le moteur les applique. Consultez le moteur et les tests des modules avant de dépendre d'un ordre précis ou d'ajouter un contrôle.

`Validate` renvoie une erreur pour une déclaration invalide, notamment des déclencheurs en double ou un gestionnaire absent. `Build` l'appelle et provoque un panic en cas d'erreur, ce qui fait échouer le démarrage. Un module trial doit être core et ne peut pas être beta; un module core ne peut pas être beta. Consultez le constructeur et ses tests pour toutes les règles, puis lancez les tests ciblés du module et du moteur.

## Ajouter un module

1. Implémentez un constructeur à côté des [modules existants](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/twitch/sesame/modules/). Choisissez un type et capturez uniquement les dépendances nécessaires aux gestionnaires depuis `engine.Deps`.
2. Déclarez commandes et gestionnaires d'événements avec le [constructeur](https://github.com/AdamOusmer/ItsBagelBot/blob/main/app/twitch/sesame/module/builder.go). Chaque commande doit avoir un gestionnaire `Run` et des noms et alias uniques dans le module. Utilisez `Validate` dans les tests ciblés des déclarations invalides.
3. Enregistrez le constructeur dans [`modules.All`](https://github.com/AdamOusmer/ItsBagelBot/blob/main/app/twitch/sesame/modules/all.go). Pour un module configurable, suivez sa configuration et son activation dans le service modules et la projection; l'enregistrement seul ne crée pas ces données.
4. Testez les sorties et erreurs du gestionnaire, les contrôles du moteur et les changements de variables publiques. Lancez `go test ./app/twitch/sesame/module/... ./app/twitch/sesame/engine/...` ainsi que les tests du module modifié.

`Build` copie la liste des commandes et la table des événements dans la valeur renvoyée; le constructeur ne réalise pas le câblage d'exécution. L'ordre réel des contrôles dépend du code et des tests du [moteur](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/twitch/sesame/engine/), pas de l'ordre de présentation des méthodes ici.

## Vérifier et résoudre les problèmes

Les tests ciblés réussis se terminent avec un code zéro. Si `Build` provoque un panic au démarrage, appelez `Validate` dans un test et examinez l'erreur : une commande peut manquer de gestionnaire, un alias peut dupliquer un déclencheur ou le type et les options du module peuvent être incompatibles. Si une commande enregistrée ne s'exécute pas, vérifiez les contrôles du moteur et l'état du module pour la chaîne avant de modifier le gestionnaire.

Voir [Variables des modules](/fr/reference/module-variables/) pour exposer des valeurs publiques et [Tests](/fr/guides/testing/) pour les vérifications plus larges.
