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
  'home.metaDesc': 'The all-in-one Twitch companion baked for independence. Automated moderation, custom commands, and engagement tools.',

  // ── Home hero (Header) ──────────────────────────────────────────
  'hero.topText': 'Freshly baked for Twitch chat',
  'hero.title1': 'Your Stream.',
  'hero.title2': 'Your Tools.',
  'hero.title3': 'Your Rules.',
  'hero.lede': 'ItsBagelBot is the all-in-one Twitch companion baked for independence. One setup, every tool. No corporate tracking, no recurring fees, no compromises.',
  'hero.devTitle': 'ItsBagelBot is in active development',
  'hero.devBody': 'The early-access is on the way. Join the discord for more updates.',
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
  'tier.premiumSuffix': '/month',
  'tier.premiumDesc': 'Keeps the servers warm and bumps you to the front of the queue when chat gets loud.',
  'tier.chipIn': 'Chip in',
  'tier.premiumFeat1': 'Everything in Free, obviously',
  'tier.premiumFeat2': 'Priority message processing queue',
  'tier.premiumFeat3': 'Faster command response times',
  'tier.premiumFeat4': 'Priority support',

  'tier.enterprise': 'Enterprise',
  'tier.enterpriseAmount': 'Custom',
  'tier.enterpriseDesc': 'Dedicated infrastructure for orgs running many channels at once.',
  'tier.enterpriseFeat1': 'Everything in Premium',
  'tier.enterpriseFeat2': 'Dedicated hardware',
  'tier.enterpriseFeat3': 'Multi-streamer management',
  'tier.enterpriseFeat4': 'Custom SLA & account manager',
  'tier.contactSales': 'Contact sales',
  'tier.oath': 'no feature gates · no trials · no card on file · cancelling is one click',

  // ── FAQ ─────────────────────────────────────────────────────────
  'faq.eyebrow': 'FAQ',
  'faq.title': 'Fair questions.',
  'faq.q1': 'Is the free plan really free?',
  'faq.a1': "Yes. Every feature, no limits, no expiry. We built this because good tools shouldn't depend on your budget, and we meant it.",
  'faq.q2': 'So what am I actually paying for with Premium?',
  'faq.a2': 'Priority. Premium messages jump the processing queue, so commands and moderation land faster when chat is busiest. It keeps the servers running too.',
  'faq.q3': 'Can I cancel Premium anytime?',
  'faq.a3': 'One click from the dashboard, no penalties, no exit survey. You keep the perks until the end of the billing period.',
  'faq.q4': "What's included in Enterprise?",
  'faq.a4': 'Dedicated hardware, multi-streamer management, custom SLAs, and direct engineering support. Write to enterprise@itsbagelbot.com for a quote.',
  'faq.q5': 'Is ItsBagelBot source available?',
  'faq.a5': 'ItsBagelBot is source available, not open source. You can read and audit every line on GitHub, but the license restricts redistribution and commercial use. Transparency without compromise.',

  // ── Contact page ────────────────────────────────────────────────
  'contact.eyebrow': 'Contact',
  'contact.title': 'Talk to a human.',
  'contact.desc': "No ticket portal, no chatbot maze. Pick a line, we're on the other end.",
  'contact.metaTitle': 'Contact - ItsBagelBot',
  'contact.metaDesc': 'Talk to a human. Community support on Discord, real email inboxes for support and enterprise, and the source on GitHub.',

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
  'steps.s2Body': 'Toggle the tools you want. Rules read like sentences, not regex.',
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
  'finale.honesty': "setup ≈ 2 minutes · uninstall anytime · we won't guilt-trip you",

  // ── Home: encryption ────────────────────────────────────────────
  'enc.p1Eyebrow': 'End-to-End Encryption',
  'enc.p1Title': 'Encrypted by design.',
  'enc.p1Desc': 'Your data stays private from origin to destination. No middlemen, no tracking, no compromise.',
  'enc.p2Eyebrow': 'Distributed Network',
  'enc.p2Title': 'Every node, every link, secured.',
  'enc.p2Desc': 'A web of independent services, each verifying the other. No single weak link.',
  'enc.p3Eyebrow': 'Powerful Architecture',
  'enc.p3Title': 'Built to withstand.',
  'enc.p3Desc': 'Built for optimization and throughput. Every system is engineered to maximize performance, efficiency and security with the latest enterprise standards.',
  'enc.p4Eyebrow': 'Yours · Always',
  'enc.p4Title': 'Yours, forever.',
  'enc.p4Desc': 'Source available, privacy-first. The way streaming infrastructure should be.',

  // ── Home: playground ────────────────────────────────────────────
  'play.eyebrow': 'No demo video. The real thing.',
  'play.title': 'Go on, poke it.',
  'play.sub': 'Past the cryptography, this is what your viewers actually meet: a bot sitting quietly in chat. Try a command. Send some spam. Watch what happens.',
  'play.sendSpam': 'send spam',
  'play.hint': 'The bot only speaks when spoken to. Chat stays yours.',
  'play.live': 'live',
  'play.inputPlaceholder': 'try !bagel',
  'play.inputAria': 'Type a chat message',
  'play.sendAria': 'Send message',
  'play.feedAria': 'Simulated Twitch chat',

  // ── Contact: switchboard ────────────────────────────────────────
  'board.l1Note': "The fast lane. The community usually answers before we're even awake.",
  'board.l1Hint': 'usually minutes',
  'board.l2Note': 'Account trouble, bug reports, or a question you want answered in writing.',
  'board.l3Note': 'Running many channels? Dedicated hardware and custom SLAs live here.',
  'board.l4Note': 'Read the source, audit the code, file issues where we track the work.',
  'board.l4Hint': 'source available',
  'board.note': 'Small team, real inboxes. We read everything, usually within a day. We do sleep sometimes.',
  'board.copy': 'Copy',
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
  'home.metaDesc': 'Le compagnon Twitch tout-en-un, conçu pour l\'indépendance. Modération automatisée, commandes personnalisées et outils d\'engagement.',

  'hero.topText': 'Fraîchement préparé pour le chat Twitch',
  'hero.title1': 'Votre Stream.',
  'hero.title2': 'Vos Outils.',
  'hero.title3': 'Vos Règles.',
  'hero.lede': 'ItsBagelBot est le compagnon Twitch tout-en-un, conçu pour l\'indépendance. Une seule configuration, tous les outils. Aucun pistage corporatif, aucun frais récurrent, aucun compromis.',
  'hero.devTitle': 'ItsBagelBot est en développement actif',
  'hero.devBody': 'L\'accès anticipé arrive bientôt. Rejoignez le Discord pour plus de nouvelles.',
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
  'tier.premiumSuffix': '/mois',
  'tier.premiumDesc': 'Garde les serveurs au chaud et vous fait passer en tête de file quand le chat s\'emballe.',
  'tier.chipIn': 'Participer',
  'tier.premiumFeat1': 'Tout ce qui est dans Gratuit, évidemment',
  'tier.premiumFeat2': 'File de traitement des messages prioritaire',
  'tier.premiumFeat3': 'Temps de réponse des commandes plus rapides',
  'tier.premiumFeat4': 'Support prioritaire',

  'tier.enterprise': 'Enterprise',
  'tier.enterpriseAmount': 'Sur mesure',
  'tier.enterpriseDesc': 'Infrastructure dédiée pour les organisations gérant plusieurs chaînes à la fois.',
  'tier.enterpriseFeat1': 'Tout ce qui est dans Premium',
  'tier.enterpriseFeat2': 'Matériel dédié',
  'tier.enterpriseFeat3': 'Gestion multi-streamers',
  'tier.enterpriseFeat4': 'SLA sur mesure et gestionnaire de compte',
  'tier.contactSales': 'Contacter les ventes',
  'tier.oath': 'aucune barrière de fonctionnalité · aucun essai · aucune carte enregistrée · l\'annulation se fait en un clic',

  'faq.eyebrow': 'FAQ',
  'faq.title': 'Questions légitimes.',
  'faq.q1': 'Le forfait gratuit est-il vraiment gratuit ?',
  'faq.a1': 'Oui. Chaque fonctionnalité, sans limites, sans expiration. Nous avons créé cela parce que de bons outils ne devraient pas dépendre de votre budget, et nous le pensons vraiment.',
  'faq.q2': 'Alors qu\'est-ce que je paie réellement avec Premium ?',
  'faq.a2': 'La priorité. Les messages Premium passent devant dans la file de traitement, donc les commandes et la modération arrivent plus vite quand le chat est le plus actif. Cela garde aussi les serveurs en marche.',
  'faq.q3': 'Puis-je annuler Premium à tout moment ?',
  'faq.a3': 'Un clic depuis le tableau de bord, aucune pénalité, aucun questionnaire de départ. Vous conservez les avantages jusqu\'à la fin de la période de facturation.',
  'faq.q4': 'Qu\'est-ce qui est inclus dans Enterprise ?',
  'faq.a4': 'Matériel dédié, gestion multi-streamers, SLA sur mesure et support technique direct. Écrivez à enterprise@itsbagelbot.com pour un devis.',
  'faq.q5': 'ItsBagelBot est-il à code source disponible ?',
  'faq.a5': 'ItsBagelBot est à source disponible, pas open source. Vous pouvez lire et auditer chaque ligne sur GitHub, mais la licence limite la redistribution et l\'usage commercial. La transparence sans compromis.',

  'contact.eyebrow': 'Contact',
  'contact.title': 'Parlez à un humain.',
  'contact.desc': 'Aucun portail de tickets, aucun dédale de chatbot. Choisissez une ligne, nous sommes à l\'autre bout.',
  'contact.metaTitle': 'Contact - ItsBagelBot',
  'contact.metaDesc': 'Parlez à un humain. Support communautaire sur Discord, de vraies boîtes e-mail pour le support et les entreprises, et le code sur GitHub.',

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
  'steps.s2Body': 'Activez les outils que vous voulez. Les règles se lisent comme des phrases, pas comme du regex.',
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
  'finale.honesty': 'installation ≈ 2 minutes · désinstallation à tout moment · aucune culpabilisation',

  // Home: encryption
  'enc.p1Eyebrow': 'Chiffrement de bout en bout',
  'enc.p1Title': 'Chiffré par conception.',
  'enc.p1Desc': 'Vos données restent privées de l\'origine à la destination. Aucun intermédiaire, aucun pistage, aucun compromis.',
  'enc.p2Eyebrow': 'Réseau distribué',
  'enc.p2Title': 'Chaque nœud, chaque lien, sécurisé.',
  'enc.p2Desc': 'Un réseau de services indépendants, chacun vérifiant l\'autre. Aucun maillon faible unique.',
  'enc.p3Eyebrow': 'Architecture robuste',
  'enc.p3Title': 'Conçu pour résister.',
  'enc.p3Desc': 'Conçu pour l\'optimisation et le débit. Chaque système est pensé pour maximiser la performance, l\'efficacité et la sécurité selon les derniers standards d\'entreprise.',
  'enc.p4Eyebrow': 'À vous · Toujours',
  'enc.p4Title': 'À vous, pour toujours.',
  'enc.p4Desc': 'Source disponible, priorité à la vie privée. L\'infrastructure de streaming telle qu\'elle devrait être.',

  // Home: playground
  'play.eyebrow': 'Pas de vidéo démo. La vraie chose.',
  'play.title': 'Allez-y, titillez-le.',
  'play.sub': 'Au-delà de la cryptographie, voici ce que vos spectateurs rencontrent vraiment : un bot posé tranquillement dans le chat. Essayez une commande. Envoyez du spam. Regardez ce qui se passe.',
  'play.sendSpam': 'envoyer du spam',
  'play.hint': 'Le bot ne parle que lorsqu\'on lui parle. Le chat reste le vôtre.',
  'play.live': 'en direct',
  'play.inputPlaceholder': 'essayez !bagel',
  'play.inputAria': 'Écrivez un message',
  'play.sendAria': 'Envoyer le message',
  'play.feedAria': 'Chat Twitch simulé',

  // Contact: switchboard
  'board.l1Note': 'La voie rapide. La communauté répond souvent avant même que nous soyons réveillés.',
  'board.l1Hint': 'généralement quelques minutes',
  'board.l2Note': 'Problème de compte, rapport de bug, ou une question à laquelle vous voulez une réponse écrite.',
  'board.l3Note': 'Vous gérez plusieurs chaînes ? Le matériel dédié et les SLA sur mesure, c\'est ici.',
  'board.l4Note': 'Lisez le code, auditez-le, signalez les problèmes là où nous suivons le travail.',
  'board.l4Hint': 'source disponible',
  'board.note': 'Petite équipe, vraies boîtes de réception. Nous lisons tout, généralement en un jour. Il nous arrive de dormir.',
  'board.copy': 'Copier',
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
