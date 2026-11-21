// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export function userDates(locale: string) {
  const dates = new Intl.DateTimeFormat(locale, { dateStyle: 'medium' });
  const relative = new Intl.RelativeTimeFormat(locale, { numeric: 'auto', style: 'short' });

  return {
    date(value?: string | null) {
      const stamp = Date.parse(value ?? '');
      return Number.isNaN(stamp) ? '-' : dates.format(stamp);
    },
    ago(value?: string | null) {
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
