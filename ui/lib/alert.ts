// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type AlertTone = 'neutral' | 'warm' | 'success' | 'warning' | 'danger';
export type AlertVariant = 'banner' | 'callout';
export type AlertPlacement = 'inline' | 'top';

export type AlertCta = { label: string; href: string } | { label: string; formAction: string };

export interface AlertLook {
  tone?: AlertTone;
  variant?: AlertVariant;
  placement?: AlertPlacement;
  row?: 1 | 2;
  flush?: boolean;
  stack?: boolean;
}

export function alertClass(look: AlertLook): string {
  return [
    'bb-alert',
    `bb-alert--${look.tone ?? 'danger'}`,
    look.variant === 'callout' && 'bb-alert--callout',
    look.placement === 'top' && 'bb-alert--top',
    look.row === 2 && 'bb-alert--row-2',
    look.flush && 'bb-alert--flush',
    look.stack && 'bb-alert--stack',
  ]
    .filter(Boolean)
    .join(' ');
}
