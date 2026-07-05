// Marketing-site i18n. A tiny hand-rolled catalog + helpers, matching the
// console's approach (no runtime dep). English is the source of truth; French
// falls back to it per-key, so a missing translation renders English, never a
// blank. Astro's own i18n routing (astro.config) owns the /fr/ URL prefix; this
// module owns the copy and the locale-aware link/switch helpers.

export const languages = { en: 'English', fr: 'Français' } as const;
export const defaultLang: Lang = 'en';
export type Lang = 'en' | 'fr';

const en = {
  // ── Nav / chrome ────────────────────────────────────────────────
  'nav.pricing': 'Pricing',
  'nav.docs': 'Docs',
  'nav.contact': 'Contact',
  'nav.cta': 'Add to Twitch',
  'nav.primary': 'Primary',
  'nav.home': 'ItsBagelBot home',
  'menu.meta': 'Freshly baked for Twitch chat',
  'menu.primary': 'Primary menu',
  'lang.switch': 'Language',
  'badge.incoming': 'Incoming feature',

  // ── Footer ──────────────────────────────────────────────────────
  'footer.signoff': 'Baked late. Served fresh.',
  'footer.signoffSub': 'See you in chat.',
  'footer.tagline': 'Your stream. Your rules.',
  'footer.product': 'Product',
  'footer.pricing': 'Pricing',
  'footer.docs': 'Documentation',
  'footer.dashboard': 'Dashboard',
  'footer.company': 'Company',
  'footer.contact': 'Contact',
  'footer.community': 'Community',
  'footer.note': 'No data sold · No trackers · No surprises',
  'footer.privacy': 'Privacy Policy',
  'footer.terms': 'Terms of Service',

  // ── Home ────────────────────────────────────────────────────────
  'home.metaTitle': 'ItsBagelBot. Your Stream, Your Rules',
  'home.metaDesc': 'The all-in-one Twitch companion baked for independence. Moderation that runs itself, custom commands, and tools that keep chat alive.',

  // ── Home hero (Header) ──────────────────────────────────────────
  'hero.topText': 'Freshly baked for Twitch chat',
  'hero.title1': 'Your Stream.',
  'hero.title2': 'Your Tools.',
  'hero.title3': 'Your Rules.',
  'hero.lede': 'ItsBagelBot is the all-in-one Twitch companion, baked for independence. One setup, every tool. No corporate tracking, no monthly fees, nothing held back.',
  'hero.devTitle': 'ItsBagelBot is still in the oven',
  'hero.devBody': 'Early access is on the way. Join the Discord and we will keep you posted.',
  'hero.devBadge': 'Alpha',
  'hero.badge1': 'Privacy-First',
  'hero.badge2': 'One-Time Setup',
  'hero.badge3': 'Eco-Friendly',
  'hero.badge4': 'Source Available',
  'hero.badge5': 'All Tools Included',

  // ── Pricing page ────────────────────────────────────────────────
  'pricing.eyebrow': 'Pricing',
  'pricing.title': 'Free is the whole product.',
  'pricing.desc': 'No gates, no trials, no asterisks. Premium exists for the people who want to keep the servers warm.',
  'pricing.metaTitle': 'Pricing - ItsBagelBot',
  'pricing.metaDesc': 'Free is the whole product. Premium is the tip jar with perks. Enterprise for organizations running many channels.',

  'tier.free': 'Free',
  'tier.freeSuffix': '/month, forever',
  'tier.freeDesc': "Not a trial. Not a teaser. The entire toolkit, because the best moderation shouldn't depend on your budget.",
  'tier.getStarted': 'Get started',
  'tier.freeFeat1': 'Chat moderation & auto-filters',
  'tier.freeFeat2': 'Custom commands & variables',
  'tier.freeFeat3': 'Song requests & queue',
  'tier.freeFeat4': 'Polls & giveaways',
  'tier.freeFeat5': 'Loyalty & engagement tools',
  'tier.freeFeat6': 'Analytics dashboard',
  'tier.freeFeat7': 'Community support via Discord',
  'tier.freeFeat8': 'Every future feature too',

  'tier.premium': 'Premium',
  'tier.premiumBadge': 'The tip jar, with perks',
  'tier.premiumSuffix': 'CAD +tx /month',
  'tier.premiumDesc': 'Keeps the servers warm and bumps you to the front of the queue when chat gets loud.',
  'tier.chipIn': 'Chip in',
  'tier.premiumFeat1': 'Everything in Free, obviously',
  'tier.premiumFeat2': 'Your messages jump to the front of the line',
  'tier.premiumFeat3': 'Faster command responses',
  'tier.premiumFeat4': 'Priority support',

  'tier.enterprise': 'Enterprise',
  'tier.enterpriseAmount': 'Custom',
  'tier.enterpriseDesc': 'Dedicated setup for teams running many channels at once.',
  'tier.enterpriseFeat1': 'Everything in Premium',
  'tier.enterpriseFeat2': 'Dedicated hardware',
  'tier.enterpriseFeat3': 'Multi-streamer management',
  'tier.enterpriseFeat4': 'Custom terms & a real account manager',
  'tier.contactSales': 'Contact sales',
  'tier.oath': 'no feature gates · no trials · no card on file · cancelling is one click',
  'tier.tebexNote': 'Secure checkout provided by Tebex.',

  // ── FAQ ─────────────────────────────────────────────────────────
  'faq.eyebrow': 'FAQ',
  'faq.title': 'Fair questions.',
  'faq.q1': 'Is the free plan really free?',
  'faq.a1': "Yes. Every feature, no limits, no expiry. We built this because good tools shouldn't depend on your budget, and we meant it.",
  'faq.q2': 'So what am I actually paying for with Premium?',
  'faq.a2': 'Priority. Premium messages jump to the front of the line, so commands and moderation land faster when chat is busiest. It keeps the servers running, too.',
  'faq.q3': 'Can I cancel Premium anytime?',
  'faq.a3': 'One click from the dashboard, no penalties, no exit survey. You keep the perks until the end of the billing period.',
  'faq.q4': "What's included in Enterprise?",
  'faq.a4': 'Dedicated hardware, multi-streamer management, custom terms, and direct engineering support. Write to enterprise@itsbagelbot.com for a quote.',
  'faq.q5': 'Is ItsBagelBot source available?',
  'faq.a5': 'ItsBagelBot is source available, not open source. You can read and audit every line on GitHub, but the license limits redistribution and commercial use. Transparency without the loopholes.',

  // ── Contact page ────────────────────────────────────────────────
  'contact.eyebrow': 'Contact',
  'contact.title': 'Talk to a human.',
  'contact.desc': "No ticket portal, no chatbot maze. Pick a line, we're on the other end.",
  'contact.metaTitle': 'Contact - ItsBagelBot',
  'contact.metaDesc': 'Talk to a human. Community support on Discord, real email inboxes for support and enterprise, and the source on GitHub.',

  // ── Home: game integrations ──────────────────────────────────────
  'games.eyebrow': 'Game Integrations',
  'games.title': 'Stats in chat, no alt-tabbing.',
  'games.bwTitle': 'Hypixel Bedwars Stats.',
  'games.bwDesc': 'Pull your Hypixel Bedwars stats right into chat.',
  'games.mcsrTitle': 'MCSR Ranked.',
  'games.mcsrDesc': 'Show off your Minecraft Speedrunning Elo and ranked stats instantly.',
  'games.reqTitle': 'Want another game?',
  'games.reqDesc': 'We are adding more. Open an issue on GitHub and tell us what you play.',

  // ── Home: quiet-work bento ──────────────────────────────────────
  'qw.eyebrow': 'The quiet work',
  'qw.title': 'While you play, it sweeps the floor.',
  'qw.modVerdict': '⌁ removed before anyone saw it',
  'qw.modTitle': 'It catches the noise before you do.',
  'qw.modDesc': 'Spam, slurs and scam links get folded away mid-sentence. You find out later, if you even want to.',
  'qw.cmdOut': 'welcome in, moth_lamp. grab a seat 🥯',
  'qw.cmdTitle': 'It greets your regulars by name.',
  'qw.cmdDesc': 'Commands, cooldowns and shout-outs that sound like you wrote them. Because you did, once.',
  'qw.eqTitle': 'No dead air.',
  'qw.eqDesc': 'Requests queue themselves. Skips behave.',
  'qw.pollTitle': 'Chat decides. It counts.',
  'qw.pollDesc': 'Polls tallied live, no napkin math.',
  'qw.winLabel': 'winner',
  'qw.winTitle': 'Fair means random.',
  'qw.winDesc': 'Giveaways drawn honestly, receipts kept.',
  'qw.beatTitle': "Awake so you don't have to be.",
  'qw.beatDesc': 'It holds the room between streams too.',

  // ── Home: steps ─────────────────────────────────────────────────
  'steps.eyebrow': 'Two minutes, start to live',
  'steps.title': 'Three steps. None of them hard.',
  'steps.s1Title': 'Connect your channel.',
  'steps.s1Body': 'One click through Twitch. No API keys, no config files, no wizard with nine screens.',
  'steps.s2Title': 'Make it yours.',
  'steps.s2Body': 'Turn on the tools you want. Rules read like sentences, not code.',
  'steps.s2Receipt': 'modules: 6 active · rules: yours',
  'steps.s3Title': 'Go live, and breathe.',
  'steps.s3Body': 'It joins your chat quietly and gets to work. You get to just stream.',
  'steps.s3Receipt': 'itsbagelbot joined #your_channel',

  // ── Home: letter ────────────────────────────────────────────────
  'letter.meta2': '02:14 · draft 47',
  'letter.line1': 'We started building this after a stream where four tools broke in one night. Chat scrolled past faster than two hands could moderate, the song queue died, and the giveaway picked nobody.',
  'letter.line2': "Every feature in here exists because one of us needed it at 2 a.m. and couldn't find it, couldn't afford it, or didn't trust where the data went.",
  'letter.line3': "So: no trackers, no data sold, no feature held hostage. If it makes one stream calmer, it's doing its job.",
  'letter.signature': 'the folks behind the bagel',
  'letter.stampAria': 'Sprinkle sesame seeds',
  'letter.ps': 'p.s. click the stamp.',

  // ── Home: finale ────────────────────────────────────────────────
  'finale.eyebrow': "The oven's warm",
  'finale.title': 'Set it up tonight. Stream calmer tomorrow.',
  'finale.sub': 'Free means free. Every tool, no card, no countdown timers.',
  'finale.seePricing': 'See pricing',
  'finale.honesty': 'setup takes about 2 minutes · uninstall anytime · no guilt trips',

  // ── Home: encryption ────────────────────────────────────────────
  'enc.p1Eyebrow': 'Private by default',
  'enc.p1Title': 'Locked from the start.',
  'enc.p1Desc': 'What you say stays between you and your chat. Nothing sits in the middle reading along, and nothing gets sold.',
  'enc.p2Eyebrow': 'Spread out, on purpose',
  'enc.p2Title': 'No single point to break.',
  'enc.p2Desc': 'The work is shared across small pieces that keep an eye on each other. If one stumbles, the rest keep your stream going.',
  'enc.p3Eyebrow': 'Built to last',
  'enc.p3Title': "Steady when it's loud.",
  'enc.p3Desc': 'It stays quick and calm when chat is at its wildest. Busy nights are exactly what it was made for.',
  'enc.p4Eyebrow': 'Yours, always',
  'enc.p4Title': 'Yours, for keeps.',
  'enc.p4Desc': 'The code is out in the open and the calls are yours to make. This is how a stream tool should treat you.',

  // ── Home: playground ────────────────────────────────────────────
  'play.eyebrow': 'No demo video. The real thing.',
  'play.title': 'Go on, poke it.',
  'play.sub': "Past all that, here's what your viewers actually meet: a bot sitting quietly in chat. Try a command, send some spam, and see what it does.",
  'play.sendSpam': 'send spam',
  'play.hint': 'The bot only speaks when spoken to. Chat stays yours.',
  'play.live': 'live',
  'play.inputPlaceholder': 'try !bagel',
  'play.inputAria': 'Type a chat message',
  'play.sendAria': 'Send message',
  'play.feedAria': 'Simulated Twitch chat',
  // Playground simulated-chat strings (read client-side via a JSON island)
  'play.replyBagel': '@you 🥯 fresh from the oven. one warm bagel, zero calories.',
  'play.replySong': "now playing · 'late night lo-fi' · lined up for @you",
  'play.replyLurk': "@you settled in to lurk. your seat's saved, I'll wave when you're back.",
  'play.replyHelp': 'try !bagel, !song, !poll or !lurk. everything else you set up in the dashboard.',
  'play.pollUp': 'poll is up. chat, vote with 1 or 2.',
  'play.pollQ': 'poll · one more game?',
  'play.pollYes': 'yes',
  'play.pollAlways': 'always',
  'play.spamRemoved': '⌁ removed · link spam · 0 viewers saw it',
  'play.quiet': 'the bot stays quiet unless you call it. chat is yours.',
  'play.unknown': "@you I don't know {cmd} yet. you could teach it in the dashboard in about ten seconds.",
  'play.seedTry': 'try typing !bagel',
  // Ambient viewer chatter (the spam lines stay English, they are scam bait)
  'play.amb1': 'this soundtrack is so good',
  'play.amb2': 'LETS GOOO',
  'play.amb3': 'first time here, vibes are great',
  'play.amb4': 'anyone else hungry now',
  'play.amb5': 'that jump was clean',
  'play.amb6': 'chat moving fast tonight',
  'play.amb7': 'ok that was actually smooth',
  'play.amb8': 'gl on the next run',
  'play.amb9': 'the bot caught that fast lol',

  // ── Contact: switchboard ────────────────────────────────────────
  'board.l1Note': "The fast lane. The community usually answers before we're even awake.",
  'board.l1Hint': 'usually minutes',
  'board.l2Note': 'Account trouble, a bug, or a question you want answered in writing.',
  'board.l3Note': 'Running many channels? Dedicated hardware and custom terms live here.',
  'board.l4Note': 'Read the source, audit the code, file issues where we track the work.',
  'board.l4Hint': 'source available',
  'board.note': 'Small team, real inboxes. We read everything, usually within a day. We do sleep sometimes.',
  'board.copy': 'Copy',

  // ── Eco Friendly ────────────────────────────────────────────────
  'eco.eyebrow': 'Eco-Friendly',
  'eco.title': 'Powered by Sustainable Energy.',
  'eco.desc': 'We run entirely on hydro-electricity and hyper-optimized code to minimize hardware overhead. This makes our website cleaner than 92% of all pages tested, and the dashboard cleaner than 95%.',
} as const;

export type UIKey = keyof typeof en;

const fr: Partial<Record<UIKey, string>> = {
  'nav.pricing': 'Tarifs',
  'nav.docs': 'Docs',
  'nav.contact': 'Contact',
  'nav.cta': 'Ajouter à Twitch',
  'nav.primary': 'Principal',
  'nav.home': 'Accueil ItsBagelBot',
  'menu.meta': 'Fraîchement préparé pour le chat Twitch',
  'menu.primary': 'Menu principal',
  'lang.switch': 'Langue',
  'badge.incoming': 'Fonctionnalité à venir',

  'footer.signoff': 'Préparé tard. Servi frais.',
  'footer.signoffSub': 'À bientôt dans le chat.',
  'footer.tagline': 'Votre stream. Vos règles.',
  'footer.product': 'Produit',
  'footer.pricing': 'Tarifs',
  'footer.docs': 'Documentation',
  'footer.dashboard': 'Tableau de bord',
  'footer.company': 'Entreprise',
  'footer.contact': 'Contact',
  'footer.community': 'Communauté',
  'footer.note': 'Aucune donnée vendue · Aucun traceur · Aucune surprise',
  'footer.privacy': 'Politique de confidentialité',
  'footer.terms': "Conditions d'utilisation",

  'home.metaTitle': 'ItsBagelBot. Votre stream, vos règles',
  'home.metaDesc': 'Le compagnon Twitch tout-en-un, conçu pour l\'indépendance. Une modération qui tourne toute seule, des commandes sur mesure et des outils qui gardent le chat vivant.',

  'hero.topText': 'Fraîchement préparé pour le chat Twitch',
  'hero.title1': 'Votre Stream.',
  'hero.title2': 'Vos Outils.',
  'hero.title3': 'Vos Règles.',
  'hero.lede': 'ItsBagelBot est le compagnon Twitch tout-en-un, conçu pour l\'indépendance. Une seule configuration, tous les outils. Aucun pistage, aucun frais mensuel, rien qui reste au placard.',
  'hero.devTitle': 'ItsBagelBot est encore au four',
  'hero.devBody': 'L\'accès anticipé arrive bientôt. Rejoignez le Discord, on vous tient au courant.',
  'hero.devBadge': 'Alpha',
  'hero.badge1': 'Priorité à la vie privée',
  'hero.badge2': 'Configuration unique',
  'hero.badge3': 'Écoresponsable',
  'hero.badge4': 'Source disponible',
  'hero.badge5': 'Tous les outils inclus',

  'pricing.eyebrow': 'Tarifs',
  'pricing.title': 'Le gratuit, c\'est tout le produit.',
  'pricing.desc': 'Aucune barrière, aucun essai, aucun astérisque. Premium existe pour ceux qui veulent garder les serveurs au chaud.',
  'pricing.metaTitle': 'Tarifs - ItsBagelBot',
  'pricing.metaDesc': 'Le gratuit, c\'est tout le produit. Premium est la cagnotte avec des avantages. Enterprise pour les organisations gérant plusieurs chaînes.',

  'tier.free': 'Gratuit',
  'tier.freeSuffix': '/mois, pour toujours',
  'tier.freeDesc': 'Pas un essai. Pas un avant-goût. La boîte à outils complète, parce que la meilleure modération ne devrait pas dépendre de votre budget.',
  'tier.getStarted': 'Commencer',
  'tier.freeFeat1': 'Modération du chat et filtres automatiques',
  'tier.freeFeat2': 'Commandes et variables personnalisées',
  'tier.freeFeat3': 'Demandes de musique et file d\'attente',
  'tier.freeFeat4': 'Sondages et concours',
  'tier.freeFeat5': 'Outils de fidélité et d\'engagement',
  'tier.freeFeat6': 'Tableau de bord d\'analyses',
  'tier.freeFeat7': 'Support communautaire via Discord',
  'tier.freeFeat8': 'Chaque future fonctionnalité aussi',

  'tier.premium': 'Premium',
  'tier.premiumBadge': 'La cagnotte, avec des avantages',
  'tier.premiumSuffix': 'CAD +tx /mois',
  'tier.premiumDesc': 'Garde les serveurs au chaud et vous fait passer en tête de file quand le chat s\'emballe.',
  'tier.chipIn': 'Participer',
  'tier.premiumFeat1': 'Tout ce qui est dans Gratuit, évidemment',
  'tier.premiumFeat2': 'Vos messages passent devant tout le monde',
  'tier.premiumFeat3': 'Des commandes qui répondent plus vite',
  'tier.premiumFeat4': 'Support prioritaire',

  'tier.enterprise': 'Enterprise',
  'tier.enterpriseAmount': 'Sur mesure',
  'tier.enterpriseDesc': 'Installation dédiée pour les équipes qui gèrent plusieurs chaînes à la fois.',
  'tier.enterpriseFeat1': 'Tout ce qui est dans Premium',
  'tier.enterpriseFeat2': 'Matériel dédié',
  'tier.enterpriseFeat3': 'Gestion multi-streamers',
  'tier.enterpriseFeat4': 'Conditions sur mesure et un vrai gestionnaire de compte',
  'tier.contactSales': 'Contacter les ventes',
  'tier.oath': 'aucune barrière de fonctionnalité · aucun essai · aucune carte enregistrée · l\'annulation se fait en un clic',
  'tier.tebexNote': 'Paiement sécurisé fourni par Tebex.',

  'faq.eyebrow': 'FAQ',
  'faq.title': 'Questions légitimes.',
  'faq.q1': 'Le forfait gratuit est-il vraiment gratuit ?',
  'faq.a1': 'Oui. Chaque fonctionnalité, sans limites, sans expiration. Nous avons créé cela parce que de bons outils ne devraient pas dépendre de votre budget, et nous le pensons vraiment.',
  'faq.q2': 'Alors qu\'est-ce que je paie réellement avec Premium ?',
  'faq.a2': 'La priorité. Les messages Premium passent devant tout le monde, donc les commandes et la modération arrivent plus vite quand le chat est au plus fort. Ça garde aussi les serveurs en marche.',
  'faq.q3': 'Puis-je annuler Premium à tout moment ?',
  'faq.a3': 'Un clic depuis le tableau de bord, aucune pénalité, aucun questionnaire de départ. Vous conservez les avantages jusqu\'à la fin de la période de facturation.',
  'faq.q4': 'Qu\'est-ce qui est inclus dans Enterprise ?',
  'faq.a4': 'Matériel dédié, gestion multi-streamers, conditions sur mesure et support technique direct. Écrivez à enterprise@itsbagelbot.com pour un devis.',
  'faq.q5': 'ItsBagelBot est-il à code source disponible ?',
  'faq.a5': 'ItsBagelBot est à source disponible, pas open source. Vous pouvez lire et auditer chaque ligne sur GitHub, mais la licence limite la redistribution et l\'usage commercial. La transparence, sans les failles.',

  'contact.eyebrow': 'Contact',
  'contact.title': 'Parlez à un humain.',
  'contact.desc': 'Aucun portail de tickets, aucun dédale de chatbot. Choisissez une ligne, nous sommes à l\'autre bout.',
  'contact.metaTitle': 'Contact - ItsBagelBot',
  'contact.metaDesc': 'Parlez à un humain. Support communautaire sur Discord, de vraies boîtes e-mail pour le support et les entreprises, et le code sur GitHub.',

  // Home: game integrations
  'games.eyebrow': 'Intégrations de jeux',
  'games.title': 'Stats dans le chat, sans alt-tab.',
  'games.bwTitle': 'Stats Hypixel Bedwars.',
  'games.bwDesc': 'Affichez vos stats Hypixel Bedwars directement dans le chat.',
  'games.mcsrTitle': 'MCSR Ranked.',
  'games.mcsrDesc': 'Montrez instantanément votre Elo de speedrun Minecraft et vos stats classées.',
  'games.reqTitle': 'Vous voulez un autre jeu ?',
  'games.reqDesc': 'Nous en ajoutons d\'autres. Ouvrez une issue sur GitHub et dites-nous à quoi vous jouez.',

  // Home: quiet-work bento
  'qw.eyebrow': 'Le travail discret',
  'qw.title': 'Pendant que vous jouez, il balaie le sol.',
  'qw.modVerdict': '⌁ supprimé avant que quiconque le voie',
  'qw.modTitle': 'Il attrape le bruit avant vous.',
  'qw.modDesc': 'Le spam, les insultes et les liens frauduleux sont escamotés en pleine phrase. Vous l\'apprenez plus tard, si vous le voulez.',
  'qw.cmdOut': 'bienvenue, moth_lamp. installe-toi 🥯',
  'qw.cmdTitle': 'Il accueille vos habitués par leur nom.',
  'qw.cmdDesc': 'Commandes, délais et shout-outs qui sonnent comme si vous les aviez écrits. Parce que vous l\'avez fait, une fois.',
  'qw.eqTitle': 'Jamais de silence.',
  'qw.eqDesc': 'Les demandes s\'enchaînent toutes seules. Les passages restent sages.',
  'qw.pollTitle': 'Le chat décide. Il compte.',
  'qw.pollDesc': 'Sondages comptés en direct, sans calcul de coin de table.',
  'qw.winLabel': 'gagnant',
  'qw.winTitle': 'Équitable veut dire aléatoire.',
  'qw.winDesc': 'Tirages faits honnêtement, preuves conservées.',
  'qw.beatTitle': 'Éveillé pour que vous n\'ayez pas à l\'être.',
  'qw.beatDesc': 'Il tient la salle entre les streams aussi.',

  // Home: steps
  'steps.eyebrow': 'Deux minutes, du début au direct',
  'steps.title': 'Trois étapes. Aucune n\'est difficile.',
  'steps.s1Title': 'Connectez votre chaîne.',
  'steps.s1Body': 'Un clic via Twitch. Aucune clé API, aucun fichier de config, aucun assistant à neuf écrans.',
  'steps.s2Title': 'Faites-le vôtre.',
  'steps.s2Body': 'Activez les outils que vous voulez. Les règles se lisent comme des phrases, pas comme du code.',
  'steps.s2Receipt': 'modules : 6 actifs · règles : les vôtres',
  'steps.s3Title': 'Passez en direct, et respirez.',
  'steps.s3Body': 'Il rejoint votre chat discrètement et se met au travail. Vous, vous n\'avez qu\'à streamer.',
  'steps.s3Receipt': 'itsbagelbot a rejoint #your_channel',

  // Home: letter
  'letter.meta2': '02:14 · brouillon 47',
  'letter.line1': 'Nous avons commencé à construire ceci après un stream où quatre outils ont lâché en une soirée. Le chat défilait plus vite que deux mains ne pouvaient modérer, la file de musique est morte, et le concours n\'a choisi personne.',
  'letter.line2': 'Chaque fonctionnalité ici existe parce que l\'un de nous en a eu besoin à 2 h du matin et ne l\'a pas trouvée, ne pouvait pas se la payer, ou ne faisait pas confiance à l\'endroit où allaient les données.',
  'letter.line3': 'Donc : aucun traceur, aucune donnée vendue, aucune fonctionnalité prise en otage. Si cela rend un seul stream plus serein, il fait son travail.',
  'letter.signature': 'l\'équipe derrière le bagel',
  'letter.stampAria': 'Parsemez des graines de sésame',
  'letter.ps': 'p.-s. cliquez sur le tampon.',

  // Home: finale
  'finale.eyebrow': 'Le four est chaud',
  'finale.title': 'Installez-le ce soir. Streamez plus sereinement demain.',
  'finale.sub': 'Gratuit veut dire gratuit. Chaque outil, sans carte, sans compte à rebours.',
  'finale.seePricing': 'Voir les tarifs',
  'finale.honesty': 'installation en 2 minutes environ · désinstallation quand vous voulez · aucune culpabilisation',

  // Home: encryption
  'enc.p1Eyebrow': 'Privé par défaut',
  'enc.p1Title': 'Protégé dès le départ.',
  'enc.p1Desc': 'Ce que vous dites reste entre vous et votre chat. Personne au milieu ne lit par-dessus votre épaule, et rien n\'est revendu.',
  'enc.p2Eyebrow': 'Réparti, volontairement',
  'enc.p2Title': 'Aucun point de rupture unique.',
  'enc.p2Desc': 'Le travail est partagé entre de petites pièces qui se surveillent. Si l\'une flanche, les autres gardent votre stream en vie.',
  'enc.p3Eyebrow': 'Fait pour durer',
  'enc.p3Title': 'Solide quand ça chauffe.',
  'enc.p3Desc': 'Ça reste rapide et calme même quand le chat s\'emballe. Les grosses soirées, c\'est exactement ce pour quoi c\'est fait.',
  'enc.p4Eyebrow': 'À vous, toujours',
  'enc.p4Title': 'À vous, pour de bon.',
  'enc.p4Desc': 'Le code est à ciel ouvert et les décisions vous reviennent. Voilà comment un outil de stream devrait vous traiter.',

  // Home: playground
  'play.eyebrow': 'Pas de vidéo démo. La vraie chose.',
  'play.title': 'Allez-y, titillez-le.',
  'play.sub': 'Après tout ça, voici ce que vos spectateurs rencontrent vraiment : un bot posé tranquillement dans le chat. Tapez une commande, envoyez du spam, et regardez ce qu\'il fait.',
  'play.sendSpam': 'envoyer du spam',
  'play.hint': 'Le bot ne parle que lorsqu\'on lui parle. Le chat reste le vôtre.',
  'play.live': 'en direct',
  'play.inputPlaceholder': 'essayez !bagel',
  'play.inputAria': 'Écrivez un message',
  'play.sendAria': 'Envoyer le message',
  'play.feedAria': 'Chat Twitch simulé',
  'play.replyBagel': '@toi 🥯 tout chaud, sorti du four. un bagel pour toi, zéro calorie.',
  'play.replySong': 'à l\'écoute · \'late night lo-fi\' · en file pour @toi',
  'play.replyLurk': '@toi te voilà en mode lurk. ta place est gardée, je te fais signe à ton retour.',
  'play.replyHelp': 'essaie !bagel, !song, !poll ou !lurk. le reste, ça se règle dans le tableau de bord.',
  'play.pollUp': 'sondage lancé. chat, votez avec 1 ou 2.',
  'play.pollQ': 'sondage · encore une partie ?',
  'play.pollYes': 'oui',
  'play.pollAlways': 'toujours',
  'play.spamRemoved': '⌁ retiré · lien de spam · 0 spectateur ne l\'a vu',
  'play.quiet': 'le bot reste tranquille tant que tu ne l\'appelles pas. le chat est à toi.',
  'play.unknown': '@toi je ne connais pas encore {cmd}. tu pourrais me l\'apprendre dans le tableau de bord en dix secondes.',
  'play.seedTry': 'essaie de taper !bagel',
  'play.amb1': 'cette bande-son est trop bonne',
  'play.amb2': 'ALLEZ',
  'play.amb3': 'première fois ici, super ambiance',
  'play.amb4': 'quelqu\'un a faim maintenant ou c\'est que moi',
  'play.amb5': 'ce saut était nickel',
  'play.amb6': 'ça défile vite dans le chat ce soir',
  'play.amb7': 'ok ça c\'était vraiment fluide',
  'play.amb8': 'bonne chance pour la prochaine',
  'play.amb9': 'le bot a chopé ça vite lol',

  // Contact: switchboard
  'board.l1Note': 'La voie rapide. La communauté répond souvent avant même que nous soyons réveillés.',
  'board.l1Hint': 'généralement quelques minutes',
  'board.l2Note': 'Un souci de compte, un bug, ou une question à laquelle vous voulez une réponse écrite.',
  'board.l3Note': 'Vous gérez plusieurs chaînes ? Le matériel dédié et les conditions sur mesure, c\'est ici.',
  'board.l4Note': 'Lisez le code, auditez-le, signalez les problèmes là où nous suivons le travail.',
  'board.l4Hint': 'source disponible',
  'board.note': 'Petite équipe, vraies boîtes de réception. Nous lisons tout, généralement en un jour. Il nous arrive de dormir.',
  'board.copy': 'Copier',

  'eco.eyebrow': 'Écoresponsable',
  'eco.title': 'Alimenté par une énergie durable.',
  'eco.desc': 'Nous fonctionnons entièrement à l\'hydroélectricité et avec un code hyper-optimisé pour minimiser la consommation. Cela rend notre site web plus propre que 92 % des pages testées, et notre tableau de bord plus propre que 95 %.',
};

const catalog: Record<Lang, Partial<Record<UIKey, string>>> = { en, fr };

/** Locale from the URL: /fr/... → 'fr', anything else → 'en'. */
export function getLangFromUrl(url: URL): Lang {
  const seg = url.pathname.split('/')[1];
  return seg === 'fr' ? 'fr' : 'en';
}

/** Bound translator for a locale, English fallback per key. */
export function useTranslations(lang: Lang) {
  return function t(key: UIKey): string {
    return catalog[lang][key] ?? en[key] ?? key;
  };
}

/**
 * Prefix an internal path with the active locale. External URLs (http…, mailto,
 * anchors) and the default locale pass through untouched.
 */
export function localizePath(path: string, lang: Lang): string {
  if (lang === defaultLang || !path.startsWith('/')) return path;
  if (path === '/') return '/fr/';
  return `/fr${path}`;
}

/** The opposite locale of the given one. For the language toggle. */
export function otherLang(lang: Lang): Lang {
  return lang === 'fr' ? 'en' : 'fr';
}
