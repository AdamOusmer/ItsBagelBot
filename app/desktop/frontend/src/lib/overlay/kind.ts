// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { IconName } from '@bagel/ui/lib/icons';
import type { LayerKind } from './types';

export const KIND_LABEL: Record<LayerKind, string> = {
  text: 'Text',
  image: 'Image',
  alert: 'Alert box',
  chat: 'Chat box',
};

export const KIND_ICON: Record<LayerKind, IconName> = {
  text: 'edit',
  image: 'modules',
  alert: 'bell',
  chat: 'users',
};

export const KIND_TINT: Record<LayerKind, string> = {
  text: 'var(--bb-tan-light)',
  image: 'var(--bb-muted-light)',
  alert: 'var(--bb-green-glow)',
  chat: 'var(--bb-tan)',
};
