// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

type WaitText = { locale: string; justJoined: string };

export function waitLabeler({ locale, justJoined }: WaitText): (seconds: number) => string {
  const unitFmt = (unit: 'hour' | 'minute') => new Intl.NumberFormat(locale, { style: 'unit', unit, unitDisplay: 'narrow' });
  const hoursFmt = unitFmt('hour');
  const minutesFmt = unitFmt('minute');

  return (seconds) => {
    const minutes = Math.floor(seconds / 60);
    if (minutes < 1) return justJoined;
    const hours = Math.floor(minutes / 60);
    const rest = minutesFmt.format(minutes % 60);
    return hours > 0 ? `${hoursFmt.format(hours)} ${rest}` : minutesFmt.format(minutes);
  };
}
