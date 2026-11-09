---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Configuration à l’exécution
description: Comment les services démarrent, chargent leur configuration, se connectent aux dépendances et signalent leur état.
docType: reference
---

Le déploiement fournit les adresses et les identifiants. Le code détermine leur utilisation. Consultez le point d’entrée du service et son chargeur de configuration avant de copier une variable d’environnement d’un autre service.

Pour les interfaces réseau des hôtes, les files, l’affinité des interruptions, les offloads et leur persistance sur node1–3, voir les [réglages réseau des workers](/fr/infrastructure/node-network-tuning/).

## Démarrage d’un service Go

`pkg/svcboot` gère la journalisation, la surveillance, l’annulation par signal et les constructeurs de dépendances partagées. `NewCore` renvoie le contexte du service et la fonction de nettoyage. Les services de données utilisent `pkg/svcboot/databoot` pour leur pilote Ent. Le test de garde `pkg/svcboot/guard_test.go` vérifie que les points d’entrée des services passent par cette interface.

```mermaid
flowchart LR
    Main["Main du service"] --> Core["svcboot.NewCore"]
    Core --> Config["Charger la configuration"]
    Config --> Clients["Construire les dépendances partagées"]
    Clients --> Handlers["Enregistrer les gestionnaires RPC et d’événements"]
    Handlers --> Health["Exposer les vérifications de santé"]
    Health --> Wait["Attendre l’annulation et nettoyer"]
```

Les applications Elixir ont leurs propres arbres de supervision. Consultez `app/twitch/ingress/lib/ingress/application.ex` et `app/controls/gateway/lib` pour ces chemins.

## Valeurs par défaut partagées

`pkg/svcboot/infra.go` charge ces valeurs. Un service peut ajouter ou remplacer des paramètres dans son propre chargeur.

| Variable | Valeur par défaut | Signification |
| --- | --- | --- |
| `NATS_URL` | `nats://127.0.0.1:4222` | Adresse de connexion principale au bus |
| `NATS_RPC_URL` | `NATS_URL` | Adresse de connexion RPC |
| `VALKEY_ADDR` | `127.0.0.1:6379` | Adresse Valkey |
| `VALKEY_PASSWORD` | Vide | Paramètre d’authentification Valkey |
| `LISTEN_ADDR` | `:8080` | Adresse d’écoute HTTP du service |

Ce sont les valeurs par défaut du chargeur, pas un environnement local complet. Elles ne créent ni flux, ni schémas, ni comptes, ni autorisations de plateforme, ni secrets.

`pkg/env` traite les valeurs vides comme absentes. Les accesseurs typés utilisent leur valeur de repli si l’analyse échoue; `MustGet` provoque une panique si une valeur obligatoire manque. Vérifiez quel assistant utilise le service avant de compter sur le démarrage pour rejeter une faute de frappe.

## Connexions et identifiants

`pkg/bus/connect.go` sépare les identifiants RPC des identifiants généraux du bus. `NATS_RPC_JWT` et `NATS_RPC_NKEY_SEED` utilisent `NATS_JWT` et `NATS_NKEY_SEED` comme valeurs de repli. La configuration TLS lit `NATS_CA_PEM` et les paramètres de certificat et de clé client par `pkg/tlsenv`. Le routage des publications peut aussi utiliser des adresses propres au hub. Une adresse leaf RPC n’est pas automatiquement la bonne adresse pour un producteur JetStream.

La connexion Valkey, TLS, les lectures sur réplica et le routage des écritures relèvent de `pkg/valkey`. La connexion et le pool de base de données relèvent de `pkg/db`. Le code des services utilise ces packages au lieu de créer d’autres clients bruts.

Ne placez pas de secrets dans les exemples, les journaux ou les réglages projetés. Les noms et responsabilités de configuration ont leur place dans la documentation; les identifiants de déploiement appartiennent au stockage de secrets.

## Santé et arrêt

`pkg/svcboot/health.go` construit un ensemble de vérifications comprenant NATS, ajoute les vérifications du service et enregistre la santé RPC. Les vérifications fournies par chaque fonction main définissent ce que signifie un service sain. Un port HTTP à l’écoute ne prouve pas à lui seul qu’un consommateur ou une base de données est prêt.

Lors d’une modification du démarrage, vérifiez aussi l’ordre du nettoyage : cesser d’accepter du travail, annuler les boucles en arrière-plan, vider ou fermer les clients concernés et laisser le nettoyage enregistré par le service se terminer. Suivez la fonction main existante plutôt que de supposer que chaque processus possède les mêmes dépendances.

## Sources

- [Démarrage partagé](https://github.com/AdamOusmer/ItsBagelBot/tree/main/pkg/svcboot)
- [Assistants d’environnement](https://github.com/AdamOusmer/ItsBagelBot/tree/main/pkg/env)
- [Connexions au bus](https://github.com/AdamOusmer/ItsBagelBot/blob/main/pkg/bus/connect.go)
- [Vérifications de santé](https://github.com/AdamOusmer/ItsBagelBot/tree/main/pkg/health)

Poursuivez avec [prise en main](/fr/guides/getting-started/) et [contrats RPC](/fr/reference/rpc-contracts/).

## Cycle de vie de la configuration

[LoadInfra](https://github.com/AdamOusmer/ItsBagelBot/blob/main/pkg/svcboot/infra.go) lit ces valeurs lorsque le service construit sa configuration d'infrastructure. Ce sont des entrées du processus, pas une API de configuration dynamique; redémarrez ou redéployez le service pour prendre en compte une modification de l'environnement. Certains services lisent d'autres variables ou utilisent d'autres constructeurs. Consultez leur point d'entrée avant de supposer qu'ils utilisent une valeur.

Pour les valeurs communes indiquées, une valeur vide suit le repli de [`env.Get`](https://github.com/AdamOusmer/ItsBagelBot/tree/main/pkg/env). Les échecs de connexion et de santé dépendent du câblage du service; une adresse par défaut ne garantit pas qu'une dépendance fonctionne.
