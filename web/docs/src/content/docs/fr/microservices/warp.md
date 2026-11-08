---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: WARP
description: "Construit l’image du client utilisée comme proxy HTTP non fiable par Gossip. Ce répertoire contient une image, pas un service Go."
docType: reference
---

## Responsabilités

Gossip valide les destinations et transmet les requêtes non fiables au proxy. Le déploiement démarre le démon en mode proxy ; une route indisponible ne doit pas provoquer de repli silencieux vers une sortie directe.

## Interfaces

Le Containerfile installe le client du fournisseur sans définir la commande d’exécution. Le déploiement configure le démon et le port SOCKS local. Un port ouvert ne prouve pas le bon fonctionnement du tunnel externe ; la validation des destinations par Gossip reste nécessaire.

## État et dépendances

Le déploiement démarre le démon du fournisseur ; l’image ne définit pas de commande.

## Comportement en cas d’échec

Une route non fiable indisponible ne doit pas entraîner de repli silencieux vers une sortie directe. Un port ouvert ne prouve pas la santé du tunnel.

## Code

Source : [app/warp/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/warp). Voir `Containerfile`, `app/gossip/internal/core/http.go` et `deploy/k8s/gossip.yaml`.

## Validation

Depuis la racine, exécuter `go test ./app/gossip/internal/core ./deploy/k8s` pour les vérifications du code associé. La construction de l’image et la vérification du tunnel exigent un environnement de conteneurs.
