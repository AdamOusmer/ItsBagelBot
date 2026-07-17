// Command-builder catalog + UI copy, both locales. This file is the marketing
// site's single source of truth for what the bot actually expands, verified
// against the worker: custom-command tokens in app/sesame/engine/vars.go,
// dynamic tokens in app/sesame/module/vars.go, module reply tokens in each
// app/sesame/modules/*.go, and limits in internal/domain/validate/validate.go
// (mirrored by console/shared/lib/commands-validate.ts). If a token isn't
// expanded there, it doesn't belong here — the bot leaves unknown braces
// as literal text.

import type { Lang } from './ui';

type L10n = Record<Lang, string>;

interface VarDef {
  token: string;
  sample: string;
  name: L10n;
  desc: L10n;
}

interface SurfaceDef {
  id: string;
  group: L10n;
  label: L10n;
  /** Where this template is edited in the dashboard. */
  dashPath: string;
  hint: L10n;
  /** Starter template shown when the surface is selected. */
  example: L10n;
  /** The viewer/system line shown in the rehearsal. */
  prompt: L10n;
  vars: VarDef[];
}

const v = (token: string, sample: string, name: L10n, desc: L10n): VarDef => ({
  token,
  sample,
  name,
  desc,
});

// Dynamic tokens work in custom commands and in every module reply template
// (module.ParseDynamic is each module's fallback).
const DYNAMIC: VarDef[] = [
  v('{random}', '42', { en: 'Random 1-100', fr: 'Aléatoire 1-100' }, { en: 'A whole number from 1 to 100, new every time.', fr: 'Un nombre entier de 1 à 100, différent à chaque fois.' }),
  v('{random:1-6}', '4', { en: 'Random range', fr: 'Plage aléatoire' }, { en: 'Pick your own range; both ends included. Change the numbers.', fr: 'Choisissez votre plage; les deux bornes comptent. Changez les nombres.' }),
  v('{choice:yes,no,maybe}', 'maybe', { en: 'Random choice', fr: 'Choix aléatoire' }, { en: 'Picks one of your comma-separated options. Replace the words.', fr: 'Choisit une option de votre liste séparée par des virgules. Remplacez les mots.' }),
];

const USER_VIEWER = v('{user}', 'maya_live', { en: 'Viewer name', fr: 'Nom du spectateur' }, { en: 'Whoever used the command. {sender} works too.', fr: 'La personne qui a utilisé la commande. {sender} fonctionne aussi.' });

export const SURFACES: SurfaceDef[] = [
  {
    id: 'custom',
    group: { en: 'Commands', fr: 'Commandes' },
    label: { en: 'Custom command', fr: 'Commande personnalisée' },
    dashPath: '/commands',
    hint: { en: 'A reply viewers trigger with !yourcommand.', fr: 'Une réponse que les spectateurs déclenchent avec !votrecommande.' },
    example: { en: 'Welcome in, {user}! Grab a seat 🥯', fr: 'Bienvenue, {user}! Installe-toi 🥯' },
    prompt: { en: '!welcome', fr: '!bienvenue' },
    vars: [
      USER_VIEWER,
      v('{touser}', 'alex', { en: 'Named person', fr: 'Personne nommée' }, { en: 'The first word typed after the command ("@" removed); the viewer themself when blank. {target} works too.', fr: 'Le premier mot tapé après la commande (sans «@»); le spectateur lui-même si vide. {target} fonctionne aussi.' }),
      v('{args}', 'good luck!', { en: 'Everything typed after', fr: 'Tout le texte tapé après' }, { en: 'All text after the command, as one string.', fr: 'Tout le texte après la commande, en une seule chaîne.' }),
      v('{channel}', 'your_channel', { en: 'Channel name', fr: 'Nom de la chaîne' }, { en: "Your channel's display name.", fr: 'Le nom d’affichage de votre chaîne.' }),
      v('{counter:falls}', '128', { en: 'Counter (+1)', fr: 'Compteur (+1)' }, { en: 'Adds 1 to the named counter and shows the total. Needs the Loyalty Points module.', fr: 'Ajoute 1 au compteur nommé et affiche le total. Requiert le module Points de fidélité.' }),
      ...DYNAMIC,
    ],
  },
  {
    id: 'follow',
    group: { en: 'Alerts', fr: 'Alertes' },
    label: { en: 'Follow alert', fr: 'Alerte de follow' },
    dashPath: '/modules/alerts',
    hint: { en: 'Chat Alerts module → follow message.', fr: 'Module Alertes de chat → message de follow.' },
    example: { en: 'Thanks for the follow, {user}!', fr: 'Merci pour le follow, {user}!' },
    prompt: { en: 'maya_live followed the channel', fr: 'maya_live suit maintenant la chaîne' },
    vars: [
      v('{user}', 'maya_live', { en: 'New follower', fr: 'Nouveau follower' }, { en: 'The person who just followed.', fr: 'La personne qui vient de suivre la chaîne.' }),
      ...DYNAMIC,
    ],
  },
  {
    id: 'subscribe',
    group: { en: 'Alerts', fr: 'Alertes' },
    label: { en: 'Subscription alert', fr: "Alerte d'abonnement" },
    dashPath: '/modules/alerts',
    hint: { en: 'Chat Alerts module → subscription message.', fr: "Module Alertes de chat → message d'abonnement." },
    example: { en: 'Welcome, {user}! Thanks for the tier {tier} sub!', fr: 'Bienvenue, {user}! Merci pour le sub palier {tier}!' },
    prompt: { en: 'maya_live subscribed', fr: "maya_live s'est abonnée" },
    vars: [
      v('{user}', 'maya_live', { en: 'Subscriber', fr: 'Abonné' }, { en: 'The new subscriber.', fr: 'La personne qui vient de s’abonner.' }),
      v('{tier}', '1', { en: 'Sub tier', fr: 'Palier' }, { en: 'The subscription tier.', fr: "Le palier de l'abonnement." }),
      ...DYNAMIC,
    ],
  },
  {
    id: 'cheer',
    group: { en: 'Alerts', fr: 'Alertes' },
    label: { en: 'Cheer alert', fr: 'Alerte de cheer' },
    dashPath: '/modules/alerts',
    hint: { en: 'Chat Alerts module → cheer message.', fr: 'Module Alertes de chat → message de cheer.' },
    example: { en: 'Thanks for the {bits} bits, {user}! 💎', fr: 'Merci pour les {bits} bits, {user}! 💎' },
    prompt: { en: 'maya_live cheered 250 bits', fr: 'maya_live a envoyé 250 bits' },
    vars: [
      v('{user}', 'maya_live', { en: 'Cheerer', fr: 'Donateur' }, { en: 'The viewer who cheered.', fr: 'La personne qui a envoyé des bits.' }),
      v('{bits}', '250', { en: 'Bits amount', fr: 'Nombre de bits' }, { en: 'How many bits were cheered.', fr: 'Le nombre de bits envoyés.' }),
      ...DYNAMIC,
    ],
  },
  {
    id: 'raid',
    group: { en: 'Alerts', fr: 'Alertes' },
    label: { en: 'Raid alert', fr: 'Alerte de raid' },
    dashPath: '/modules/alerts',
    hint: { en: 'Chat Alerts module → raid message.', fr: 'Module Alertes de chat → message de raid.' },
    example: { en: '{user} raided with {viewers} viewers! Welcome!', fr: '{user} raid avec {viewers} spectateurs! Bienvenue!' },
    prompt: { en: 'CoolStreamer raided with 42 viewers', fr: 'CoolStreamer raid avec 42 spectateurs' },
    vars: [
      v('{user}', 'CoolStreamer', { en: 'Raider', fr: 'Raider' }, { en: "The raiding channel's display name.", fr: "Le nom d'affichage de la chaîne qui raid." }),
      v('{viewers}', '42', { en: 'Raid size', fr: 'Taille du raid' }, { en: 'How many viewers arrived.', fr: 'Le nombre de spectateurs arrivés.' }),
      ...DYNAMIC,
    ],
  },
  {
    id: 'shoutout',
    group: { en: 'Chat tools', fr: 'Outils de chat' },
    label: { en: 'Auto Shoutout', fr: 'Shoutout automatique' },
    dashPath: '/modules/shoutout',
    hint: { en: 'Auto Shoutout module → raid shoutout message.', fr: 'Module Shoutout automatique → message de shoutout.' },
    example: { en: 'Go follow {raider} → twitch.tv/{raider.login} · {viewers} friends came over!', fr: 'Allez suivre {raider} → twitch.tv/{raider.login} · {viewers} amis sont arrivés!' },
    prompt: { en: 'CoolStreamer raided the channel', fr: 'CoolStreamer a raid la chaîne' },
    vars: [
      v('{raider}', 'CoolStreamer', { en: 'Raider name', fr: 'Nom du raider' }, { en: 'Friendly display name of the raiding channel.', fr: "Nom d'affichage de la chaîne qui raid." }),
      v('{raider.login}', 'coolstreamer', { en: 'Raider login', fr: 'Login du raider' }, { en: 'URL-safe login, for twitch.tv/ links.', fr: 'Login compatible URL, pour les liens twitch.tv/.' }),
      v('{viewers}', '42', { en: 'Raid size', fr: 'Taille du raid' }, { en: 'How many viewers arrived.', fr: 'Le nombre de spectateurs arrivés.' }),
      ...DYNAMIC,
    ],
  },
  {
    id: 'triggers',
    group: { en: 'Chat tools', fr: 'Outils de chat' },
    label: { en: 'Trigger Words reply', fr: 'Réponse de mots déclencheurs' },
    dashPath: '/modules/triggers',
    hint: { en: 'Trigger Words module → the response side of a rule.', fr: 'Module Mots déclencheurs → la partie réponse d’une règle.' },
    example: { en: 'Hey {user}! {choice:Welcome in,Good to see you}!', fr: 'Salut {user}! {choice:Bienvenue,Contente de te voir}!' },
    prompt: { en: 'hello everyone', fr: 'bonjour tout le monde' },
    vars: [
      v('{user}', 'maya_live', { en: 'Chatter', fr: 'Auteur du message' }, { en: 'The person whose message matched the rule.', fr: 'La personne dont le message correspond à la règle.' }),
      ...DYNAMIC,
    ],
  },
  {
    id: 'clip',
    group: { en: 'Chat tools', fr: 'Outils de chat' },
    label: { en: '!clip reply', fr: 'Réponse de !clip' },
    dashPath: '/commands',
    hint: { en: 'Built-in !clip command → reply template (Commands page).', fr: 'Commande intégrée !clip → modèle de réponse (page Commandes).' },
    example: { en: '{user} clipped: {target} → {clip}', fr: '{user} a créé un clip: {target} → {clip}' },
    prompt: { en: '!clip That clutch', fr: '!clip Quel finish' },
    vars: [
      v('{clip}', 'clips.twitch.tv/FreshBagel', { en: 'Clip link', fr: 'Lien du clip' }, { en: 'The freshly created clip link.', fr: 'Le lien du clip fraîchement créé.' }),
      v('{user}', 'maya_live', { en: 'Clipper', fr: 'Créateur du clip' }, { en: 'The viewer who used !clip.', fr: 'La personne qui a utilisé !clip.' }),
      v('{target}', 'That clutch', { en: 'Clip title', fr: 'Titre du clip' }, { en: 'The title typed after !clip.', fr: 'Le titre tapé après !clip.' }),
    ],
  },
  {
    id: 'time',
    group: { en: 'Chat tools', fr: 'Outils de chat' },
    label: { en: '!time reply', fr: 'Réponse de !time' },
    dashPath: '/modules/time',
    hint: { en: 'Local Time module → !time reply.', fr: 'Module Heure locale → réponse de !time.' },
    example: { en: "It's {time} where I live ({timezone}).", fr: 'Il est {time} chez moi ({timezone}).' },
    prompt: { en: '!time', fr: '!time' },
    vars: [
      v('{time}', '2:30 PM', { en: 'Local time', fr: 'Heure locale' }, { en: 'The clock, in your configured timezone and format.', fr: "L'heure, selon votre fuseau et votre format." }),
      v('{date}', 'July 16', { en: 'Local date', fr: 'Date locale' }, { en: "Today's date in your timezone.", fr: "La date du jour dans votre fuseau." }),
      v('{timezone}', 'America/Montreal', { en: 'Timezone', fr: 'Fuseau horaire' }, { en: 'The configured timezone name.', fr: 'Le nom du fuseau configuré.' }),
      v('{user}', 'maya_live', { en: 'Asker', fr: 'Demandeur' }, { en: 'Who asked for the time.', fr: "Qui a demandé l'heure." }),
      ...DYNAMIC,
    ],
  },
  {
    id: 'channelpoints',
    group: { en: 'Chat tools', fr: 'Outils de chat' },
    label: { en: 'Channel Points reward reply', fr: 'Réponse de récompense (points de chaîne)' },
    dashPath: '/channelpoints',
    hint: { en: 'Channel Points page → the chat line a redemption posts.', fr: 'Page Points de chaîne → la ligne publiée lors d’un échange.' },
    example: { en: '{user} redeemed {reward} ({cost} pts): {input}', fr: '{user} a échangé {reward} ({cost} pts): {input}' },
    prompt: { en: 'maya_live redeemed Hydrate!', fr: 'maya_live a échangé Hydrate!' },
    vars: [
      v('{user}', 'maya_live', { en: 'Redeemer', fr: 'Échangeur' }, { en: 'Who redeemed the reward.', fr: 'Qui a échangé la récompense.' }),
      v('{input}', 'stay hydrated!', { en: 'Viewer input', fr: 'Texte du spectateur' }, { en: 'The text typed with the redemption (when the reward asks for one).', fr: "Le texte saisi avec l'échange (si la récompense en demande un)." }),
      v('{reward}', 'Hydrate!', { en: 'Reward title', fr: 'Titre de la récompense' }, { en: 'The name of the redeemed reward.', fr: 'Le nom de la récompense échangée.' }),
      v('{cost}', '500', { en: 'Point cost', fr: 'Coût en points' }, { en: 'What the reward costs.', fr: 'Le coût de la récompense.' }),
      v('{channel}', 'your_channel', { en: 'Channel', fr: 'Chaîne' }, { en: 'Your channel name.', fr: 'Le nom de votre chaîne.' }),
      v('{counter}', '129', { en: 'Bound counter', fr: 'Compteur lié' }, { en: "The bound counter's new value (when the reward has one).", fr: 'La nouvelle valeur du compteur lié (si la récompense en a un).' }),
      v('{points}', '50', { en: 'Loyalty points', fr: 'Points de fidélité' }, { en: 'Loyalty points the reward grants (when positive).', fr: 'Les points de fidélité accordés (si positifs).' }),
      ...DYNAMIC,
    ],
  },
  {
    id: 'queue-join',
    group: { en: 'Play Queue', fr: "File d'attente" },
    label: { en: 'Queue: join confirmation', fr: 'File: confirmation de !join' },
    dashPath: '/modules/queue',
    hint: { en: 'Play Queue module → the !join confirmation.', fr: "Module File d'attente → la confirmation de !join." },
    example: { en: '{user} joined the queue at spot #{pos}.', fr: '{user} rejoint la file en position #{pos}.' },
    prompt: { en: '!join', fr: '!join' },
    vars: [
      v('{user}', 'maya_live', { en: 'Joiner', fr: 'Participant' }, { en: 'Who joined the queue.', fr: 'Qui a rejoint la file.' }),
      v('{pos}', '3', { en: 'Queue position', fr: 'Position' }, { en: 'Their spot in line.', fr: 'Sa place dans la file.' }),
      ...DYNAMIC,
    ],
  },
  {
    id: 'queue-next',
    group: { en: 'Play Queue', fr: "File d'attente" },
    label: { en: 'Queue: next player up', fr: 'File: joueur suivant' },
    dashPath: '/modules/queue',
    hint: { en: 'Play Queue module → the !queue next announcement.', fr: "Module File d'attente → l'annonce de !queue next." },
    example: { en: "You're up, {target}! {count} waiting behind you.", fr: 'À toi, {target}! {count} personnes derrière toi.' },
    prompt: { en: '!queue next', fr: '!queue next' },
    vars: [
      v('{target}', 'alex', { en: 'Next player', fr: 'Joueur suivant' }, { en: 'Who is up next.', fr: 'La personne dont c’est le tour.' }),
      v('{count}', '2', { en: 'Still waiting', fr: 'Encore en attente' }, { en: 'How many people remain in line.', fr: 'Combien de personnes restent dans la file.' }),
      ...DYNAMIC,
    ],
  },
  {
    id: 'bw-session',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'Bedwars: !daily / !weekly / !monthly', fr: 'Bedwars: !daily / !weekly / !monthly' },
    dashPath: '/modules/urchin',
    hint: { en: 'Bedwars Stats module → session-stats reply.', fr: 'Module Stats Bedwars → réponse des stats de période.' },
    example: { en: '{player}: {wins}W {losses}L · {finals} finals · {beds} beds · {fkdr} FKDR', fr: '{player}: {wins}V {losses}D · {finals} finals · {beds} lits · {fkdr} FKDR' },
    prompt: { en: '!daily Technoblade', fr: '!daily Technoblade' },
    vars: [
      v('{player}', 'Technoblade', { en: 'Player', fr: 'Joueur' }, { en: 'The resolved Minecraft player.', fr: 'Le joueur Minecraft résolu.' }),
      v('{wins}', '5', { en: 'Wins', fr: 'Victoires' }, { en: 'Wins in the period.', fr: 'Victoires sur la période.' }),
      v('{losses}', '2', { en: 'Losses', fr: 'Défaites' }, { en: 'Losses in the period.', fr: 'Défaites sur la période.' }),
      v('{finals}', '21', { en: 'Final kills', fr: 'Final kills' }, { en: 'Final kills in the period.', fr: 'Final kills sur la période.' }),
      v('{finaldeaths}', '3', { en: 'Final deaths', fr: 'Morts finales' }, { en: 'Final deaths in the period.', fr: 'Morts finales sur la période.' }),
      v('{beds}', '9', { en: 'Beds broken', fr: 'Lits détruits' }, { en: 'Beds broken in the period.', fr: 'Lits détruits sur la période.' }),
      v('{games}', '8', { en: 'Games', fr: 'Parties' }, { en: 'Games played in the period.', fr: 'Parties jouées sur la période.' }),
      v('{levels}', '1', { en: 'Levels gained', fr: 'Niveaux gagnés' }, { en: 'Star levels gained.', fr: 'Niveaux d’étoile gagnés.' }),
      v('{fkdr}', '7.00', { en: 'FKDR', fr: 'FKDR' }, { en: 'Final kills divided by final deaths.', fr: 'Final kills divisés par les morts finales.' }),
      ...DYNAMIC,
    ],
  },
  {
    id: 'bwstats',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'Bedwars: !bwstats (lifetime)', fr: 'Bedwars: !bwstats (à vie)' },
    dashPath: '/modules/urchin',
    hint: { en: 'Bedwars Stats module → lifetime-stats reply.', fr: 'Module Stats Bedwars → réponse des stats à vie.' },
    example: { en: '{player}: {stars}✫ · {wins} wins · {fkdr} FKDR · {wlr} WLR', fr: '{player}: {stars}✫ · {wins} victoires · {fkdr} FKDR · {wlr} WLR' },
    prompt: { en: '!bwstats Technoblade', fr: '!bwstats Technoblade' },
    vars: [
      v('{player}', 'Technoblade', { en: 'Player', fr: 'Joueur' }, { en: 'The resolved Minecraft player.', fr: 'Le joueur Minecraft résolu.' }),
      v('{stars}', '402', { en: 'Stars', fr: 'Étoiles' }, { en: 'Bedwars star level.', fr: 'Niveau d’étoile Bedwars.' }),
      v('{wins}', '1000', { en: 'Wins', fr: 'Victoires' }, { en: 'Lifetime wins.', fr: 'Victoires à vie.' }),
      v('{losses}', '100', { en: 'Losses', fr: 'Défaites' }, { en: 'Lifetime losses.', fr: 'Défaites à vie.' }),
      v('{finals}', '5000', { en: 'Final kills', fr: 'Final kills' }, { en: 'Lifetime final kills.', fr: 'Final kills à vie.' }),
      v('{finaldeaths}', '500', { en: 'Final deaths', fr: 'Morts finales' }, { en: 'Lifetime final deaths.', fr: 'Morts finales à vie.' }),
      v('{beds}', '2000', { en: 'Beds broken', fr: 'Lits détruits' }, { en: 'Lifetime beds broken.', fr: 'Lits détruits à vie.' }),
      v('{fkdr}', '10.00', { en: 'FKDR', fr: 'FKDR' }, { en: 'Lifetime final K/D ratio.', fr: 'Ratio final K/D à vie.' }),
      v('{wlr}', '10.00', { en: 'Win/loss ratio', fr: 'Ratio V/D' }, { en: 'Lifetime win/loss ratio.', fr: 'Ratio victoires/défaites à vie.' }),
      ...DYNAMIC,
    ],
  },
  {
    id: 'sniper',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'Bedwars: !sniper', fr: 'Bedwars: !sniper' },
    dashPath: '/modules/urchin',
    hint: { en: 'Bedwars Stats module → sniper-score reply.', fr: 'Module Stats Bedwars → réponse du score sniper.' },
    example: { en: '{player} sniper score: {score} ({mode})', fr: '{player} score sniper: {score} ({mode})' },
    prompt: { en: '!sniper Technoblade', fr: '!sniper Technoblade' },
    vars: [
      v('{player}', 'Technoblade', { en: 'Player', fr: 'Joueur' }, { en: 'The resolved player.', fr: 'Le joueur résolu.' }),
      v('{score}', '7.5', { en: 'Sniper score', fr: 'Score sniper' }, { en: 'The current overlay score.', fr: 'Le score actuel.' }),
      v('{mode}', 'warn', { en: 'Mode', fr: 'Mode' }, { en: 'The current warning mode.', fr: "Le mode d'avertissement actuel." }),
      v('{tagcount}', '1', { en: 'Tag count', fr: 'Nombre de tags' }, { en: 'Active blacklist tags.', fr: 'Tags de liste noire actifs.' }),
      ...DYNAMIC,
    ],
  },
  {
    id: 'tags',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'Bedwars: !tag / !tagdescription', fr: 'Bedwars: !tag / !tagdescription' },
    dashPath: '/modules/urchin',
    hint: { en: 'Bedwars Stats module → tag-lookup replies.', fr: 'Module Stats Bedwars → réponses de recherche de tags.' },
    example: { en: '{player}: {tags}', fr: '{player}: {tags}' },
    prompt: { en: '!tag Technoblade', fr: '!tag Technoblade' },
    vars: [
      v('{player}', 'Technoblade', { en: 'Player', fr: 'Joueur' }, { en: 'The resolved player.', fr: 'Le joueur résolu.' }),
      v('{tags}', 'Blatant Cheater (Jul 3, 2024)', { en: 'Tags', fr: 'Tags' }, { en: 'The formatted tag list.', fr: 'La liste des tags formatée.' }),
      v('{tagcount}', '1', { en: 'Tag count', fr: 'Nombre de tags' }, { en: 'How many tags are active.', fr: 'Combien de tags sont actifs.' }),
      ...DYNAMIC,
    ],
  },
  {
    id: 'elo',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'MCSR: !elo', fr: 'MCSR: !elo' },
    dashPath: '/modules/mcsr',
    hint: { en: 'MCSR Ranked module → current-standing reply.', fr: 'Module MCSR Ranked → réponse du classement actuel.' },
    example: { en: '{player}: {elo} elo · rank #{rank} · {wins}W {losses}L', fr: '{player}: {elo} elo · rang #{rank} · {wins}V {losses}D' },
    prompt: { en: '!elo Feinberg', fr: '!elo Feinberg' },
    vars: [
      v('{player}', 'Feinberg', { en: 'Player', fr: 'Joueur' }, { en: 'The resolved player.', fr: 'Le joueur résolu.' }),
      v('{elo}', '1650', { en: 'Elo', fr: 'Elo' }, { en: 'Current rating.', fr: 'Le classement actuel.' }),
      v('{rank}', '12', { en: 'Rank', fr: 'Rang' }, { en: 'Leaderboard rank.', fr: 'Rang au classement.' }),
      v('{wins}', '40', { en: 'Wins', fr: 'Victoires' }, { en: 'Season wins.', fr: 'Victoires de la saison.' }),
      v('{losses}', '20', { en: 'Losses', fr: 'Défaites' }, { en: 'Season losses.', fr: 'Défaites de la saison.' }),
      v('{matches}', '60', { en: 'Matches', fr: 'Matchs' }, { en: 'Season matches.', fr: 'Matchs de la saison.' }),
      v('{country}', 'us', { en: 'Country', fr: 'Pays' }, { en: 'Country code.', fr: 'Code du pays.' }),
      ...DYNAMIC,
    ],
  },
  {
    id: 'mcsr-session',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'MCSR: !session', fr: 'MCSR: !session' },
    dashPath: '/modules/mcsr',
    hint: { en: 'MCSR Ranked module → this-stream session reply.', fr: 'Module MCSR Ranked → réponse de la session du stream.' },
    example: { en: '{player}: {elochange} elo ({elo} now) · {wins}W {losses}L in {matches} matches', fr: '{player}: {elochange} elo ({elo} maintenant) · {wins}V {losses}D en {matches} matchs' },
    prompt: { en: '!session', fr: '!session' },
    vars: [
      v('{player}', 'Feinberg', { en: 'Player', fr: 'Joueur' }, { en: 'The linked player.', fr: 'Le joueur lié.' }),
      v('{elo}', '1660', { en: 'Current Elo', fr: 'Elo actuel' }, { en: 'Rating right now.', fr: 'Le classement en ce moment.' }),
      v('{elochange}', '+24', { en: 'Elo change', fr: 'Variation Elo' }, { en: 'Change since the stream started.', fr: 'Variation depuis le début du stream.' }),
      v('{wins}', '3', { en: 'Wins', fr: 'Victoires' }, { en: 'Wins this stream.', fr: 'Victoires ce stream.' }),
      v('{losses}', '1', { en: 'Losses', fr: 'Défaites' }, { en: 'Losses this stream.', fr: 'Défaites ce stream.' }),
      v('{matches}', '4', { en: 'Matches', fr: 'Matchs' }, { en: 'Matches this stream.', fr: 'Matchs ce stream.' }),
      ...DYNAMIC,
    ],
  },
  {
    id: 'fn-stats',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'Fortnite: !fn / !fn season', fr: 'Fortnite: !fn / !fn season' },
    dashPath: '/modules/fortnite',
    hint: { en: 'Fortnite Stats module → lifetime & season replies.', fr: 'Module Stats Fortnite → réponses à vie et de saison.' },
    example: { en: '{player} ({window}): {wins} wins · {kd} K/D · {winrate}% winrate', fr: '{player} ({window}): {wins} victoires · {kd} K/D · {winrate}% de victoires' },
    prompt: { en: '!fn', fr: '!fn' },
    vars: [
      v('{player}', 'Ninja', { en: 'Player', fr: 'Joueur' }, { en: 'The linked account.', fr: 'Le compte lié.' }),
      v('{window}', 'lifetime', { en: 'Window', fr: 'Période' }, { en: 'Which stats window: lifetime or season.', fr: 'La période des stats: à vie ou saison.' }),
      v('{wins}', '301', { en: 'Wins', fr: 'Victoires' }, { en: 'Total wins.', fr: 'Total de victoires.' }),
      v('{matches}', '6232', { en: 'Matches', fr: 'Matchs' }, { en: 'Matches played.', fr: 'Matchs joués.' }),
      v('{kills}', '21679', { en: 'Kills', fr: 'Éliminations' }, { en: 'Total eliminations.', fr: 'Total d’éliminations.' }),
      v('{kd}', '3.66', { en: 'K/D', fr: 'K/D' }, { en: 'Kill/death ratio.', fr: 'Ratio éliminations/morts.' }),
      v('{winrate}', '4.83', { en: 'Win rate %', fr: 'Taux de victoire %' }, { en: 'Wins per hundred matches.', fr: 'Victoires par centaine de matchs.' }),
      v('{solowins}', '120', { en: 'Solo wins', fr: 'Victoires solo' }, { en: 'Wins in solos.', fr: 'Victoires en solo.' }),
      v('{solomatches}', '2400', { en: 'Solo matches', fr: 'Matchs solo' }, { en: 'Matches played in solos.', fr: 'Matchs joués en solo.' }),
      v('{solokd}', '3.2', { en: 'Solo K/D', fr: 'K/D solo' }, { en: 'Kill/death ratio in solos.', fr: 'Ratio éliminations/morts en solo.' }),
      v('{duowins}', '90', { en: 'Duo wins', fr: 'Victoires duo' }, { en: 'Wins in duos.', fr: 'Victoires en duo.' }),
      v('{duomatches}', '1900', { en: 'Duo matches', fr: 'Matchs duo' }, { en: 'Matches played in duos.', fr: 'Matchs joués en duo.' }),
      v('{duokd}', '3.8', { en: 'Duo K/D', fr: 'K/D duo' }, { en: 'Kill/death ratio in duos.', fr: 'Ratio éliminations/morts en duo.' }),
      v('{squadwins}', '91', { en: 'Squad wins', fr: 'Victoires squad' }, { en: 'Wins in squads.', fr: 'Victoires en squad.' }),
      v('{squadmatches}', '1932', { en: 'Squad matches', fr: 'Matchs squad' }, { en: 'Matches played in squads.', fr: 'Matchs joués en squad.' }),
      v('{squadkd}', '4.1', { en: 'Squad K/D', fr: 'K/D squad' }, { en: 'Kill/death ratio in squads.', fr: 'Ratio éliminations/morts en squad.' }),
      ...DYNAMIC,
    ],
  },
  {
    id: 'fn-store',
    group: { en: 'Game stats', fr: 'Stats de jeu' },
    label: { en: 'Fortnite: !fn store', fr: 'Fortnite: !fn store' },
    dashPath: '/modules/fortnite',
    hint: { en: 'Fortnite Stats module → item-shop reply.', fr: 'Module Stats Fortnite → réponse de la boutique.' },
    example: { en: 'Item shop {date} ({count} items): {items}', fr: 'Boutique du {date} ({count} objets): {items}' },
    prompt: { en: '!fn store', fr: '!fn store' },
    vars: [
      v('{date}', 'July 16', { en: 'Shop date', fr: 'Date de la boutique' }, { en: "Today's shop date.", fr: 'La date de la boutique du jour.' }),
      v('{count}', '24', { en: 'Item count', fr: "Nombre d'objets" }, { en: 'How many items are in the shop.', fr: "Combien d'objets sont en boutique." }),
      v('{items}', 'Renegade Raider, Skull Trooper, …', { en: 'Item list', fr: 'Liste des objets' }, { en: 'The featured items.', fr: 'Les objets en vedette.' }),
      ...DYNAMIC,
    ],
  },
];

// First-line chat actions (app/sesame/engine/slash.go). Values are prefixes
// prepended to the first response line.
export const STYLES: { value: string; label: L10n }[] = [
  { value: '', label: { en: 'Normal message', fr: 'Message normal' } },
  { value: '/me ', label: { en: 'Action (/me)', fr: 'Action (/me)' } },
  { value: '/announce ', label: { en: 'Announcement', fr: 'Annonce' } },
  { value: '/announceblue ', label: { en: 'Blue announcement', fr: 'Annonce bleue' } },
  { value: '/announcegreen ', label: { en: 'Green announcement', fr: 'Annonce verte' } },
  { value: '/announceorange ', label: { en: 'Orange announcement', fr: 'Annonce orange' } },
  { value: '/announcepurple ', label: { en: 'Purple announcement', fr: 'Annonce violette' } },
  { value: '/shoutout ', label: { en: 'Twitch shoutout', fr: 'Shoutout Twitch' } },
  { value: '/pin ', label: { en: 'Pin the message', fr: 'Épingler le message' } },
];

// Access levels, low → high (app/sesame/module/permission.go, dashboard PERMS).
export const PERMS: { value: string; label: L10n }[] = [
  { value: 'everyone', label: { en: 'Everyone', fr: 'Tout le monde' } },
  { value: 'sub', label: { en: 'Subscribers & up', fr: 'Abonnés et plus' } },
  { value: 'vip', label: { en: 'VIPs & up', fr: 'VIP et plus' } },
  { value: 'mod', label: { en: 'Moderators & up', fr: 'Modérateurs et plus' } },
  { value: 'lead_mod', label: { en: 'Lead moderators & up', fr: 'Modérateurs principaux et plus' } },
  { value: 'broadcaster', label: { en: 'Broadcaster only', fr: 'Diffuseur seulement' } },
];

// Validation limits, mirrored from internal/domain/validate/validate.go.
export const LIMITS = {
  nameMax: 64,
  aliasMax: 25,
  lineMax: 500,
  linesMax: 5,
  cooldownMax: 86400,
} as const;

export const DASHBOARD_ORIGIN = 'https://dashboard.itsbagelbot.com';

// UI copy for the builder page chrome, both locales.
const UI = {
  metaTitle: {
    en: 'Command Builder — ItsBagelBot',
    fr: 'Constructeur de commandes — ItsBagelBot',
  },
  metaDesc: {
    en: 'Build powerful ItsBagelBot commands without the syntax: click variables, watch a live chat rehearsal, then send the finished command straight to your dashboard.',
    fr: 'Créez des commandes ItsBagelBot puissantes sans la syntaxe: cliquez les variables, regardez la répétition en direct, puis envoyez la commande dans votre tableau de bord.',
  },
  eyebrow: { en: 'Command builder', fr: 'Constructeur de commandes' },
  title1: { en: 'Powerful commands.', fr: 'Des commandes puissantes.' },
  title2: { en: 'No syntax degree required.', fr: 'Aucun diplôme de syntaxe requis.' },
  lede: {
    en: 'Choose what you are writing, type the message normally, then click variables to add the smart parts. The rehearsal shows exactly what chat will see.',
    fr: 'Choisissez ce que vous écrivez, tapez le message normalement, puis cliquez les variables pour ajouter les parties intelligentes. La répétition montre exactement ce que le chat verra.',
  },
  step1Title: { en: 'What are you building?', fr: 'Que construisez-vous?' },
  modeCustom: { en: 'Custom command', fr: 'Commande personnalisée' },
  modeModule: { en: 'Module message', fr: 'Message de module' },
  surfaceLabel: { en: 'Exact message to customize', fr: 'Message exact à personnaliser' },
  nameLabel: { en: 'Command name', fr: 'Nom de la commande' },
  nameHint: { en: 'One word, no spaces. The ! is added for you.', fr: 'Un seul mot, sans espaces. Le ! est ajouté pour vous.' },
  aliasLabel: { en: 'Alternate names', fr: 'Autres noms' },
  aliasHint: { en: 'Optional, comma-separated. The same command answers to all of them.', fr: 'Optionnel, séparés par des virgules. La même commande répond à tous.' },
  permLabel: { en: 'Who can use it', fr: 'Qui peut l’utiliser' },
  cooldownLabel: { en: 'Cooldown (seconds)', fr: 'Délai (secondes)' },
  cooldownHint: { en: 'Shared by the whole chat. 0 = none.', fr: 'Partagé par tout le chat. 0 = aucun.' },
  styleLabel: { en: 'First-line style', fr: 'Style de la première ligne' },
  responseLabel: { en: 'Bot response', fr: 'Réponse du bot' },
  recipesLabel: { en: 'Quick starts', fr: 'Départs rapides' },
  moreOptions: { en: 'More options: access, cooldown, alternate names', fr: "Plus d'options: accès, délai, autres noms" },
  sendHelp: { en: 'Review the summary that opens, press Create, done.', fr: "Relisez le récapitulatif qui s'ouvre, appuyez sur Créer, c'est fait." },
  step3Title: { en: 'Make it dynamic', fr: 'Rendez-la dynamique' },
  step3Sub: { en: 'Click a variable to insert it at your cursor. Only variables that work here are shown.', fr: 'Cliquez une variable pour l’insérer au curseur. Seules les variables qui fonctionnent ici sont montrées.' },
  bracesSummary: { en: 'What do the braces mean?', fr: 'Que signifient les accolades?' },
  bracesBody: {
    en: 'A variable is a placeholder. Write "Hello {user}", and if Maya uses it, the bot says "Hello Maya". Keep both braces exactly as shown; an unknown variable is left as literal text.',
    fr: 'Une variable est un espace réservé. Écrivez «Bonjour {user}» et si Maya l’utilise, le bot dit «Bonjour Maya». Gardez les deux accolades telles quelles; une variable inconnue reste du texte littéral.',
  },
  previewTitle: { en: 'Live rehearsal', fr: 'Répétition en direct' },
  // Rehearsal chrome — word-for-word the dashboard's chatPreview catalog
  // (console/shared/lib/i18n/{en,fr}.ts), so the builder reads as the same
  // surface reaching out onto the marketing site.
  rehearsal: { en: 'Chat rehearsal', fr: 'Répétition du chat' },
  ariaTyping: { en: 'Bot is typing', fr: "Le bot est en train d'écrire" },
  announcement: { en: 'Announcement', fr: 'Annonce' },
  addMessageAfter: { en: '…add a message after {verb}', fr: '…ajoutez un message après {verb}' },
  shoutsOut: { en: 'Shouts out', fr: 'Fait un shoutout à' },
  nameChannel: { en: '…name a channel after /shoutout', fr: '…nommez une chaîne après /shoutout' },
  pinnedForStream: { en: 'Pinned until the stream ends', fr: 'Épinglé jusqu’à la fin du stream' },
  addActionAfterMe: { en: '…add an action after /me', fr: '…ajoutez une action après /me' },
  nothingToSay: { en: '…the bot has nothing to say yet', fr: "…le bot n'a rien à dire pour le moment" },
  unknownVar: { en: 'Unknown variable', fr: 'Variable inconnue' },
  sendTitle: { en: 'Send it to your dashboard', fr: 'Envoyez-la au tableau de bord' },
  sendCta: { en: 'Open in dashboard', fr: 'Ouvrir le tableau de bord' },
  copyTitleCustom: { en: 'Or paste it in chat', fr: 'Ou collez-la dans le chat' },
  copyTitleModule: { en: 'Copy the message template', fr: 'Copiez le modèle de message' },
  copyBodyModule: {
    en: 'Paste it into the matching field in your dashboard.',
    fr: 'Collez-le dans le champ correspondant du tableau de bord.',
  },
  copyCta: { en: 'Copy', fr: 'Copier' },
  copied: { en: 'Copied!', fr: 'Copié!' },
  copyFail: { en: 'Select and copy the text above', fr: 'Sélectionnez et copiez le texte ci-dessus' },
  openModule: { en: 'Open the module page', fr: 'Ouvrir la page du module' },
  statusName: { en: 'Check the name', fr: 'Vérifiez le nom' },
  statusLines: { en: 'Too many lines (max 5)', fr: 'Trop de lignes (max 5)' },
  statusLineLen: { en: 'A line is over 500 characters', fr: 'Une ligne dépasse 500 caractères' },
  statusEmpty: { en: 'Write a response', fr: 'Écrivez une réponse' },
  learnMore: {
    en: 'New to variables? Read the commands guide first.',
    fr: 'Les variables sont nouvelles pour vous? Lisez d’abord le guide des commandes.',
  },
  learnMoreCta: { en: 'Commands & variables guide', fr: 'Guide des commandes et variables' },
} as const;

type UIKeys = keyof typeof UI;

export function builderUI(lang: Lang): Record<UIKeys, string> {
  const out = {} as Record<UIKeys, string>;
  for (const key of Object.keys(UI) as UIKeys[]) out[key] = UI[key][lang];
  return out;
}

/** Everything the builder page needs, resolved for one locale. */
export function builderData(lang: Lang) {
  return {
    lang,
    limits: LIMITS,
    dashboardOrigin: DASHBOARD_ORIGIN,
    perms: PERMS.map((p) => ({ value: p.value, label: p.label[lang] })),
    styles: STYLES.map((s) => ({ value: s.value, label: s.label[lang] })),
    surfaces: SURFACES.map((s) => ({
      id: s.id,
      group: s.group[lang],
      label: s.label[lang],
      dashPath: s.dashPath,
      hint: s.hint[lang],
      example: s.example[lang],
      prompt: s.prompt[lang],
      vars: s.vars.map((x) => ({
        token: x.token,
        sample: x.sample,
        name: x.name[lang],
        desc: x.desc[lang],
      })),
    })),
  };
}

export type BuilderData = ReturnType<typeof builderData>;
