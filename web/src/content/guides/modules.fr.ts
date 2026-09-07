// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { GuideContent } from '../../lib/guides/types';

const guide: GuideContent = {
  slug: 'modules',
  meta: {
    title: 'Le manuel des modules - Guides ItsBagelBot',
    description:
      "Chaque module d'ItsBagelBot expliqué: alertes de chat, shoutout automatique, mots déclencheurs, points de fidélité, récompenses de points de chaîne, minuteries, loteries, jeux de mise, file d'attente, citations, stats de jeu, pyramides d'émotes et AutoMod.",
    eyebrow: 'Guide 03',
    heading: 'Le manuel des modules',
    lead: "Chaque module sur une seule page: ce qu'il fait, ce qu'il dit, et quelles commandes il apporte à votre chat.",
    minutes: '12 min de lecture',
    card: {
      title: 'Le manuel des modules',
      description:
        "Chaque module, une seule page: alertes, minuteries, points de fidélité, récompenses de points de chaîne, mots déclencheurs, file d'attente, citations et stats de jeu.",
      meta: '12 min · 6 étapes',
      chips: ['alertes', 'minuteries', 'fidélité', 'stats de jeu'],
    },
  },
  sections: [
    {
      id: 'how',
      heading: 'Comment fonctionnent les modules',
      note: 'Un interrupteur par fonction. Cliquez la tuile pour que ses messages sonnent comme vous.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Un module est une fonction avec un interrupteur. La
                <a href="https://dashboard.itsbagelbot.com/modules" target="_blank" rel="noopener noreferrer">page Modules</a>
                les présente en tuiles: l'interrupteur active la fonction, cliquer la tuile ouvre ses
                réglages. La plupart des modules parlent dans le chat, et chacune de leurs phrases est
                un modèle que vous pouvez réécrire, avec les mêmes variables à accolades que les
                <a href="/fr/guides/commands">commandes personnalisées</a> (plus quelques extras par
                module, listés plus bas). Chaque modèle est accompagné de la même répétition en direct
                que les commandes, juste sous son éditeur : vous voyez la réponse atterrir dans un chat
                factice avant n'importe quel spectateur. À noter: les noms des modules s'affichent en
                anglais dans le tableau de bord (Chat Alerts, Timers, Loyalty Points…), comme dans les
                aperçus ci-dessous.
            </p>`,
        },
        {
          kind: 'dash',
          screen: 'ModulesCategories',
          path: '/modules',
          caption: 'Des tuiles groupées par catégorie, un rail à gauche. Configurer ouvre la page du module.',
          notes: [
            { n: 1, text: "L'interrupteur rapide. Inactif = module totalement silencieux, réglages conservés pour plus tard." },
            { n: 2, text: 'Configurer ouvre la page du module, où vivent ses phrases de chat et ses options.' },
          ],
          labels: {
            account: 'streamer · Diffuseur',
            eyebrow: 'Gérer',
            titleHtml: 'Modules de <i>chaîne</i>',
            sub: 'Fonctions optionnelles pour votre chaîne. 1 sur 20 activée.',
            configure: 'Configurer',
            dockOverview: 'Aperçu',
            dockCommands: 'Commandes',
            dockModules: 'Modules',
            dockBilling: 'Facturation',
            dockSettings: 'Paramètres',
          },
        },
        {
          kind: 'prose',
          html: `
            <p>
                Deux modules sont actifs par défaut: les <strong>Alertes de chat</strong> et
                <strong>AutoMod</strong> (qui reste hors de cette grille). Tout le reste vous attend.
            </p>`,
        },
      ],
    },
    {
      id: 'welcome',
      heading: 'Accueillir le monde',
      note: "Alertes, shoutouts de raid, mots déclencheurs et l'heure chez vous.",
      blocks: [
        {
          kind: 'prose',
          html: `
            <h3>Alertes de chat</h3>
            <p>
                Remercie les nouveaux follows, subs, cheers et raids dans le chat, chacun avec son
                message et son interrupteur. Variables en plus selon l'alerte: les subs ont
                <code>&#123;tier&#125;</code>, les cheers ont <code>&#123;bits&#125;</code>, les raids
                ont <code>&#123;viewers&#125;</code>.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#votre_chaine',
          caption: 'Des alertes avec de la personnalité valent mieux que des confettis.',
          lines: [
            { who: 'system', text: 'maya_live suit maintenant la chaîne' },
            { who: 'bot', text: 'Merci pour le follow, maya_live! Installe-toi 🥯' },
            { who: 'system', text: 'alex raid avec 42 spectateurs' },
            { who: 'bot', text: 'alex arrive avec 42 amis! Bienvenue tout le monde!' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <h3>Shoutout automatique</h3>
            <p>
                Quand on vous raid, le bot met la chaîne en avant sans que vous quittiez votre partie:
                <code>&#123;raider&#125;</code> est son nom d'affichage,
                <code>&#123;raider.login&#125;</code> la version compatible URL pour un lien twitch.tv,
                et <code>&#123;viewers&#125;</code> le nombre d'arrivants. Il peut aussi déclencher le
                /shoutout natif de Twitch en même temps.
            </p>
            <h3>Mots déclencheurs</h3>
            <p>
                Des réponses automatiques sans le «!»: donnez au bot une phrase à surveiller et la
                ligne à publier quand elle apparaît dans le chat ordinaire. Chaque règle a son petit
                éditeur sur la page du module: la phrase, sa détection (mot entier par défaut, donc
                «hi» ne se déclenche pas dans «this»; ou contient, message exact, commence par), la
                réponse avec la même répétition que les commandes, et son propre interrupteur. La
                première règle qui correspond gagne: un message reçoit au plus une réponse. Les
                réponses connaissent <code>&#123;user&#125;</code> plus les variables de dés et de
                choix.
            </p>
            <h3>Heure locale</h3>
            <p>
                Donne au chat une commande <code>!time</code> avec votre fuseau horaire et votre format
                d'heure, pour que plus personne ne demande «il est quelle heure chez toi?».
            </p>`,
        },
      ],
    },
    {
      id: 'economy',
      heading: 'Points, récompenses et minuteries',
      note: 'Des points de fidélité pour la présence, des récompenses de points de chaîne qui agissent, des messages planifiés et deux jeux de mise pour les courageux.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <h3>Points de fidélité</h3>
            <p>
                Les spectateurs gagnent des points en étant là: regarder, s'abonner, offrir des subs,
                envoyer des bits. Nommez les points comme vous voulez (des bagels?), et le chat vérifie
                son solde avec <code>!points</code>. Les modérateurs créditent ou fixent les soldes avec
                <code>!points add</code> et <code>!points set</code>. Ce module stocke aussi les
                <a href="/fr/guides/counters">compteurs</a> derrière
                <code>&#123;counter:…&#125;</code>, avec les outils de modération sous
                <code>!counter</code>.
            </p>
            <h3>Points de chaîne</h3>
            <p>
                Crée de vraies récompenses de points de chaîne Twitch et relie chaque échange à une
                action du bot, comme publier une phrase à modèle. Vous choisissez par récompense si les
                échanges sont acceptés, remboursés ou laissés au jugement d'un modérateur. Les modèles
                connaissent <code>&#123;user&#125;</code>, <code>&#123;input&#125;</code>,
                <code>&#123;reward&#125;</code>, <code>&#123;cost&#125;</code>,
                <code>&#123;counter&#125;</code> et <code>&#123;points&#125;</code>.
            </p>`,
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Le clou du spectacle</b>
                Combinez-le au module <strong>Lumières Govee</strong> et un échange peut recolorer les
                lumières de votre pièce. Le chat choisit coucher de soleil, votre mur obéit.`,
        },
        {
          kind: 'prose',
          html: `
            <h3>Minuteries</h3>
            <p>
                Publie un message à intervalle régulier pendant le direct: le rappel Discord toutes les
                20 minutes, l'hydratation toutes les heures.
            </p>`,
        },
        {
          kind: 'callout',
          tone: 'warn',
          html: `
                <b>À savoir</b>
                Les messages de minuterie sont publiés exactement tels quels: les variables à accolades
                ne fonctionnent pas dans les minuteries. Gardez-les pour les commandes et les réponses
                de modules.`,
        },
        {
          kind: 'prose',
          html: `
            <h3>Pari</h3>
            <p>
                Transforme vos points en jeu : les spectateurs misent leur propre solde avec
                <code>!gamble 100</code>, ou <code>!gamble half</code> / <code>!gamble all</code> pour
                les téméraires, et le bot lance un dé de 1 à 100. Atterrir dans la chance de gain que
                vous avez fixée rapporte la mise plus son équivalent ; sinon elle est prise. Vous
                choisissez aussi les limites de mise et un délai par spectateur, et chaque paiement ou
                débit passe par le même registre que <code>!points</code>. Les lignes de gain et de
                perte sont des modèles, avec <code>&#123;roll&#125;</code>,
                <code>&#123;chance&#125;</code>, <code>&#123;amount&#125;</code> et
                <code>&#123;balance&#125;</code>.
            </p>
            <h3>Duels</h3>
            <p>
                Des duels de points entre spectateurs, de deux façons. Le pot : quelqu'un ouvre avec
                <code>!duel 100</code>, chacun ajoute sa mise tant que la fenêtre est ouverte, puis le
                bot tire un gagnant pondéré par la mise et il rafle tout. Le défi :
                <code>!duel @maya_live 500</code> désigne un adversaire qui doit taper
                <code>!duel accept</code> avant la fin du délai : mises égales, pile ou face, le
                gagnant prend tout. Refus, annulations et absences remboursent toujours chaque point.
            </p>`,
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Équitable par conception</b>
                Les deux jeux passent par le registre des Points de fidélité : personne ne peut miser
                ce qu'il n'a pas, et la cote est exactement le nombre que vous fixez.`,
        },
      ],
    },
    {
      id: 'together',
      heading: 'Jouer ensemble',
      note: "Une loterie sans contestation possible, une file d'attente équitable, un recueil des meilleures citations du chat et des applaudissements pour les pyramides d'émotes.",
      blocks: [
        {
          kind: 'prose',
          html: `
            <h3>Loterie</h3>
            <p>
                Un tirage au sort minuté, impossible à contester. Les spectateurs participent une
                seule fois avec <code>!join</code> ; le bot fait le compte à rebours à voix haute à
                l'intervalle que vous choisissez, puis tire lui-même les gagnants à la fin du délai,
                uniformément au hasard, une participation par spectateur, avec un reçu
                vérifiable. Les gagnants confirment avec <code>!claim</code>. Vous et vos modérateurs
                lancez avec <code>!raffle open</code>, clôturez en avance avec
                <code>!raffle draw</code>, ou annulez avec <code>!raffle cancel</code>.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#votre_chaine',
          caption: 'Ouverte, rappelée, tirée, confirmée.',
          lines: [
            { who: 'mod', name: 'mod_sam', text: '!raffle open 10' },
            { who: 'bot', text: 'La loterie est LANCÉE ! Tapez !join pour participer. Tirage dans 10 min !' },
            { who: 'bot', text: 'Rappel loterie : ~5 min restantes ! 14 inscrits, tapez !join ! Les gagnants doivent taper !claim.' },
            { who: 'viewer', name: 'maya_live', text: '!join' },
            { who: 'bot', text: "@maya_live c'est bon, tu participes ! 15 inscrits pour l'instant. Bonne chance !" },
            { who: 'bot', text: '@crustycrumbs, @maya_live, félicitations ! Vous avez gagné la loterie (2 gagnant(s) parmi 15) ! Tapez !claim dans les 15 min pour confirmer votre lot !' },
            { who: 'viewer', name: 'maya_live', text: '!claim' },
            { who: 'bot', text: '@maya_live ton lot est confirmé, profite bien !' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <h3>File d'attente</h3>
            <p>
                «Je peux jouer?» devient libre-service. Les spectateurs entrent dans la file avec
                <code>!join</code>, la quittent avec <code>!leave</code> et vérifient l'ordre avec
                <code>!list</code>. Vous et vos modérateurs pilotez <code>!queue open</code>,
                <code>!queue next</code>, <code>!queue close</code>. Les réponses conversationnelles
                (rejoindre, quitter, au suivant, file ouverte et fermée) sont des modèles
                réécrivables.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#votre_chaine',
          caption: 'La file se tient toute seule.',
          lines: [
            { who: 'viewer', name: 'maya_live', text: '!join' },
            { who: 'bot', text: 'maya_live rejoint la file en position #3.' },
            { who: 'mod', name: 'mod_sam', text: '!queue next' },
            { who: 'bot', text: 'À toi, alex! 2 personnes derrière toi.' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <h3>Citations</h3>
            <p>
                Un recueil des phrases que le chat refuse de vous laisser oublier.
                <code>!quote</code> en sert une au hasard, <code>!quote 7</code> une précise, et
                <code>!quote furet</code> une au hasard contenant ce mot. L'ajout
                (<code>!addquote texte</code>) et la réécriture (<code>!quote edit 7 texte</code>)
                sont chacun réservés à qui vous voulez. Les modérateurs font le ménage avec
                <code>!quote remove</code>. La collection se consulte et se modifie depuis sa page
                du tableau de bord.
            </p>
            <h3>Pyramides et séries d'émotes</h3>
            <p>
                Une clique d'encouragements pour les projets artistiques du chat. Le module surveille
                les pyramides d'émotes (le même émote empilé 1-2-3 puis redescendu) et les séries de
                messages à émote unique, et publie une ligne de célébration quand l'une atterrit
                proprement, rien d'autre posté entre deux. Totalement automatique : pas de commande,
                pas de réglage, juste l'interrupteur.
            </p>`,
        },
      ],
    },
    {
      id: 'games',
      heading: 'Stats de jeu dans le chat',
      note: 'Bedwars, MCSR Ranked, Fortnite, Clash Royale et Valorant, à une commande de distance.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Cinq intégrations mettent les stats de jeu dans le chat pour que personne ne quitte le
                stream. Liez votre compte une fois sur la page du module; chaque réponse est un modèle
                réécrivable avec des variables propres au jeu.
            </p>`,
        },
        {
          kind: 'table',
          head: ['Module', 'Commandes', "Ce qu'elles répondent"],
          rows: [
            [
              'Stats Bedwars',
              '<code>!daily</code> <code>!weekly</code> <code>!monthly</code> <code>!bwstats</code> <code>!sniper</code> <code>!tag</code>',
              'Stats Hypixel Bedwars de période et à vie (victoires, finals, lits, FKDR), plus les recherches du réseau sniper.',
            ],
            [
              'MCSR Ranked',
              '<code>!elo</code> <code>!session</code> <code>!lastmatch</code> <code>!record</code> <code>!lb</code> <code>!race</code> <code>!pace</code> <code>!nethers</code> <code>!lastfort</code> <code>!pb</code>',
              'Elo de speedrun Minecraft, rang et bilan, comment se passe la session du stream, le dernier match, les bilans face-à-face, les classements top 5, la course hebdomadaire, le pace des splits en direct via PaceMan.gg, et les records personnels (quotidien/hebdomadaire/mensuel/classé).',
            ],
            [
              'Stats Fortnite',
              '<code>!fn</code> <code>!fn season</code> <code>!fn session</code> <code>!fn store</code>',
              'Stats à vie, de saison et du stream (victoires, K/D, taux de victoire), plus la boutique du jour.',
            ],
            [
              'Stats Clash Royale',
              '<code>!cr</code> <code>!cr decks</code> <code>!cr ranked</code> <code>!cr road</code>',
              'Profil à vie (niveau, victoires/défaites, taux de victoire), le deck actuel avec son élixir moyen, le classement Path of Legends et le record trophée, par tag de joueur.',
            ],
            [
              'Stats Valorant',
              '<code>!val</code> <code>!val matches</code> <code>!val account</code> <code>!val lb</code> <code>!val shop</code>',
              "Classement compétitif (rang, RR, variation du dernier match, record), dernières parties classées en K/D/A d'agent, résolution d'un Riot ID avec son niveau de compte, top 10 régionaux sur PC ou console, et rotation boutique du jour avec prix en VP et compte à rebours. Les formes contractées comme <code>!valrank</code> marchent aussi ; un mot de shard ou de ladder dans la ligne délimite cette recherche.",
            ],
          ],
        },
        {
          kind: 'chat',
          title: '#votre_chaine',
          caption: "Les spectateurs peuvent aussi chercher d'autres joueurs en ajoutant un nom.",
          lines: [
            { who: 'viewer', name: 'maya_live', text: '!elo' },
            { who: 'bot', text: 'votre_chaine: 1650 elo · rang #12 · 40V 20D cette saison' },
          ],
        },
      ],
    },
    {
      id: 'safety',
      heading: 'Modération et commandes intégrées',
      note: "Les quatre niveaux d'AutoMod, et les quatre commandes livrées avec le bot.",
      blocks: [
        {
          kind: 'prose',
          html: `
            <h3>AutoMod</h3>
            <p>
                La modération en couches de la page d'accueil. Elle arrive réglée sur son niveau
                intermédiaire recommandé et protège dès que le bot rejoint le chat: harcèlement,
                contenu sexuel, vulgarité, majuscules et symboles, liens indésirables sont filtrés,
                et le plancher de sécurité (insultes haineuses, liens frauduleux) ne se désactive
                jamais. Il n'y a pas encore de tuile ni de page de réglages dans le tableau de bord;
                les contrôles fins arrivent.
            </p>
            <h3>Les quatre commandes intégrées</h3>
            <p>
                Sur la <a href="https://dashboard.itsbagelbot.com/commands" target="_blank" rel="noopener noreferrer">page Commandes</a>,
                au-dessus de vos propres commandes, vivent quatre commandes livrées avec le bot:
            </p>
            <ul>
                <li><code>!followage</code>: depuis combien de temps quelqu'un vous suit.</li>
                <li><code>!accountage</code>: l'âge d'un compte Twitch.</li>
                <li><code>!uptime</code>: depuis combien de temps votre stream est en direct.</li>
                <li><code>!clip</code>: crée un clip des derniers instants et publie le lien (en direct seulement). Sa réponse est un modèle réécrivable, avec <code>&#123;clip&#125;</code>, <code>&#123;user&#125;</code> et <code>&#123;target&#125;</code>.</li>
            </ul>
            <p>
                Elles se désactivent comme n'importe quel module, mais ne se renomment pas et ne se
                suppriment pas. Voilà tout le manuel: activez les choses au rythme où votre communauté
                grandit.
            </p>`,
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Construisez quelque chose</b>
                Prêt à écrire vos propres commandes? Le
                <a href="/fr/command-builder">constructeur de commandes</a> fait la syntaxe pour vous.`,
        },
      ],
    },
  ],
};

export default guide;
