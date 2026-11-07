// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { IconName } from '@bagel/ui/lib/icons';

export type SectionId = 'overview' | 'recipes' | 'overlay' | 'connections' | 'settings';

export interface NavItem {
  id: SectionId;
  label: string;
  icon: IconName;
}

export const NAV_ITEMS: readonly NavItem[] = [
  { id: 'overview', label: 'Overview', icon: 'overview' },
  { id: 'recipes', label: 'Recipes', icon: 'start' },
  { id: 'overlay', label: 'Overlay editor', icon: 'lanes' },
  { id: 'connections', label: 'Connections', icon: 'integrate' },
  { id: 'settings', label: 'Settings', icon: 'gear' },
];

export const RAIL_COLLAPSED_KEY = 'bb-desktop-rail-collapsed';
