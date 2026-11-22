// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { poweredConfigError, type PoweredAlign, type PoweredConfig, type PoweredDirection } from '@bagel/kit/overlays';

const field = (fields: FormData, name: keyof PoweredConfig | 'expected_rev' | 'id') => String(fields.get(name) ?? '');
const whole = (raw: string) => (/^\d+$/.test(raw) ? Number(raw) : Number.NaN);

export function poweredFields(config: PoweredConfig): Record<string, string> {
  return {
    every_minutes: String(config.every_minutes),
    show_seconds: String(config.show_seconds),
    directions: config.directions.join(','),
    align: config.align
  };
}

export function parsePoweredSave(fields: FormData) {
  const config: PoweredConfig = {
    every_minutes: whole(field(fields, 'every_minutes')),
    show_seconds: whole(field(fields, 'show_seconds')),
    directions: field(fields, 'directions').split(',').filter(Boolean) as PoweredDirection[],
    align: field(fields, 'align') as PoweredAlign
  };
  const error = poweredConfigError(config);
  if (error) return { error } as const;
  return { error: null, config, id: whole(field(fields, 'id')) || 0, revision: whole(field(fields, 'expected_rev')) || 0 } as const;
}
