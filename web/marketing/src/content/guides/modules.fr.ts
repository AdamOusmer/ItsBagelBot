// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The "modules" guide in fr. Copy only: the structure it fills is the
// English guide, modules.en.ts, and every id below names one string in it
// (lib/guides/translate.ts derives the ids from where the strings sit).
// Adding a language is this file translated, with no structure to get wrong.
import type { GuideStrings } from '../../lib/guides/translate';

const strings: GuideStrings = {
    'builtins.b0.html': `
            <p>
                Au-dessus de vos propres commandes, la
                <a href="https://dashboard.itsbagelbot.com/commands" target="_blank" rel="noopener noreferrer">page Commandes</a>
                liste neuf commandes intégrées. Elles arrivent avec le bot, elles se désactivent, et
                elles ne se renomment ni ne se suppriment.
            </p>`,
    'builtins.b1.caption': 'Les neuf commandes intégrées, et qui a le droit de les lancer.',
    'builtins.b1.head.0': 'Commande',
    'builtins.b1.head.1': 'Qui peut la lancer',
    'builtins.b1.head.2': "Ce qu'elle répond",
    'builtins.b1.rows.0.0': '<code>!accountage</code>',
    'builtins.b1.rows.0.1': 'Tout le monde',
    'builtins.b1.rows.0.2': "L'âge d'un compte Twitch.",
    'builtins.b1.rows.1.0': '<code>!followage</code>',
    'builtins.b1.rows.1.1': 'Tout le monde',
    'builtins.b1.rows.1.2': "Depuis combien de temps quelqu'un vous suit.",
    'builtins.b1.rows.2.0': '<code>!uptime</code>',
    'builtins.b1.rows.2.1': 'Tout le monde',
    'builtins.b1.rows.2.2': 'Depuis combien de temps le direct est en cours.',
    'builtins.b1.rows.3.0': '<code>!clip</code>',
    'builtins.b1.rows.3.1': 'Tout le monde, en direct seulement',
    'builtins.b1.rows.3.2': 'Clippe les derniers instants et publie le lien. <code>!clip30</code> fait un clip de 30 secondes.',
    'builtins.b1.rows.4.0': '<code>!title</code> <code>!settitle</code>',
    'builtins.b1.rows.4.1': 'Lead modo',
    'builtins.b1.rows.4.2': 'Lit le titre du direct, ou en met un nouveau.',
    'builtins.b1.rows.5.0': '<code>!game</code> <code>!setgame</code>',
    'builtins.b1.rows.5.1': 'Lead modo',
    'builtins.b1.rows.5.2': 'Lit la catégorie, ou en met une nouvelle.',
    'builtins.b1.rows.6.0': '<code>!tags</code> <code>!settags</code>',
    'builtins.b1.rows.6.1': 'Lead modo',
    'builtins.b1.rows.6.2': 'Lit les tags du direct, ou les remplace.',
    'builtins.b1.rows.7.0': '<code>!commercial</code> <code>!ad</code>',
    'builtins.b1.rows.7.1': 'Lead modo, en direct seulement',
    'builtins.b1.rows.7.2': 'Lance une pause publicitaire.',
    'builtins.b1.rows.8.0': '<code>!marker</code>',
    'builtins.b1.rows.8.1': 'Lead modo, en direct seulement',
    'builtins.b1.rows.8.2': 'Pose un marqueur que vous retrouverez dans le VOD.',
    'builtins.b2.html': `
            <p>
                Quelques autres répondent sans apparaître nulle part: <code>!ping</code> prouve que le
                bot est réveillé, <code>!itsbagelbot</code> et <code>!source</code> disent ce qu'il
                est, <code>!bagels</code> compte les bagels que le chat lui a donnés, et
                <code>!bagelboard</code> classe les donneurs. Les modérateurs ont <code>!cmd</code>
                (aussi <code>!commands</code>) pour la liste des commandes, et <code>!nuke</code>
                quand un raid demande d'effacer une même ligne chez beaucoup de monde d'un coup.
            </p>`,
    'builtins.b3.html': `
                <b>Note</b>
                Un lead modo est un modérateur que vous avez promu dans le tableau de bord, un cran
                au-dessus de vos autres modérateurs. C'est le niveau derrière lequel se trouvent les
                contrôles du direct.`,
    'builtins.heading': 'Toujours actives',
    'builtins.note': 'Neuf commandes arrivent avec le bot, plus une poignée de petites.',
    'catalog.b0.html': `
            <p>
                Voici la liste complète, la même que celle du tableau de bord. Chaque carte porte la
                description d'une ligne du module, sa catégorie, son état de départ, ce qu'il faut
                préparer avant, et les commandes de chat qu'il ajoute. Choisissez une pastille de
                catégorie, ou tapez dans le filtre: il cherche dans les noms, les descriptions et les
                commandes, donc <code>!sr</code> trouve Song Requests et "elo" trouve MCSR Ranked.
            </p>`,
    'catalog.b1.labels.all': 'Tous',
    'catalog.b1.labels.commands': 'Commandes',
    'catalog.b1.labels.countAll': '{n} modules',
    'catalog.b1.labels.countCat': '{n} dans {cat}',
    'catalog.b1.labels.countMatch': '{shown} sur {total}',
    'catalog.b1.labels.empty': 'Aucun résultat. Essayez une commande comme !sr, ou un nom de jeu.',
    'catalog.b1.labels.free': 'Gratuit',
    'catalog.b1.labels.needs': 'Nécessite',
    'catalog.b1.labels.premium': 'Bêta premium',
    'catalog.b1.labels.searchLabel': 'Filtrer les modules',
    'catalog.b1.labels.searchPlaceholder': 'Nom, commande, ou ce que ça fait',
    'catalog.b1.props.categories.0': 'Modération',
    'catalog.b1.props.categories.1': 'Chat',
    'catalog.b1.props.categories.2': 'Chaîne',
    'catalog.b1.props.categories.3': 'Points',
    'catalog.b1.props.categories.4': 'Jouer',
    'catalog.b1.props.categories.5': 'Matériel',
    'catalog.b1.props.categories.6': 'Stats',
    'catalog.b1.props.modules.0.cat': 'Modération',
    'catalog.b1.props.modules.0.name': 'AutoMod',
    'catalog.b1.props.modules.0.plan': 'premium',
    'catalog.b1.props.modules.0.start': 'Actif par défaut',
    'catalog.b1.props.modules.0.tagline': 'Catch scams, IP-grabbers and raid spam before your mods do.',
    'catalog.b1.props.modules.1.cat': 'Chat',
    'catalog.b1.props.modules.1.name': 'Trigger Words',
    'catalog.b1.props.modules.1.start': 'Inactif par défaut',
    'catalog.b1.props.modules.1.tagline': 'Auto-reply when a word shows up in chat, no "!" needed.',
    'catalog.b1.props.modules.10.cat': 'Points',
    'catalog.b1.props.modules.10.commands': '!points, !points give @user 50, !leaderboard, !points set @user 500, !points add @user 100, !points remove @user 100',
    'catalog.b1.props.modules.10.name': 'Loyalty Points',
    'catalog.b1.props.modules.10.start': 'Inactif par défaut',
    'catalog.b1.props.modules.10.tagline': 'Viewers earn channel currency for subs, cheers and watch time.',
    'catalog.b1.props.modules.11.cat': 'Points',
    'catalog.b1.props.modules.11.commands': '!counter',
    'catalog.b1.props.modules.11.name': 'Counters',
    'catalog.b1.props.modules.11.start': 'Toujours actif',
    'catalog.b1.props.modules.11.tagline': 'Track wins, deaths, hugs, redeems, anything your chat can count.',
    'catalog.b1.props.modules.12.cat': 'Points',
    'catalog.b1.props.modules.12.commands': '!gamble <montant>, !gamble half, !gamble all',
    'catalog.b1.props.modules.12.name': 'Gamble',
    'catalog.b1.props.modules.12.needs': 'Loyalty Points activé. Gamble est une ligne sur la page Loyalty.',
    'catalog.b1.props.modules.12.start': 'Inactif par défaut',
    'catalog.b1.props.modules.12.tagline': 'Let viewers wager their points on a roll with !gamble.',
    'catalog.b1.props.modules.13.cat': 'Points',
    'catalog.b1.props.modules.13.commands': '!duel, !duel <mise>, !duel <utilisateur> <mise>, !duel accept, !duel decline, !duel cancel',
    'catalog.b1.props.modules.13.name': 'Duels',
    'catalog.b1.props.modules.13.needs': 'Loyalty Points activé. Duels est une ligne sur la page Loyalty.',
    'catalog.b1.props.modules.13.start': 'Inactif par défaut',
    'catalog.b1.props.modules.13.tagline': 'Viewer-vs-viewer point duels: pot free-for-alls and 1v1 challenges.',
    'catalog.b1.props.modules.14.cat': 'Jouer',
    'catalog.b1.props.modules.14.commands': '!join, !leave, !list, !queuelist, !queue, !queue open, !queue close, !queue next, !queue remove <utilisateur>, !queue clear',
    'catalog.b1.props.modules.14.name': 'Play Queue',
    'catalog.b1.props.modules.14.start': 'Inactif par défaut',
    'catalog.b1.props.modules.14.tagline': 'Let viewers line up to play with you, first come first served.',
    'catalog.b1.props.modules.15.cat': 'Jouer',
    'catalog.b1.props.modules.15.commands': '!join, !claim, !winner, !raffle, !raffle open [minutes] [gagnants] [rappel], !raffle draw [gagnants], !raffle close, !raffle cancel',
    'catalog.b1.props.modules.15.name': 'Raffle',
    'catalog.b1.props.modules.15.needs': 'Raffle prend la main sur !join quand Play Queue est actif aussi.',
    'catalog.b1.props.modules.15.start': 'Inactif par défaut',
    'catalog.b1.props.modules.15.tagline': 'Timed random draws your chat enters with !join.',
    'catalog.b1.props.modules.16.cat': 'Matériel',
    'catalog.b1.props.modules.16.commands': '!sr <titre ou lien>, !remove, !srlist, !songlist, !current, !song, !nowplaying, !np, !skip, !next, !clear',
    'catalog.b1.props.modules.16.name': 'Song Requests',
    'catalog.b1.props.modules.16.needs': 'Votre propre application Spotify, plus une connexion Spotify.',
    'catalog.b1.props.modules.16.start': 'Inactif par défaut',
    'catalog.b1.props.modules.16.tagline': '!sr and a channel-points reward that queue songs from Spotify.',
    'catalog.b1.props.modules.17.cat': 'Matériel',
    'catalog.b1.props.modules.17.name': 'Govee Lights',
    'catalog.b1.props.modules.17.needs': 'Une clé API Govee et une récompense de points de chaîne. Fonctionne pendant le direct.',
    'catalog.b1.props.modules.17.start': 'Inactif par défaut',
    'catalog.b1.props.modules.17.tagline': 'Let viewers recolour your Govee lights with channel points.',
    'catalog.b1.props.modules.18.cat': 'Matériel',
    'catalog.b1.props.modules.18.name': 'Discord',
    'catalog.b1.props.modules.18.needs': 'Un serveur Discord connecté, ou créé depuis le modèle.',
    'catalog.b1.props.modules.18.plan': 'premium',
    'catalog.b1.props.modules.18.start': 'Inactif par défaut',
    'catalog.b1.props.modules.18.tagline': 'One bot on Twitch and Discord. Go-live, clips, welcomes, tickets, and voice.',
    'catalog.b1.props.modules.19.cat': 'Stats',
    'catalog.b1.props.modules.19.commands': '!daily, !bwdaily, !weekly, !bwweekly, !monthly, !bwmonthly, !bwstats, !bedwars, !sniper, !urchin, !tag, !tags, !bwtags, !tagdescription',
    'catalog.b1.props.modules.19.name': 'Bedwars Stats',
    'catalog.b1.props.modules.19.needs': 'Votre pseudo Minecraft.',
    'catalog.b1.props.modules.19.start': 'Inactif par défaut',
    'catalog.b1.props.modules.19.tagline': 'Hypixel Bedwars stats, urchin score and blacklist tags in chat.',
    'catalog.b1.props.modules.2.cat': 'Chat',
    'catalog.b1.props.modules.2.commands': '!time',
    'catalog.b1.props.modules.2.name': 'Local Time',
    'catalog.b1.props.modules.2.needs': 'Un fuseau horaire, choisi sur la page du module.',
    'catalog.b1.props.modules.2.start': 'Inactif par défaut',
    'catalog.b1.props.modules.2.tagline': 'Viewers ask what time it is for you with !time.',
    'catalog.b1.props.modules.20.cat': 'Stats',
    'catalog.b1.props.modules.20.commands': '!elo [joueur], !lastmatch, !record <a> <b>, !matchrecord, !lb, !leaderboard, !rankedlb, !session, !mcsrsession, !race, !weeklyrace, !pb, !personalbest daily, !pace, !nethers, !lastfort',
    'catalog.b1.props.modules.20.name': 'MCSR Ranked',
    'catalog.b1.props.modules.20.needs': 'Votre pseudo Minecraft, un compte MCSR Ranked et un compte PaceMan.',
    'catalog.b1.props.modules.20.start': 'Inactif par défaut',
    'catalog.b1.props.modules.20.tagline': 'Ranked elo and per-stream session stats for MCSR runners.',
    'catalog.b1.props.modules.21.cat': 'Stats',
    'catalog.b1.props.modules.21.commands': '!fn [joueur], !fnstats, !fortnitestats, !fnseason, !fnsession, !fnstore, !itemshop, !fnshop',
    'catalog.b1.props.modules.21.name': 'Fortnite Stats',
    'catalog.b1.props.modules.21.needs': 'Votre nom affiché Epic.',
    'catalog.b1.props.modules.21.start': 'Inactif par défaut',
    'catalog.b1.props.modules.21.tagline': 'Fortnite BR stats and the daily item shop in chat.',
    'catalog.b1.props.modules.22.cat': 'Stats',
    'catalog.b1.props.modules.22.commands': '!cr [tag], !crstats, !clashroyale, !crdecks, !crdeck, !crranked, !crpol, !crroad, !crtrophy',
    'catalog.b1.props.modules.22.name': 'Clash Royale Stats',
    'catalog.b1.props.modules.22.needs': 'Votre tag de joueur Supercell, du genre #P2LQ0GR.',
    'catalog.b1.props.modules.22.start': 'Inactif par défaut',
    'catalog.b1.props.modules.22.tagline': 'Clash Royale profiles, decks and Path of Legends standing in chat.',
    'catalog.b1.props.modules.23.cat': 'Stats',
    'catalog.b1.props.modules.23.commands': '!val [RiotID], !valrank, !valmatches, !valhistory, !valaccount, !valwho, !vallb, !valleaderboard, !valshop, !valrotation',
    'catalog.b1.props.modules.23.name': 'Valorant Stats',
    'catalog.b1.props.modules.23.needs': 'Votre Riot ID, du genre Frosty#EUW1, et votre région.',
    'catalog.b1.props.modules.23.start': 'Inactif par défaut',
    'catalog.b1.props.modules.23.tagline': 'Valorant ranks, match history, leaderboards and the daily shop rotation in chat.',
    'catalog.b1.props.modules.3.cat': 'Chat',
    'catalog.b1.props.modules.3.commands': '!quote, !quotes, !quote <n>, !quote <mot>, !addquote, !quoteadd, !quote edit <n> <texte>, !quote remove <n>',
    'catalog.b1.props.modules.3.name': 'Quotes',
    'catalog.b1.props.modules.3.start': 'Inactif par défaut',
    'catalog.b1.props.modules.3.tagline': 'Save the best things said on stream and replay them in chat.',
    'catalog.b1.props.modules.4.cat': 'Chat',
    'catalog.b1.props.modules.4.name': 'Timers',
    'catalog.b1.props.modules.4.needs': 'Un direct en cours. Hors ligne, les minuteurs se taisent.',
    'catalog.b1.props.modules.4.start': 'Inactif par défaut',
    'catalog.b1.props.modules.4.tagline': 'Post repeating chat messages on a schedule while you are live.',
    'catalog.b1.props.modules.5.cat': 'Chat',
    'catalog.b1.props.modules.5.name': 'Emote Pyramids & Streaks',
    'catalog.b1.props.modules.5.start': 'Inactif par défaut',
    'catalog.b1.props.modules.5.tagline': 'Celebrate chat-built emote pyramids and emote streaks.',
    'catalog.b1.props.modules.6.cat': 'Chaîne',
    'catalog.b1.props.modules.6.name': 'Chat Alerts',
    'catalog.b1.props.modules.6.needs': 'Les permissions Twitch accordées à la connexion.',
    'catalog.b1.props.modules.6.start': 'Actif par défaut',
    'catalog.b1.props.modules.6.tagline': 'Announce follows, subs, cheers, raids and ad breaks in chat.',
    'catalog.b1.props.modules.7.cat': 'Chaîne',
    'catalog.b1.props.modules.7.name': 'Auto Shoutout',
    'catalog.b1.props.modules.7.needs': 'Le rôle de modérateur, si vous voulez aussi le /shoutout natif de Twitch.',
    'catalog.b1.props.modules.7.start': 'Inactif par défaut',
    'catalog.b1.props.modules.7.tagline': 'Welcome incoming raids with an automatic shoutout.',
    'catalog.b1.props.modules.8.cat': 'Chaîne',
    'catalog.b1.props.modules.8.name': 'Channel Points',
    'catalog.b1.props.modules.8.needs': 'Un compte affilié ou partenaire. Le bot crée les récompenses lui-même.',
    'catalog.b1.props.modules.8.start': 'Inactif par défaut',
    'catalog.b1.props.modules.8.tagline': 'Turn channel-point redemptions into bot actions.',
    'catalog.b1.props.modules.9.cat': 'Chaîne',
    'catalog.b1.props.modules.9.commands': '!title, !settitle, !game, !setgame, !tags, !settags, !commercial, !ad, !marker, !cmd, !cmds, !command, !commands',
    'catalog.b1.props.modules.9.name': 'Stream Management',
    'catalog.b1.props.modules.9.start': 'Toujours actif',
    'catalog.b1.props.modules.9.tagline': 'Set the live title, category and tags, run ads, and drop markers from chat.',
    'catalog.b2.html': `
                <b>Astuce</b>
                La même recherche se trouve en haut de la page du tableau de bord. Si un viewer
                demande quelque chose et que vous ne savez plus quel module s'en occupe, tapez la
                commande là.`,
    'catalog.heading': 'Tous les modules',
    'catalog.note': 'Le catalogue complet, filtré par catégorie ou par commande.',
    'configure.b0.html': `
            <p>
                Configurer ouvre la page du module. Elle a toujours les mêmes trois parties: l'état
                du module en haut, les réglages, et les réponses qu'il publie dans le chat. Chat
                Alerts est un bon terrain d'apprentissage, parce qu'il a six réponses avec six
                interrupteurs: follow, abonnement, sub offert, cheer, raid, et pause publicitaire.
            </p>`,
    'configure.b1.caption': "Chat Alerts: l'état du module, les six réponses, et l'éditeur ancré à droite.",
    'configure.b1.notes.0.text': "État du module. Un seul interrupteur pour tout le module, et l'éteindre conserve chaque réglage.",
    'configure.b1.notes.1.text': 'Chaque réponse est une ligne que vous pouvez ouvrir, avec son propre interrupteur. Chat Alerts en a six.',
    'configure.b1.notes.2.text': "L'alerte de pause publicitaire arrive éteinte. Activez-la si vous voulez prévenir le chat avant les pubs.",
    'configure.b1.notes.3.text': "Le message. Les accolades sont des variables que le bot remplit: {user} ici, {bits} sur l'alerte de cheer.",
    'configure.b1.notes.4.text': "La répétition, comme dans l'éditeur de commandes. Vous voyez la ligne arriver avant un viewer.",
    'configure.b2.html': `
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
    'configure.b3.html': `
                <b>Note</b>
                Une alerte de follow se déclenche au maximum une fois par viewer tous les trois
                jours: quelqu'un qui se désabonne et se réabonne ne peut pas inonder votre chat.`,
    'configure.heading': 'Configurer un module',
    'configure.note': "Chat Alerts, ses six réponses, et la répétition sous l'éditeur.",
    'games.b0.html': `
            <p>
                Cinq modules répondent à "tu es quel rang?" pour que vous n'ayez plus à le faire.
                Chacun demande un seul champ de compte sur sa page, puis chaque réponse est un modèle
                avec les variables de ce jeu. Les alias sont généreux: l'orthographe courte et la
                longue marchent toutes les deux.
            </p>`,
    'games.b1.items.0.chips.0': 'Hypixel',
    'games.b1.items.0.html': `
                <p>Renseignez votre pseudo Minecraft. Sept modèles de réponse, avec un délai par commande.</p>
                <p><code>!daily</code> <code>!weekly</code> <code>!monthly</code> <code>!bwstats</code> <code>!sniper</code> <code>!tag</code></p>
                <p>sesame_sam today: 12W 4L · 210 finals · 34 beds · 3.1 FKDR</p>`,
    'games.b1.items.0.title': 'Bedwars Stats',
    'games.b1.items.1.chips.0': 'Minecraft',
    'games.b1.items.1.html': `
                <p>Renseignez votre pseudo Minecraft. Demande un compte MCSR Ranked et un compte PaceMan. Chaque commande a son interrupteur.</p>
                <p><code>!elo</code> <code>!session</code> <code>!lastmatch</code> <code>!record</code> <code>!lb</code> <code>!pace</code> <code>!pb</code></p>
                <p>sesame_sam: 1650 elo · rank #12 · 40W 20L this season</p>`,
    'games.b1.items.1.title': 'MCSR Ranked',
    'games.b1.items.2.chips.0': 'Epic',
    'games.b1.items.2.html': `
                <p>Renseignez votre nom affiché Epic. Le type de compte est Epic par défaut.</p>
                <p><code>!fn</code> <code>!fnstats</code> <code>!fnseason</code> <code>!fnsession</code> <code>!fnstore</code></p>
                <p>Item Shop 2026-09-07: Renegade Raider, Aerial Assault Trooper, Take the L</p>`,
    'games.b1.items.2.title': 'Fortnite Stats',
    'games.b1.items.3.chips.0': 'Supercell',
    'games.b1.items.3.html': `
                <p>Renseignez votre tag de joueur Supercell, celui qui ressemble à #P2LQ0GR.</p>
                <p><code>!cr</code> <code>!crstats</code> <code>!crdecks</code> <code>!crranked</code> <code>!crroad</code></p>
                <p>sesame_sam · level 42 · 5120W/4380L · 54% WR · 1180 three-crowns · Crust Clan</p>`,
    'games.b1.items.3.title': 'Clash Royale Stats',
    'games.b1.items.4.chips.0': 'Riot',
    'games.b1.items.4.html': `
                <p>Renseignez votre Riot ID et votre région. La région est eu par défaut, la plateforme pc.</p>
                <p><code>!val</code> <code>!valrank</code> <code>!valmatches</code> <code>!vallb</code> <code>!valshop</code></p>
                <p>Frosty#EUW1 · Immortal 2 · 143 RR (+21) · peak Immortal 3</p>`,
    'games.b1.items.4.title': 'Valorant Stats',
    'games.b2.html': `
                <b>Attention</b>
                Deux orthographes que tout le monde rate. La commande de saison Fortnite s'écrit
                <code>!fnseason</code>, en un seul mot. Et la session MCSR repart à zéro dès que
                votre direct commence, donc <code>!session</code> répond pour ce soir, pas pour la
                semaine.`,
    'games.heading': 'Les stats de jeu dans le chat',
    'games.note': 'Cinq modules, un champ de compte chacun, une commande que votre chat va user.',
    'gear.b0.html': `
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
    'gear.b1.html': `
            <h3>Govee Lights</h3>
            <p>
                Les viewers dépensent des points de chaîne pour recolorer vos lumières Govee. Vous
                collez une clé API Govee, vous choisissez l'appareil, et vous liez une récompense. La
                clé est chiffrée et ne vous est jamais réaffichée, pas même au tableau de bord. Pour
                en obtenir une: application Govee Home &gt; Profile &gt; roue dentée &gt; "Apply for
                API Key".
            </p>`,
    'gear.b2.html': `
                <b>Attention</b>
                Les lumières ne répondent que pendant le direct. Une récompense utilisée hors direct
                est remboursée automatiquement, donc personne ne paie pour une pièce éteinte.`,
    'gear.b3.html': `
            <h3>Discord</h3>
            <p>
                Le même bot des deux côtés: annonces de direct, clips, accueils, tickets et salons
                vocaux. Discord saute la liste des modules et ne dit rien dans le chat Twitch; il
                reçoit sa propre page dans le tableau de bord. Connectez un serveur que vous gérez
                déjà, ou laissez le bot en construire un depuis le modèle. C'est premium pendant la
                bêta, et ce que vous configurez pendant la bêta continue de fonctionner après.
            </p>`,
    'gear.heading': 'Musique, lumières, Discord',
    'gear.note': 'Les trois modules qui sortent de Twitch, et ce que chacun réclame en premier.',
    'meta.card.chips.0': 'modération',
    'meta.card.chips.1': 'chat',
    'meta.card.chips.2': 'points',
    'meta.card.chips.3': 'stats',
    'meta.card.description': "Les 24 modules de votre page Modules: ce que fait chacune, ce dont elle a besoin pour fonctionner, et les commandes de chat qu'elle apporte.",
    'meta.card.meta': '10 min · 7 sections',
    'meta.card.title': 'Modules',
    'meta.description': "La page Modules d'ItsBagelBot expliquée: les sept catégories, les formes de lignes, la configuration d'un module, les points et les jeux, les stats de jeu dans le chat, et les neuf commandes intégrées.",
    'meta.eyebrow': 'Guide',
    'meta.heading': 'Modules',
    'meta.lead': "Chaque fonction de votre chaîne est une ligne avec un interrupteur. Voici ce que fait chacune, ce qu'il faut préparer avant, et ce que veulent dire les lignes bizarres.",
    'meta.minutes': '10 min de lecture',
    'meta.title': 'Modules - Guides ItsBagelBot',
    'page.b0.html': `
            <p>
                Un module est une fonction du bot avec son propre interrupteur. La
                <a href="https://dashboard.itsbagelbot.com/modules" target="_blank" rel="noopener noreferrer">page Modules</a>
                les présente en lignes, une carte par catégorie, avec le rail Catégories à gauche:
                Modération, Chat, Chaîne, Points, Jouer, Matériel, Stats. La ligne sous le titre compte ce qui tourne,
                et la recherche accepte un nom de module, ce qu'il fait, ou une commande de chat dont
                vous vous souvenez à moitié.
            </p>`,
    'page.b1.caption': "La page Modules: le rail Catégories, une ligne par module, et l'interrupteur.",
    'page.b1.notes.0.text': 'Le rail Catégories. Sept groupes, dans cet ordre, et un clic fait défiler la liste jusque-là.',
    'page.b1.notes.1.text': "Une ligne, c'est un module: son nom, la phrase que le tableau de bord utilise pour le décrire, et les commandes de chat qu'il apporte.",
    'page.b1.notes.2.text': 'Cliquer une ligne ouvre la page du module, là où vivent ses réglages et ses messages de chat.',
    'page.b1.notes.3.text': "L'interrupteur. Éteint, le module se tait, et chaque réglage reste tel que vous l'avez laissé.",
    'page.b1.notes.4.text': 'AutoMod porte une pastille "Bêta · Premium". Sur une chaîne gratuite, la ligne est verrouillée.',
    'page.b1.notes.5.text': "Counters n'a pas d'interrupteur. Il est toujours actif, comme Stream Management.",
    'page.b2.html': `
            <p>
                La plupart des lignes fonctionnent pareil: vous basculez l'interrupteur, vous ouvrez
                la ligne, c'est fini. Cinq modules se comportent autrement, et savoir lesquels vous
                évite de chercher un interrupteur qui n'a jamais existé.
            </p>`,
    'page.b3.caption': 'Six formes de lignes, dont cinq surprennent tout le monde au moins une fois.',
    'page.b3.head.0': 'Forme de ligne',
    'page.b3.head.1': 'Ce que vous voyez',
    'page.b3.head.2': 'Quels modules',
    'page.b3.rows.0.0': 'Ordinaire',
    'page.b3.rows.0.1': "Une ligne à ouvrir et un interrupteur à côté. L'interrupteur active la fonction sur votre chaîne.",
    'page.b3.rows.0.2': 'Timers, Quotes, Raffle, et la majorité de la liste.',
    'page.b3.rows.1.0': 'Caché',
    'page.b3.rows.1.1': "Le module tourne à l'intérieur du bot et n'atteint jamais la liste, parce qu'il n'y a rien à régler.",
    'page.b3.rows.1.2': 'La tuyauterie interne derrière les commandes.',
    'page.b3.rows.2.0': 'Section',
    'page.b3.rows.2.1': 'Le module saute la liste et reçoit sa propre page dans le tableau de bord.',
    'page.b3.rows.2.2': 'Discord.',
    'page.b3.rows.3.0': 'Imbriqué',
    'page.b3.rows.3.1': 'Une ligne sur la page du module parent, sans interrupteur à lui. La ligne dit: "Ce jeu dépense les <code>&#123;parent&#125;</code>. Activez-le depuis cette page. Il ne peut pas tourner tout seul."',
    'page.b3.rows.3.2': 'Gamble et Duels, sur la page Loyalty Points.',
    'page.b3.rows.4.0': 'Toujours actif',
    'page.b3.rows.4.1': "Une ligne à ouvrir, et l'interrupteur manque volontairement. La fonction tourne quoi qu'il arrive.",
    'page.b3.rows.4.2': 'Counters, Stream Management.',
    'page.b3.rows.5.0': 'Bêta',
    'page.b3.rows.5.1': 'Une ligne verrouillée avec une pastille "Bêta · Premium", et les réglages en dessous une fois Premium activé.',
    'page.b3.rows.5.2': 'AutoMod, Discord.',
    'page.b4.html': `
                <b>Note</b>
                Deux modules sont réservés au Premium pendant leur bêta: AutoMod et Discord. Tout le
                reste de cette page fonctionne sur le forfait gratuit, aussi longtemps que vous voulez.`,
    'page.heading': 'La page Modules',
    'page.note': 'Sept catégories, un interrupteur par ligne, six formes de lignes.',
    'points.b0.html': `
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
                Loyalty Points plutôt qu'en lignes à eux.
            </p>`,
    'points.b1.caption': 'Les valeurs livrées avec le bot. Toutes se changent.',
    'points.b1.head.0': 'Jeu',
    'points.b1.head.1': 'Valeurs par défaut',
    'points.b1.head.2': 'Commandes',
    'points.b1.rows.0.0': 'Gamble',
    'points.b1.rows.0.1': 'Chance de gagner 50%, réglable de 1 à 99. Mise minimum 1, maximum 1000. Délai de 10 s par viewer.',
    'points.b1.rows.0.2': '<code>!gamble 100</code>, <code>!gamble half</code>, <code>!gamble all</code>',
    'points.b1.rows.1.0': 'Duels',
    'points.b1.rows.1.1': 'Mises de 1 à 1000. Une cagnotte reste ouverte 60 s. Un défi nominatif attend une réponse 120 s.',
    'points.b1.rows.1.2': '<code>!duel</code>, <code>!duel 500</code>, <code>!duel @ferret_king 500</code>, <code>!duel accept</code>',
    'points.b2.caption': 'Un lancer qui paie, un lancer qui ne paie pas, et un duel qui finit mal pour un des deux.',
    'points.b2.lines.0.name': 'sesame_sam',
    'points.b2.lines.0.text': '!gamble 100',
    'points.b2.lines.1.text': '@sesame_sam rolled 37 (needed 50 or less) and won 100 bagels, now at 480!',
    'points.b2.lines.2.name': 'ferret_king',
    'points.b2.lines.2.text': '!gamble 250',
    'points.b2.lines.3.text': '@ferret_king rolled 88 (needed 50 or less) and lost 250 bagels. Now at 90.',
    'points.b2.lines.4.name': 'maya_live',
    'points.b2.lines.4.text': '!duel @ferret_king 500',
    'points.b2.lines.5.text': '@maya_live challenges @ferret_king for 500 bagels! @ferret_king, type !duel accept within 120s. Winner takes 1000!',
    'points.b2.lines.6.name': 'ferret_king',
    'points.b2.lines.6.text': '!duel accept',
    'points.b2.lines.7.text': 'The blades fall: @maya_live defeats @ferret_king and takes 1000 bagels!',
    'points.b2.title': '#your_channel',
    'points.b3.html': `
                <b>Attention</b>
                Chercher Gamble ou Duels dans la liste des modules est un voyage pour rien. Activez
                Loyalty Points, ouvrez sa page, et allumez les jeux depuis les lignes qui s'y
                trouvent.`,
    'points.heading': 'Points, gamble et duels',
    'points.note': 'Loyalty est le parent. Gamble et Duels sont des lignes sur sa page.',
};

export default strings;
