// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { getContext, setContext } from 'svelte';
import { createUiI18n, UI_DEFAULT_LOCALE, type UiI18n, type UiOverride } from '../lib/i18n';

const KEY = Symbol.for('@bagel/ui/i18n');
const FALLBACK = createUiI18n(() => UI_DEFAULT_LOCALE);

export function setUiI18n(locale: string | (() => string | null | undefined), override?: UiOverride): UiI18n {
  const read = typeof locale === 'function' ? locale : () => locale;
  return setContext(KEY, createUiI18n(read, override));
}

export function getUiI18n(): UiI18n {
  return getContext<UiI18n | undefined>(KEY) ?? FALLBACK;
}
