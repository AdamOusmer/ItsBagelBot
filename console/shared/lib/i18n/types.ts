// The console i18n surface. Locales are a closed set so detection, the cookie
// codec and the switcher all agree on what "fr" means; add a locale here, ship a
// catalog for it, and every app picks it up.
export type Locale = 'en' | 'fr';

// A message catalog is a tree of namespaces. Leaves are strings (with optional
// {name} placeholders resolved by translate()) or string[] for the few ordered
// lists (feature bullets, headline words) that a component renders in sequence.
export type MessageTree = { [key: string]: string | string[] | MessageTree };
