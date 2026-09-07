// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { GuideContent } from '../../lib/guides/types';

// Le catalogue du widget ModuleCatalog. Les noms et les descriptions d'une
// ligne sont ceux du tableau de bord: ils restent en anglais dans toutes les
// langues. La catégorie, le forfait, l'état de départ et la ligne "Nécessite"
// sont à nous, donc traduits.
const categories = ['Modération', 'Chat', 'Chaîne', 'Points', 'Jouer', 'Matériel', 'Stats'];

const modules = [
  {
    name: 'AutoMod',
    tagline: 'Catch scams, IP-grabbers and raid spam before your mods do.',
    cat: 'Modération',
    plan: 'premium',
    start: 'Actif par défaut',
  },
  {
    name: 'Trigger Words',
    tagline: 'Auto-reply when a word shows up in chat, no "!" needed.',
    cat: 'Chat',
    start: 'Inactif par défaut',
  },
  {
    name: 'Local Time',
    tagline: 'Viewers ask what time it is for you with !time.',
    cat: 'Chat',
    start: 'Inactif par défaut',
    needs: 'Un fuseau horaire, choisi sur la page du module.',
    commands: '!time',
  },
  {
    name: 'Quotes',
    tagline: 'Save the best things said on stream and replay them in chat.',
    cat: 'Chat',
    start: 'Inactif par défaut',
    commands: '!quote, !quotes, !quote <n>, !quote <mot>, !addquote, !quoteadd, !quote edit <n> <texte>, !quote remove <n>',
  },
  {
    name: 'Timers',
    tagline: 'Post repeating chat messages on a schedule while you are live.',
    cat: 'Chat',
    start: 'Inactif par défaut',
    needs: 'Un direct en cours. Hors ligne, les minuteurs se taisent.',
  },
  {
    name: 'Emote Pyramids & Streaks',
    tagline: 'Celebrate chat-built emote pyramids and emote streaks.',
    cat: 'Chat',
    start: 'Inactif par défaut',
  },
  {
    name: 'Chat Alerts',
    tagline: 'Announce follows, subs, cheers, raids and ad breaks in chat.',
    cat: 'Chaîne',
    start: 'Actif par défaut',
    needs: 'Les permissions Twitch accordées à la connexion.',
  },
  {
    name: 'Auto Shoutout',
    tagline: 'Welcome incoming raids with an automatic shoutout.',
    cat: 'Chaîne',
    start: 'Inactif par défaut',
    needs: 'Le rôle de modérateur, si vous voulez aussi le /shoutout natif de Twitch.',
  },
  {
    name: 'Channel Points',
    tagline: 'Turn channel-point redemptions into bot actions.',
    cat: 'Chaîne',
    start: 'Inactif par défaut',
    needs: 'Un compte affilié ou partenaire. Le bot crée les récompenses lui-même.',
  },
  {
    name: 'Stream Management',
    tagline: 'Set the live title, category and tags, run ads, and drop markers from chat.',
    cat: 'Chaîne',
    start: 'Toujours actif',
    commands: '!title, !settitle, !game, !setgame, !tags, !settags, !commercial, !ad, !marker, !cmd, !cmds, !command, !commands',
  },
  {
    name: 'Loyalty Points',
    tagline: 'Viewers earn channel currency for subs, cheers and watch time.',
    cat: 'Points',
    start: 'Inactif par défaut',
    commands: '!points, !points give @user 50, !leaderboard, !points set @user 500, !points add @user 100, !points remove @user 100',
  },
  {
    name: 'Counters',
    tagline: 'Track wins, deaths, hugs, redeems, anything your chat can count.',
    cat: 'Points',
    start: 'Toujours actif',
    commands: '!counter',
  },
  {
    name: 'Gamble',
    tagline: 'Let viewers wager their points on a roll with !gamble.',
    cat: 'Points',
    start: 'Inactif par défaut',
    needs: 'Loyalty Points activé. Gamble est une ligne sur la page Loyalty.',
    commands: '!gamble <montant>, !gamble half, !gamble all',
  },
  {
    name: 'Duels',
    tagline: 'Viewer-vs-viewer point duels: pot free-for-alls and 1v1 challenges.',
    cat: 'Points',
    start: 'Inactif par défaut',
    needs: 'Loyalty Points activé. Duels est une ligne sur la page Loyalty.',
    commands: '!duel, !duel <mise>, !duel <utilisateur> <mise>, !duel accept, !duel decline, !duel cancel',
  },
  {
    name: 'Play Queue',
    tagline: 'Let viewers line up to play with you, first come first served.',
    cat: 'Jouer',
    start: 'Inactif par défaut',
    commands: '!join, !leave, !list, !queuelist, !queue, !queue open, !queue close, !queue next, !queue remove <utilisateur>, !queue clear',
  },
  {
    name: 'Raffle',
    tagline: 'Timed random draws your chat enters with !join.',
    cat: 'Jouer',
    start: 'Inactif par défaut',
    needs: 'Raffle prend la main sur !join quand Play Queue est actif aussi.',
    commands: '!join, !claim, !winner, !raffle, !raffle open [minutes] [gagnants] [rappel], !raffle draw [gagnants], !raffle close, !raffle cancel',
  },
  {
    name: 'Song Requests',
    tagline: '!sr and a channel-points reward that queue songs from Spotify.',
    cat: 'Matériel',
    start: 'Inactif par défaut',
    needs: 'Votre propre application Spotify, plus une connexion Spotify.',
    commands: '!sr <titre ou lien>, !remove, !srlist, !songlist, !current, !song, !nowplaying, !np, !skip, !next, !clear',
  },
  {
    name: 'Govee Lights',
    tagline: 'Let viewers recolour your Govee lights with channel points.',
    cat: 'Matériel',
    start: 'Inactif par défaut',
    needs: 'Une clé API Govee et une récompense de points de chaîne. Fonctionne pendant le direct.',
  },
  {
    name: 'Discord',
    tagline: 'One bot on Twitch and Discord. Go-live, clips, welcomes, tickets, and voice.',
    cat: 'Matériel',
    plan: 'premium',
    start: 'Inactif par défaut',
    needs: 'Un serveur Discord connecté, ou créé depuis le modèle.',
  },
  {
    name: 'Bedwars Stats',
    tagline: 'Hypixel Bedwars stats, urchin score and blacklist tags in chat.',
    cat: 'Stats',
    start: 'Inactif par défaut',
    needs: 'Votre pseudo Minecraft.',
    commands: '!daily, !bwdaily, !weekly, !bwweekly, !monthly, !bwmonthly, !bwstats, !bedwars, !sniper, !urchin, !tag, !tags, !bwtags, !tagdescription',
  },
  {
    name: 'MCSR Ranked',
    tagline: 'Ranked elo and per-stream session stats for MCSR runners.',
    cat: 'Stats',
    start: 'Inactif par défaut',
    needs: 'Votre pseudo Minecraft, un compte MCSR Ranked et un compte PaceMan.',
    commands: '!elo [joueur], !lastmatch, !record <a> <b>, !matchrecord, !lb, !leaderboard, !rankedlb, !session, !mcsrsession, !race, !weeklyrace, !pb, !personalbest daily, !pace, !nethers, !lastfort',
  },
  {
    name: 'Fortnite Stats',
    tagline: 'Fortnite BR stats and the daily item shop in chat.',
    cat: 'Stats',
    start: 'Inactif par défaut',
    needs: 'Votre nom affiché Epic.',
    commands: '!fn [joueur], !fnstats, !fortnitestats, !fnseason, !fnsession, !fnstore, !itemshop, !fnshop',
  },
  {
    name: 'Clash Royale Stats',
    tagline: 'Clash Royale profiles, decks and Path of Legends standing in chat.',
    cat: 'Stats',
    start: 'Inactif par défaut',
    needs: 'Votre tag de joueur Supercell, du genre #P2LQ0GR.',
    commands: '!cr [tag], !crstats, !clashroyale, !crdecks, !crdeck, !crranked, !crpol, !crroad, !crtrophy',
  },
  {
    name: 'Valorant Stats',
    tagline: 'Valorant ranks, match history, leaderboards and the daily shop rotation in chat.',
    cat: 'Stats',
    start: 'Inactif par défaut',
    needs: 'Votre Riot ID, du genre Frosty#EUW1, et votre région.',
    commands: '!val [RiotID], !valrank, !valmatches, !valhistory, !valaccount, !valwho, !vallb, !valleaderboard, !valshop, !valrotation',
  },
];

const guide: GuideContent = {
  slug: 'modules',
  meta: {
    title: 'Modules - Guides ItsBagelBot',
    description:
      "La page Modules d'ItsBagelBot expliquée: les sept catégories, les formes de tuiles, la configuration d'un module, les points et les jeux, les stats de jeu dans le chat, et les neuf commandes intégrées.",
    eyebrow: 'Guide',
    heading: 'Modules',
    lead: "Chaque fonction de votre chaîne est une tuile avec un interrupteur. Voici ce que fait chacune, ce qu'il faut préparer avant, et ce que veulent dire les tuiles bizarres.",
    minutes: '10 min de lecture',
    card: {
      title: 'Modules',
      description:
        'Les 24 tuiles de votre page Modules: ce que fait chacune, ce dont elle a besoin pour fonctionner, et les commandes de chat qu\'elle apporte.',
      meta: '10 min · 7 sections',
      chips: ['modération', 'chat', 'points', 'stats'],
    },
  },
  sections: [
    {
      id: 'page',
      heading: 'La page Modules',
      note: 'Sept catégories, un interrupteur par tuile, six formes de tuiles.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Un module est une fonction du bot avec son propre interrupteur. La
                <a href="https://dashboard.itsbagelbot.com/modules" target="_blank" rel="noopener noreferrer">page Modules</a>
                les présente en tuiles, regroupées par le rail Catégories à gauche: Modération, Chat,
                Chaîne, Points, Jouer, Matériel, Stats. La ligne sous le titre compte ce qui tourne,
                et la recherche accepte un nom de module, ce qu'il fait, ou une commande de chat dont
                vous vous souvenez à moitié.
            </p>`,
        },
        {
          kind: 'dash',
          screen: 'ModulesGrid',
          path: '/modules',
          caption: 'La page Modules: le rail Catégories, les tuiles avec leur bouton Configurer, et l\'interrupteur.',
          notes: [
            { n: 1, text: 'Le rail Catégories. Sept groupes, dans cet ordre, et un clic fait défiler la grille jusque-là.' },
            { n: 2, text: 'Une tuile, c\'est un module: son nom, sa catégorie, et la phrase que le tableau de bord utilise pour le décrire.' },
            { n: 3, text: 'Configurer ouvre la page du module, là où vivent ses réglages et ses messages de chat.' },
            { n: 4, text: 'L\'interrupteur. Éteint, le module se tait, et chaque réglage reste tel que vous l\'avez laissé.' },
            { n: 5, text: 'AutoMod porte une pastille "Bêta · Premium". Sur une chaîne gratuite, la tuile est verrouillée.' },
            { n: 6, text: 'Counters n\'a pas d\'interrupteur. Il est toujours actif, comme Stream Management.' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <p>
                La plupart des tuiles fonctionnent pareil: vous basculez l'interrupteur, vous cliquez
                sur Configurer, c'est fini. Cinq tuiles se comportent autrement, et savoir lesquelles
                vous évite de chercher un interrupteur qui n'a jamais existé.
            </p>`,
        },
        {
          kind: 'table',
          head: ['Forme de tuile', 'Ce que vous voyez', 'Quels modules'],
          rows: [
            [
              'Ordinaire',
              'Un interrupteur et un bouton Configurer. L\'interrupteur active la fonction sur votre chaîne.',
              'Timers, Quotes, Raffle, et la majorité de la grille.',
            ],
            [
              'Caché',
              'Le module tourne à l\'intérieur du bot et n\'atteint jamais la grille, parce qu\'il n\'y a rien à régler.',
              'La tuyauterie interne derrière les commandes.',
            ],
            [
              'Section',
              'Le module saute la grille et reçoit sa propre page dans le tableau de bord.',
              'Discord.',
            ],
            [
              'Imbriqué',
              'Une ligne sur la page du module parent, sans interrupteur à lui. La ligne dit: "Ce jeu dépense les <code>&#123;parent&#125;</code>. Activez-le depuis cette page. Il ne peut pas tourner tout seul."',
              'Gamble et Duels, sur la page Loyalty Points.',
            ],
            [
              'Toujours actif',
              'Une tuile avec un bouton Configurer, et l\'interrupteur manque volontairement. La fonction tourne quoi qu\'il arrive.',
              'Counters, Stream Management.',
            ],
            [
              'Bêta',
              'Une tuile verrouillée avec une pastille "Bêta · Premium", et les réglages en dessous une fois Premium activé.',
              'AutoMod, Discord.',
            ],
          ],
          caption: 'Six formes de tuiles, dont cinq surprennent tout le monde au moins une fois.',
        },
        {
          kind: 'callout',
          tone: 'note',
          html: `
                <b>Note</b>
                Deux modules sont réservés au Premium pendant leur bêta: AutoMod et Discord. Tout le
                reste de cette page fonctionne sur le forfait gratuit, aussi longtemps que vous voulez.`,
        },
      ],
    },
    {
      id: 'catalog',
      heading: 'Tous les modules',
      note: 'Le catalogue complet, filtré par catégorie ou par commande.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Voici la liste complète, la même que celle du tableau de bord. Chaque carte porte la
                description d'une ligne du module, sa catégorie, son état de départ, ce qu'il faut
                préparer avant, et les commandes de chat qu'il ajoute. Choisissez une pastille de
                catégorie, ou tapez dans le filtre: il cherche dans les noms, les descriptions et les
                commandes, donc <code>!sr</code> trouve Song Requests et "elo" trouve MCSR Ranked.
            </p>`,
        },
        {
          kind: 'widget',
          name: 'ModuleCatalog',
          labels: {
            all: 'Tous',
            searchLabel: 'Filtrer les modules',
            searchPlaceholder: 'Nom, commande, ou ce que ça fait',
            countAll: '{n} modules',
            countCat: '{n} dans {cat}',
            countMatch: '{shown} sur {total}',
            commands: 'Commandes',
            needs: 'Nécessite',
            free: 'Gratuit',
            premium: 'Bêta premium',
            empty: 'Aucun résultat. Essayez une commande comme !sr, ou un nom de jeu.',
          },
          props: { categories, modules },
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Astuce</b>
                La même recherche se trouve en haut de la page du tableau de bord. Si un viewer
                demande quelque chose et que vous ne savez plus quel module s'en occupe, tapez la
                commande là.`,
        },
      ],
    },
    {
      id: 'configure',
      heading: 'Configurer un module',
      note: 'Chat Alerts, ses six réponses, et la répétition sous l\'éditeur.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Configurer ouvre la page du module. Elle a toujours les mêmes trois parties: l'état
                du module en haut, les réglages, et les réponses qu'il publie dans le chat. Chat
                Alerts est un bon terrain d'apprentissage, parce qu'il a six réponses avec six
                interrupteurs: follow, abonnement, sub offert, cheer, raid, et pause publicitaire.
            </p>`,
        },
        {
          kind: 'dash',
          screen: 'ModuleConfigure',
          path: '/modules/alerts',
          caption: 'Chat Alerts: l\'état du module, les six réponses, et l\'éditeur ancré à droite.',
          notes: [
            { n: 1, text: 'État du module. Un seul interrupteur pour tout le module, et l\'éteindre conserve chaque réglage.' },
            { n: 2, text: 'Chaque réponse est une ligne que vous pouvez ouvrir, avec son propre interrupteur. Chat Alerts en a six.' },
            { n: 3, text: 'L\'alerte de pause publicitaire arrive éteinte. Activez-la si vous voulez prévenir le chat avant les pubs.' },
            { n: 4, text: 'Le message. Les accolades sont des variables que le bot remplit: {user} ici, {bits} sur l\'alerte de cheer.' },
            { n: 5, text: 'La répétition, comme dans l\'éditeur de commandes. Vous voyez la ligne arriver avant un viewer.' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <p>
                Chaque réponse est un modèle. <code>&#123;user&#125;</code> est le viewer qui l'a
                déclenchée, et chaque alerte ajoute la sienne: <code>&#123;tier&#125;</code> pour les
                abonnements, <code>&#123;count&#125;</code> pour les subs offerts,
                <code>&#123;bits&#125;</code> pour les cheers, <code>&#123;viewers&#125;</code> pour
                les raids, <code>&#123;duration&#125;</code> pour les pauses publicitaires. L'éditeur
                liste les variables acceptées par cette réponse, et le catalogue plus haut vous dit
                quels modules ont des réponses à réécrire. Laissez un message vide et le bot utilise
                sa phrase par défaut.
            </p>`,
        },
        {
          kind: 'callout',
          tone: 'note',
          html: `
                <b>Note</b>
                Une alerte de follow se déclenche au maximum une fois par viewer tous les trois
                jours: quelqu'un qui se désabonne et se réabonne ne peut pas inonder votre chat.`,
        },
      ],
    },
    {
      id: 'points',
      heading: 'Points, gamble et duels',
      note: 'Loyalty est le parent. Gamble et Duels sont des lignes sur sa page.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                <strong>Loyalty Points</strong> donne une monnaie à votre chaîne. Vous la nommez
                (bagels, miettes, ce que le chat acceptera de dire à voix haute) et vous décidez ce
                qui en fait gagner: un abonnement, un réabonnement, un sub offert, un cheer, et le
                temps de visionnage compté toutes les 5 minutes. Les viewers consultent leur solde
                avec <code>!points</code>, en donnent avec
                <code>!points give @maya_live 50</code>, et se comparent avec
                <code>!leaderboard</code>. Les modérateurs corrigent le registre avec
                <code>!points set</code>, <code>!points add</code> et <code>!points remove</code>.
            </p>
            <p>
                Deux jeux dépensent cette monnaie, et tous les deux vivent en lignes sur la page
                Loyalty Points plutôt qu'en tuiles à eux.
            </p>`,
        },
        {
          kind: 'table',
          head: ['Jeu', 'Valeurs par défaut', 'Commandes'],
          rows: [
            [
              'Gamble',
              'Chance de gagner 50%, réglable de 1 à 99. Mise minimum 1, maximum 1000. Délai de 10 s par viewer.',
              '<code>!gamble 100</code>, <code>!gamble half</code>, <code>!gamble all</code>',
            ],
            [
              'Duels',
              'Mises de 1 à 1000. Une cagnotte reste ouverte 60 s. Un défi nominatif attend une réponse 120 s.',
              '<code>!duel</code>, <code>!duel 500</code>, <code>!duel @ferret_king 500</code>, <code>!duel accept</code>',
            ],
          ],
          caption: 'Les valeurs livrées avec le bot. Toutes se changent.',
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: 'Un lancer qui paie, un lancer qui ne paie pas, et un duel qui finit mal pour un des deux.',
          lines: [
            { who: 'viewer', name: 'sesame_sam', text: '!gamble 100' },
            { who: 'bot', text: '@sesame_sam rolled 37 (needed 50 or less) and won 100 bagels, now at 480!' },
            { who: 'viewer', name: 'ferret_king', text: '!gamble 250' },
            { who: 'bot', text: '@ferret_king rolled 88 (needed 50 or less) and lost 250 bagels. Now at 90.' },
            { who: 'viewer', name: 'maya_live', text: '!duel @ferret_king 500' },
            { who: 'bot', text: '@maya_live challenges @ferret_king for 500 bagels! @ferret_king, type !duel accept within 120s. Winner takes 1000!' },
            { who: 'viewer', name: 'ferret_king', text: '!duel accept' },
            { who: 'bot', text: 'The blades fall: @maya_live defeats @ferret_king and takes 1000 bagels!' },
          ],
        },
        {
          kind: 'callout',
          tone: 'warn',
          html: `
                <b>Attention</b>
                Chercher Gamble ou Duels sur la grille des modules est un voyage pour rien. Activez
                Loyalty Points, ouvrez sa page, et allumez les jeux depuis les lignes qui s'y
                trouvent.`,
        },
      ],
    },
    {
      id: 'games',
      heading: 'Les stats de jeu dans le chat',
      note: 'Cinq modules, un champ de compte chacun, une commande que votre chat va user.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Cinq modules répondent à "tu es quel rang?" pour que vous n'ayez plus à le faire.
                Chacun demande un seul champ de compte sur sa page, puis chaque réponse est un modèle
                avec les variables de ce jeu. Les alias sont généreux: l'orthographe courte et la
                longue marchent toutes les deux.
            </p>`,
        },
        {
          kind: 'cards',
          columns: 3,
          items: [
            {
              title: 'Bedwars Stats',
              html: `
                <p>Renseignez votre pseudo Minecraft. Sept modèles de réponse, avec un délai par commande.</p>
                <p><code>!daily</code> <code>!weekly</code> <code>!monthly</code> <code>!bwstats</code> <code>!sniper</code> <code>!tag</code></p>
                <p>sesame_sam today: 12W 4L · 210 finals · 34 beds · 3.1 FKDR</p>`,
              chips: ['Hypixel'],
            },
            {
              title: 'MCSR Ranked',
              html: `
                <p>Renseignez votre pseudo Minecraft. Demande un compte MCSR Ranked et un compte PaceMan. Chaque commande a son interrupteur.</p>
                <p><code>!elo</code> <code>!session</code> <code>!lastmatch</code> <code>!record</code> <code>!lb</code> <code>!pace</code> <code>!pb</code></p>
                <p>sesame_sam: 1650 elo · rank #12 · 40W 20L this season</p>`,
              chips: ['Minecraft'],
            },
            {
              title: 'Fortnite Stats',
              html: `
                <p>Renseignez votre nom affiché Epic. Le type de compte est Epic par défaut.</p>
                <p><code>!fn</code> <code>!fnstats</code> <code>!fnseason</code> <code>!fnsession</code> <code>!fnstore</code></p>
                <p>Item Shop 2026-09-07: Renegade Raider, Aerial Assault Trooper, Take the L</p>`,
              chips: ['Epic'],
            },
            {
              title: 'Clash Royale Stats',
              html: `
                <p>Renseignez votre tag de joueur Supercell, celui qui ressemble à #P2LQ0GR.</p>
                <p><code>!cr</code> <code>!crstats</code> <code>!crdecks</code> <code>!crranked</code> <code>!crroad</code></p>
                <p>sesame_sam · level 42 · 5120W/4380L · 54% WR · 1180 three-crowns · Crust Clan</p>`,
              chips: ['Supercell'],
            },
            {
              title: 'Valorant Stats',
              html: `
                <p>Renseignez votre Riot ID et votre région. La région est eu par défaut, la plateforme pc.</p>
                <p><code>!val</code> <code>!valrank</code> <code>!valmatches</code> <code>!vallb</code> <code>!valshop</code></p>
                <p>Frosty#EUW1 · Immortal 2 · 143 RR (+21) · peak Immortal 3</p>`,
              chips: ['Riot'],
            },
          ],
        },
        {
          kind: 'callout',
          tone: 'warn',
          html: `
                <b>Attention</b>
                Deux orthographes que tout le monde rate. La commande de saison Fortnite s'écrit
                <code>!fnseason</code>, en un seul mot. Et la session MCSR repart à zéro dès que
                votre direct commence, donc <code>!session</code> répond pour ce soir, pas pour la
                semaine.`,
        },
      ],
    },
    {
      id: 'gear',
      heading: 'Musique, lumières, Discord',
      note: 'Les trois modules qui sortent de Twitch, et ce que chacun réclame en premier.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <h3>Song Requests</h3>
            <p>
                Le chat met de la musique dans votre propre Spotify avec <code>!sr un titre</code> ou
                un lien Spotify. Vous décidez quel niveau d'accès peut demander. Tout le monde a
                <code>!current</code> (aussi <code>!song</code>, <code>!nowplaying</code>,
                <code>!np</code>) et <code>!srlist</code>; les modérateurs ont <code>!skip</code> et
                <code>!clear</code>. La configuration demande deux choses: votre propre application
                Spotify, et une connexion Spotify. Une récompense de points de chaîne qui met un
                titre en file est optionnelle, et se règle sur la même page.
            </p>`,
        },
        {
          kind: 'prose',
          html: `
            <h3>Govee Lights</h3>
            <p>
                Les viewers dépensent des points de chaîne pour recolorer vos lumières Govee. Vous
                collez une clé API Govee, vous choisissez l'appareil, et vous liez une récompense. La
                clé est chiffrée et ne vous est jamais réaffichée, pas même au tableau de bord. Pour
                en obtenir une: application Govee Home &gt; Profile &gt; roue dentée &gt; "Apply for
                API Key".
            </p>`,
        },
        {
          kind: 'callout',
          tone: 'warn',
          html: `
                <b>Attention</b>
                Les lumières ne répondent que pendant le direct. Une récompense utilisée hors direct
                est remboursée automatiquement, donc personne ne paie pour une pièce éteinte.`,
        },
        {
          kind: 'prose',
          html: `
            <h3>Discord</h3>
            <p>
                Le même bot des deux côtés: annonces de direct, clips, accueils, tickets et salons
                vocaux. Discord saute la grille des modules et ne dit rien dans le chat Twitch; il
                reçoit sa propre page dans le tableau de bord. Connectez un serveur que vous gérez
                déjà, ou laissez le bot en construire un depuis le modèle. C'est premium pendant la
                bêta, et ce que vous configurez pendant la bêta continue de fonctionner après.
            </p>`,
        },
      ],
    },
    {
      id: 'builtins',
      heading: 'Toujours actives',
      note: 'Neuf commandes arrivent avec le bot, plus une poignée de petites.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Au-dessus de vos propres commandes, la
                <a href="https://dashboard.itsbagelbot.com/commands" target="_blank" rel="noopener noreferrer">page Commandes</a>
                liste neuf commandes intégrées. Elles arrivent avec le bot, elles se désactivent, et
                elles ne se renomment ni ne se suppriment.
            </p>`,
        },
        {
          kind: 'table',
          head: ['Commande', 'Qui peut la lancer', 'Ce qu\'elle répond'],
          rows: [
            ['<code>!accountage</code>', 'Tout le monde', 'L\'âge d\'un compte Twitch.'],
            ['<code>!followage</code>', 'Tout le monde', 'Depuis combien de temps quelqu\'un vous suit.'],
            ['<code>!uptime</code>', 'Tout le monde', 'Depuis combien de temps le direct est en cours.'],
            [
              '<code>!clip</code>',
              'Tout le monde, en direct seulement',
              'Clippe les derniers instants et publie le lien. <code>!clip30</code> fait un clip de 30 secondes.',
            ],
            ['<code>!title</code> <code>!settitle</code>', 'Lead modo', 'Lit le titre du direct, ou en met un nouveau.'],
            ['<code>!game</code> <code>!setgame</code>', 'Lead modo', 'Lit la catégorie, ou en met une nouvelle.'],
            ['<code>!tags</code> <code>!settags</code>', 'Lead modo', 'Lit les tags du direct, ou les remplace.'],
            ['<code>!commercial</code> <code>!ad</code>', 'Lead modo, en direct seulement', 'Lance une pause publicitaire.'],
            ['<code>!marker</code>', 'Lead modo, en direct seulement', 'Pose un marqueur que vous retrouverez dans le VOD.'],
          ],
          caption: 'Les neuf commandes intégrées, et qui a le droit de les lancer.',
        },
        {
          kind: 'prose',
          html: `
            <p>
                Quelques autres répondent sans apparaître nulle part: <code>!ping</code> prouve que le
                bot est réveillé, <code>!itsbagelbot</code> et <code>!source</code> disent ce qu'il
                est, <code>!bagels</code> compte les bagels que le chat lui a donnés, et
                <code>!bagelboard</code> classe les donneurs. Les modérateurs ont <code>!cmd</code>
                (aussi <code>!commands</code>) pour la liste des commandes, et <code>!nuke</code>
                quand un raid demande d'effacer une même ligne chez beaucoup de monde d'un coup.
            </p>`,
        },
        {
          kind: 'callout',
          tone: 'note',
          html: `
                <b>Note</b>
                Un lead modo est un modérateur que vous avez promu dans le tableau de bord, un cran
                au-dessus de vos autres modérateurs. C'est le niveau derrière lequel se trouvent les
                contrôles du direct.`,
        },
      ],
    },
  ],
};

export default guide;
