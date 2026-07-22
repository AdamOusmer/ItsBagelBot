// The console i18n surface. A locale is any catalog shipped in this package: the
// closed set is not a type union any more but the data itself — the JSON files in
// lib/i18n/locales/ (frontend) and the internal/domain/i18n/locales.json manifest
// (backend). Adding a language is a data-only change: drop en/fr-shaped JSON, no
// code or type edit. Runtime code narrows arbitrary strings to a real locale via
// isLocale() (see messages.ts).
export type Locale = string;

// A message catalog is a tree of namespaces. Leaves are strings (with optional
// {name} placeholders resolved by translate()) or string[] for the few ordered
// lists (feature bullets, headline words) that a component renders in sequence.
export type MessageTree = { [key: string]: string | string[] | MessageTree };
