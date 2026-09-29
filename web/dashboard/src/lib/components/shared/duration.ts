// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

type Translate = (key: string, params?: Record<string, string | number>) => string;

export function formatDuration(seconds: number, t: Translate, locale: string): string {
  const fmt = new Intl.NumberFormat(locale, { maximumFractionDigits: 1 });
  if (seconds > 0 && seconds % 3600 === 0) return t('common.duration.hours', { n: fmt.format(seconds / 3600) });
  if (seconds > 0 && seconds % 60 === 0) return t('common.duration.minutes', { n: fmt.format(seconds / 60) });
  if (seconds < 60) return t('common.duration.seconds', { n: fmt.format(seconds) });
  return t('common.duration.minutes', { n: fmt.format(Math.round(seconds / 60)) });
}
