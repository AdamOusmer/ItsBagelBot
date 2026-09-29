// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'add' | 'brand';
export type ButtonTone = 'neutral' | 'success' | 'danger';
export type ButtonSize = 'md' | 'sm';

export interface ButtonLook {
  variant?: ButtonVariant;
  tone?: ButtonTone;
  size?: ButtonSize;
  block?: boolean;
  busy?: boolean;
  done?: boolean;
  static?: boolean;
}

const TONED = new Set<ButtonVariant>(['primary', 'secondary', 'ghost']);

function lookClasses(variant: ButtonVariant, tone: ButtonTone): string[] {
  if (!TONED.has(variant) || tone === 'neutral') return [`bb-btn--${variant}`];
  if (tone === 'success') return [variant === 'primary' ? 'bb-btn--go-solid' : 'bb-btn--go'];
  return variant === 'ghost' ? ['bb-btn--ghost', 'bb-btn--danger-hover'] : ['bb-btn--danger'];
}

export function hasMark(variant: ButtonVariant): boolean {
  return variant !== 'add';
}

export function buttonClass(look: ButtonLook): string {
  return [
    'bb-btn',
    ...lookClasses(look.variant ?? 'primary', look.tone ?? 'neutral'),
    look.static && 'bb-btn--static',
    look.block && 'bb-btn--block',
    look.size === 'sm' && 'bb-btn--sm',
    look.busy && 'is-loading',
    look.done && 'is-done',
  ]
    .filter(Boolean)
    .join(' ');
}
