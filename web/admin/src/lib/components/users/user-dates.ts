// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Locale } from '@bagel/kit/i18n';

type UserTimestamp = string | null;

const formatsByLocale = new Map<Locale, ReturnType<typeof makeUserDates>>();

export function userDates(locale: Locale) {
  const existing = formatsByLocale.get(locale);
  if (existing) return existing;

  const formats = makeUserDates(locale);
  formatsByLocale.set(locale, formats);
  return formats;
}

function makeUserDates(locale: Locale) {
  const dates = new Intl.DateTimeFormat(locale, { dateStyle: 'medium' });
  const relative = new Intl.RelativeTimeFormat(locale, { numeric: 'auto', style: 'short' });

  return {
    date(value?: UserTimestamp) {
      const stamp = Date.parse(value ?? '');
      return Number.isNaN(stamp) ? '-' : dates.format(stamp);
    },
    ago(value?: UserTimestamp) {
      const stamp = Date.parse(value ?? '');
      if (Number.isNaN(stamp)) return '-';
      const minutes = Math.max(Math.round((Date.now() - stamp) / 60_000), 0);
      if (minutes < 60) return relative.format(-minutes, 'minute');
      const hours = Math.round(minutes / 60);
      if (hours < 48) return relative.format(-hours, 'hour');
      return relative.format(-Math.round(hours / 24), 'day');
    }
  };
}
