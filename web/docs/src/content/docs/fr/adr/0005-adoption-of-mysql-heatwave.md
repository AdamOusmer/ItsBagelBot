---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0005 - Adoption de MySQL HeatWave"
description: "Décision d'architecture : adoption de MySQL HeatWave comme base de données relationnelle"
---

**Date:** 2026-05-23

## Statut

Acceptée

## Contexte

Le système a besoin d'une base de données relationnelle pour les éléments d'état
qui n'ont pas leur place dans un stockage clé-valeur : fiches utilisateur,
configuration des tenants, jetons OAuth, définitions de commandes, et tout ce
pour quoi les lignes, clés étrangères et requêtes ad hoc sont naturelles. Le
plan précédent pour ce rôle était PostgreSQL, choix par défaut de la v1.

Sur le papier, Postgres est le meilleur moteur. Son SQL est plus proche du
standard, son planificateur est plus performant pour les jointures complexes,
son écosystème d'extensions (PostGIS, pg_trgm, pgvector, index partiels et
d'expression, opérateurs JSONB riches, CTE et fonctions de fenêtrage robustes)
est plus large que celui de MySQL, et ses sémantiques par défaut (DDL
transactionnel, gestion plus stricte des types, isolation raisonnable)
surprennent moins les développeurs. Si le seul critère était « quel moteur est
le plus capable pour une petite équipe ? », Postgres gagnerait.

Ce n'est pas le seul critère. Les contraintes de
[l'ADR 0004](/fr/adr/0004-adoption-of-oracle-cloud/) s'appliquent directement
ici : le projet fonctionne avec un budget étudiant, sur la capacité Oracle
Cloud Always Free, à Montréal. Deux options réelles découlent de cette
position, et une troisième est exclue :

- Une instance Postgres gérée comparable aux besoins du système ne fait pas
  partie du niveau OCI Always Free. Les solutions sont soit un Postgres géré
  payant chez un autre fournisseur (facture récurrente, ce qui contredit la
  même règle de coût qui a motivé le choix du cloud), soit un Postgres
  auto-hébergé sur le nœud ARM Oracle, ce qui fonctionne mais consomme les 24 Go
  de RAM partagés avec les autres charges et ajoute le travail de notre propre
  administrateur de base de données (sauvegardes, récupération à un instant
  donné, mises à niveau, réglages).
- L'Autonomous Database d'Oracle (variante Oracle DB) est disponible avec
  Always Free. Le blocage se situe côté Go : les pilotes Oracle DB pour Go sont
  peu pratiques (`godror` exige d'intégrer Oracle Instant Client à l'exécution,
  les alternatives en Go pur sont limitées et la communauté est réduite). Pour
  un code principalement en Go (voir [l'ADR
  0002](/fr/adr/0002-adoption-of-go-as-primary-service-language/)), cela
  représente une charge importante pour chaque service qui touche à la base.
- Le service **MySQL HeatWave** d'Oracle fait également partie d'Always Free,
  avec beaucoup plus de marge que les autres options gratuites examinées, et
  l'histoire du pilote Go pour MySQL est excellente
  (`go-sql-driver/mysql` est en Go pur, mature et très répandu).

Ce que nous voulons réellement du service HeatWave, ce ne sont pas ses
fonctionnalités d'accélération (moteur analytique en mémoire, intégration
Lakehouse, extensions ML). Nous ne les utiliserons pas. Nous voulons le moteur
MySQL sous-jacent sur lequel HeatWave est construit, hébergé par Oracle,
dimensionné à **8 Go de RAM et 50 Go de stockage** sans coût. Cette allocation
est nettement supérieure à celle des autres bases gérées gratuites, se trouve
dans la même région de Montréal que notre nœud ARM et est entièrement gérée
(sauvegardes, correctifs, haute disponibilité au niveau du stockage), ce qui
évite d'y consacrer notre temps.

Nos exigences sont les suivantes :

- Relationnelle, avec SQL correct, transactions, clés étrangères et index.
- Gratuite à l'échelle nécessaire, sur un niveau qui n'expire pas après 12 mois.
- Un pilote mature en Go pur, afin que les services n'aient pas à intégrer une
  bibliothèque cliente du fournisseur.
- Isolation du schéma par service, afin que chaque service possède ses propres
  tables sans partager d'espace de noms avec un autre service.
- Un dialecte SQL assez proche de Postgres pour qu'une migration future (si le
  budget permet plus tard un Postgres géré) reste un travail raisonnable plutôt
  qu'une réécriture.

## Décision

Au vu des exigences ci-dessus, **MySQL HeatWave sur OCI** est la base de
données relationnelle choisie. Les fonctionnalités d'accélération sont
ignorées ; nous l'utilisons comme une instance MySQL 8 gérée.

- **Capacité sans coût.** La forme HeatWave Always Free fournit 8 Go de RAM et
  50 Go de stockage à Montréal, l'allocation relationnelle gratuite la plus
  généreuse trouvée et une taille suffisante pour porter le système dans un
  avenir prévisible sans toucher au budget.
- **Qualité du pilote Go.** `github.com/go-sql-driver/mysql` est en Go pur, bien
  maintenu et fonctionne via l'interface standard `database/sql`. Aucun
  environnement d'exécution fournisseur, aucun CGO, aucun Oracle Instant
  Client à intégrer.
- **Isolation du schéma par service.** Chaque service possède son propre schéma
  MySQL (base de données dans la terminologie MySQL). Les lectures
  interservices passent par des API et des événements, jamais par des jointures
  entre schémas, ce qui préserve aussi au niveau des données les frontières de
  service définies dans [l'ADR 0001](/fr/adr/0001-rewriting-to-microservices/).
- **Proximité avec les standards de Postgres.** MySQL 8 est plus proche de
  Postgres que les versions anciennes (fonctions de fenêtrage, CTE, fonctions
  JSON, contraintes CHECK, valeurs par défaut utf8mb4 raisonnables). Combiné à
  `database/sql` et à un style de requête qui n'utilise pas de syntaxe propre à
  un dialecte, cela laisse ouverte la porte à une migration vers Postgres géré
  si le budget le permet, sans réécrire toute la couche de données.

Nous n'adoptons volontairement pas Oracle DB bien qu'il soit également
disponible avec Always Free, car son écosystème de pilotes Go est le facteur
limitant pour notre codebase. Les mérites techniques d'Oracle DB ne sont pas
remis en cause ; c'est le coût d'une intégration propre depuis Go qui pose
problème.

Nous n'auto-hébergeons pas non plus Postgres sur le nœud ARM, car la charge
opérationnelle (sauvegardes, mises à niveau, réplication, PITR) pour une équipe
d'une personne coûte plus cher que la facture cloud que nous cherchons à éviter.

## Conséquences

- Nous renonçons aux fonctionnalités propres à Postgres que nous aurions aimé
  utiliser un jour (pgvector pour d'éventuels embeddings, PostGIS, opérateurs
  `jsonb`, index partiels, DDL transactionnel). Si ces fonctionnalités deviennent
  nécessaires, nous effectuons le travail hors de la base ou réexaminons cette
  décision.
- MySQL possède quelques valeurs par défaut qui surprennent les développeurs
  Postgres (identifiants insensibles à la casse selon la plateforme, coercition
  de types plus permissive dans certains cas limites, `REPEATABLE READ` comme
  niveau d'isolation par défaut au lieu de `READ COMMITTED`). Nous fixons la
  configuration attendue (`utf8mb4`, `READ COMMITTED`, mode SQL strict) au
  niveau du schéma plutôt que de dépendre des valeurs par défaut.
- La base est une dépendance gérée par Oracle, et la perte de cette dépendance
  aurait un rayon d'impact supérieur à celui d'un nœud de calcul puisque les
  données y résident. La posture PAYG (voir
  [l'ADR 0004](/fr/adr/0004-adoption-of-oracle-cloud/)) supprime les risques de
  récupération après inactivité et de suspension, mais les pratiques standards
  de reprise après sinistre restent applicables : sauvegardes logiques
  régulières (`mysqldump` ou équivalent) stockées hors du fournisseur, afin de
  pouvoir récupérer d'une perte de compte ou d'une erreur opérateur dans le pire
  cas.
- Une migration future vers Postgres géré est réaliste, mais pas gratuite. En
  écrivant du SQL qui reste dans le sous-ensemble commun (aucune extension
  propre à MySQL, UTC partout, aucune dépendance aux raccourcis MySQL
  `ON UPDATE CURRENT_TIMESTAMP`, aucune date nulle), nous bornons ce coût. Des
  outils comme `sqlc` ou `sqlx` sur `database/sql` font du changement de pilote
  la plus petite partie de la migration ; les décisions de schéma constituent
  la partie la plus importante.
- Les schémas par service rendent les jointures interservices impossibles par
  conception. Les services qui ont besoin de données d'un autre service les
  demandent via l'API ou la surface événementielle documentée, ce qui est
  cohérent avec l'approche microservices et impose le respect des frontières de
  propriété au niveau des données.
- Les fonctionnalités d'accélération de HeatWave sont présentes mais inutilisées.
  Nous acceptons de porter (en charge cognitive, pas financière) des capacités
  que nous n'exerçons pas. Si une charge de travail bénéficie réellement un
  jour de l'analytique en mémoire, le moteur sera déjà disponible.
- Le parc compte désormais une dépendance gérée supplémentaire à surveiller. La
  disponibilité de HeatWave entre dans le SLO du système chaque fois qu'un
  service se trouve sur le chemin d'une requête qui l'interroge.

## Alternatives étudiées

- **PostgreSQL (auto-hébergé sur le nœud ARM).** Notre moteur préféré sur le
  plan technique. Rejeté parce qu'il consommerait de la RAM partagée avec les
  autres charges et parce qu'être notre propre administrateur de base de
  données (sauvegardes, récupération à un instant donné, mises à niveau,
  réglages) coûte plus cher que de confier la base à un service géré gratuit.
- **PostgreSQL géré chez un autre fournisseur** (DigitalOcean Managed Postgres,
  Neon, Supabase, etc.). La réponse la plus propre techniquement, mais toute
  option d'une taille utile entraîne une facture mensuelle récurrente. Cela
  contredit la règle de coût qui a motivé [l'ADR
  0004](/fr/adr/0004-adoption-of-oracle-cloud/), et les niveaux gratuits de ces
  fournisseurs sont soit trop petits, soit limités dans le temps.
- **Oracle Autonomous Database (variante Oracle DB) avec Always Free.** Capable
  techniquement et gratuit à une taille utile, mais le pilote Go est le blocage.
  `godror` exige d'intégrer Oracle Instant Client à chaque image qui parle à la
  base, et les alternatives en Go pur n'ont pas la maturité que nous jugeons
  nécessaire en production. Le coût se cumule pour chaque service.
- **SQLite (par service, embarqué).** Séduisant, sans opérations et adapté à
  l'histoire de coût, mais un véritable système multi-tenant avec des écritures
  concurrentes entre services n'est pas le terrain où SQLite excelle. Nous
  pourrons encore l'utiliser pour le développement local ou certains cas
  embarqués étroits, mais pas comme stockage principal.
- **MariaDB sur le nœud ARM.** Évite le risque d'un service géré par un
  fournisseur et conserve le même modèle de pilote Go inspiré de MySQL, mais
  réintroduit tous les coûts de l'auto-hébergement (sauvegardes, mises à niveau,
  réglages) qui nous ont éloignés de Postgres auto-hébergé. L'allocation HeatWave
  gérée est un meilleur compromis.
