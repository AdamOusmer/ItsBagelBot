---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Valider la livraison Twitch
description: Vérifier ensemble l’admission Outgress, la pause, les reprises et les effets HTTP.
docType: how-to
---

Outgress transforme les actions en file en effets externes. Vérifiez tout le chemin de décision avant d’affirmer qu’une optimisation locale améliore la livraison.

## Prérequis

Lisez les prérequis des tests d’intégration et choisissez le code et la charge à valider. Utilisez un vrai Valkey pour vérifier l’atomicité du script; un substitut ne peut pas la démontrer. Examinez ces périmètres :

| Sujet | Source |
| --- | --- |
| Distribution et politique des actions | `app/twitch/outgress/internal/worker` |
| État des chaînes et instantanés de pause | `app/twitch/outgress/internal/channels` |
| Admission ordonnée | `pkg/ratelimit/valkey.go` |
| Traitement des requêtes et réponses HTTP | `app/twitch/outgress/internal/twitch` |
| Acquittement, reprise et nack attendu | `pkg/bus` |

## Procédure

1. Vérifiez qu’un refus de la voie standard respecte les règles de capacité partagée et qu’un refus du second bucket préserve la consommation du premier documentée par le limiteur ordonné.
2. Testez la propagation des pauses entre instances et la récupération après une invalidation manquée. Exercez les données périmées et le démarrage avant disponibilité ainsi que les lectures sur cache chaud.
3. Couvrez les succès HTTP, les limites de débit, les échecs d’autorisation, les annulations et la lecture bornée des réponses.
4. Testez l’expiration et les nouvelles livraisons. Distinguez les effets externes ambigus des opérations pouvant être reprises sans risque.
5. Exécutez la suite ciblée depuis la racine du dépôt :

   ```sh
   go test -race ./app/twitch/outgress/... ./pkg/ratelimit/... ./pkg/bus/...
   ```

## Vérifier et consigner

Confirmez que les tests pertinents ont réussi et consignez leur périmètre et leur environnement. Pour comparer des déploiements, conservez le commit, la charge, la répartition des voies, l’état chaud ou froid, les nombres d’erreurs et de reprises, la distribution des latences et les observations sur la propagation des pauses. Voir les [preuves de validation](/fr/qa/) et les [travaux de performance](/fr/infrastructure/performance/).

## Limites et dépannage

Un instantané local ne démontre pas le comportement de toute la flotte. Des connexions locales réutilisées ne démontrent ni le protocole de connexion ni la latence de Twitch. Un acquittement NATS ne prouve pas qu’un effet a eu lieu exactement une fois sur la plateforme. Si une vérification d’intégration ne peut joindre ses dépendances, consignez cette limite au lieu de la considérer comme réussie. Ce guide ne décrit pas un canari de production achevé. Voir [Outgress](/fr/microservices/outgress/) pour ses responsabilités.
