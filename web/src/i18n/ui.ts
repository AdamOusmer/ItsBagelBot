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
  'hero.lede': 'ItsBagelBot is the all-in-one Twitch companion baked for independence. One setup, every tool — no corporate tracking, no recurring fees, no compromises.',
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
  'pricing.metaTitle': 'Pricing — ItsBagelBot',
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
  'contact.metaTitle': 'Contact — ItsBagelBot',
  'contact.metaDesc': 'Talk to a human. Community support on Discord, real email inboxes for support and enterprise, and the source on GitHub.',
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
  'hero.lede': 'ItsBagelBot est le compagnon Twitch tout-en-un, conçu pour l\'indépendance. Une seule configuration, tous les outils — aucun pistage corporatif, aucun frais récurrent, aucun compromis.',
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
  'pricing.metaTitle': 'Tarifs — ItsBagelBot',
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
  'contact.metaTitle': 'Contact — ItsBagelBot',
  'contact.metaDesc': 'Parlez à un humain. Support communautaire sur Discord, de vraies boîtes e-mail pour le support et les entreprises, et le code sur GitHub.',
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

/** The opposite locale of the given one — for the language toggle. */
export function otherLang(lang: Lang): Lang {
  return lang === 'fr' ? 'en' : 'fr';
}
