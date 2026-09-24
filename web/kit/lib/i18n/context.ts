// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { getContext, setContext } from 'svelte';
import type { Locale } from './types';
import type { MessageKey } from './keys';
import { DEFAULT_LOCALE, translate, translateList } from './messages';

export interface I18n {
  locale: Locale;
  t: (key: MessageKey, params?: Record<string, string | number>) => string;
  tl: (key: MessageKey) => string[];
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

export function getI18n(): I18n {
  return getContext<I18n>(KEY) ?? make(DEFAULT_LOCALE);
}
