// Svelte context glue for i18n. Each app's RootShell calls setI18n(locale) once
// per render tree; every descendant (including shared components) reads the
// translator via getI18n(). Context is per-render on the server, so there is no
// cross-request locale bleed the way a module-level store would have.
import { getContext, setContext } from 'svelte';
import type { Locale } from './types';
import { DEFAULT_LOCALE, translate, translateList } from './messages';

export interface I18n {
  locale: Locale;
  /** translate(key, params) bound to the active locale. */
  t: (key: string, params?: Record<string, string | number>) => string;
  /** ordered-list leaves (feature bullets, headline words). */
  tl: (key: string) => string[];
}

const KEY = Symbol('bagel.i18n');

function make(locale: Locale): I18n {
  return {
    locale,
    t: (key, params) => translate(locale, key, params),
    tl: (key) => translateList(locale, key)
  };
}

export function setI18n(locale: Locale): I18n {
  const i18n = make(locale);
  setContext(KEY, i18n);
  return i18n;
}

/**
 * Read the active translator. Falls back to a default-locale translator when no
 * context is set (e.g. the admin app, which has not opted into i18n yet), so
 * shared components keep rendering English instead of throwing.
 */
export function getI18n(): I18n {
  return getContext<I18n>(KEY) ?? make(DEFAULT_LOCALE);
}
