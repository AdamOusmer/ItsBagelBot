---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0002 - Adoption de Go comme langage principal des services"
description: "Décision d'architecture : adoption de Go comme langage principal des services"
---

**Date:** 2026-01-04

## Statut

Acceptée

## Contexte

Les précédentes itérations ont été réalisées avec Python. Ce langage avait été
choisi pour sa facilité de développement et sa grande communauté, qui offre de
nombreuses bibliothèques et outils pour accélérer le développement. Cependant,
rester sur Python contredirait notre [décision de réécrire le système en
microservices](/fr/adr/0001-rewriting-to-microservices/), car les bibliothèques
actuelles ne sont pas prêtes à gérer des systèmes de production.

De plus, ce langage n'est pas prêt à assurer un débit élevé dans un
environnement peu puissant à cause de son verrou global de l'interpréteur
(GIL). Python 3.13 a introduit une compilation expérimentale sans verrou par
thread, et les versions suivantes l'ont affinée jusqu'à la version actuelle
(3.14), mais l'environnement d'exécution n'est pas encore mature et les
bibliothèques ne sont ni suffisamment prêtes ni suffisamment stables pour la
production.

On pourrait soutenir que le GIL ne pose pas vraiment de problème de débit sur
le matériel actuel, mais nous voulons évoluer à coût faible, voire nul. Dans
ce contexte, le modèle de concurrence limité de Python devient rapidement un
goulot d'étranglement.

En outre, la taille des images Python et les temps de démarrage sont
importants. Résoudre les dépendances wheel à chaque compilation Docker et
réindexer à chaque démarrage coûte beaucoup plus de ressources de calcul que
les solutions proposées.

Nos exigences sont les suivantes :

- Débit élevé dans des environnements consommant peu de ressources.
- Modèle de concurrence robuste.
- Maturité et stabilité des outils disponibles pour les websockets/webhooks.
- Petite taille d'image et démarrage rapide.

## Décision

Au vu des exigences ci-dessus, Go est le langage choisi. Il satisfait chacune
des exigences établies.

- Débit élevé dans des environnements consommant peu de ressources : Go se
  compile en un binaire statique unique qui s'exécute sans machine virtuelle,
  et son environnement d'exécution reste léger sous charge.
- Modèle de concurrence : les goroutines et les channels rendent le code
  concurrent peu coûteux et idiomatique, sans GIL à se disputer.
- Maturité pour les websockets/webhooks : `net/http` est prêt pour la
  production dès l'installation, et `gorilla/websocket` couvre nos besoins de
  streaming et d'événements.
- Taille de l'image et temps de démarrage : un binaire Go sur une base
  `scratch` ou `distroless` fait généralement 10 à 20 Mo et démarre en
  quelques millisecondes.

## Conséquences

- La courbe d'apprentissage du langage est réelle et doit être prise en compte.
- Le coût de la concurrence est suffisamment faible pour être presque
  négligeable dans nos cas d'utilisation.
- La gestion de plusieurs services et le partage d'état demanderont une
  coordination via des services supplémentaires ou un stockage clé-valeur,
  avec des conventions strictes à respecter pendant le développement.
- La gestion des erreurs est verbeuse : chaque appel susceptible d'échouer
  renvoie une erreur explicite, ce qui ajoute du code répétitif mais force à
  envisager les chemins d'échec dès le départ.
- Le vivier de personnes connaissant Go est plus réduit que celui de Python,
  ce qui rend l'intégration des futures contributions plus exigeante.
- Certains SDK Twitch et Discord sont conçus d'abord pour Python ; nous
  pourrions donc devoir encapsuler nous-mêmes des API tierces là où la
  communauté Go n'a pas encore rattrapé son retard.

## Alternatives envisagées

- Python sans verrou ([PEP 703](https://peps.python.org/pep-0703/), la
  compilation sans GIL) était une bonne candidate, mais la maturité actuelle
  de l'environnement d'exécution ferait passer plus de temps à le combattre
  qu'à l'utiliser.
- L'empreinte de Java est lourde pour notre matériel peu coûteux.
  [GraalVM native-image](https://www.graalvm.org/latest/reference-manual/native-image/)
  réduit considérablement le démarrage à froid et la mémoire, mais la
  complexité du pipeline de compilation ne correspond pas à la taille de
  l'équipe.
- Rust nous donnerait un débit comparable et des binaires encore plus petits,
  mais sa courbe d'apprentissage est absolument incompatible avec le contexte
  actuel.
- Node.js avec TypeScript possède un riche écosystème Twitch/Discord, mais la
  boucle d'événements mono-thread et l'empreinte mémoire plus élevée par
  connexion diminuent l'avantage de débit que nous cherchons à optimiser.
- C++ offrirait un coût de calcul extrêmement faible, mais le temps de
  développement ferait entrer une équipe d'une seule personne dans une zone
  d'échec à haut risque.
