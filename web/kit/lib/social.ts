// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { IconName } from '@bagel/ui/lib/icons';
import { SITE } from './site-links';

export interface SocialLink {
  label: string;
  href: string;
  icon: IconName;
}

export const SOCIAL_LINKS: readonly SocialLink[] = [
  { label: 'Discord', href: SITE.discord, icon: 'discord' },
  { label: 'X', href: 'https://x.com/itsbagelbot', icon: 'brandX' },
  { label: 'TikTok', href: 'https://tiktok.com/@itsbagelbot', icon: 'tiktok' },
  { label: 'YouTube', href: 'https://youtube.com/@itsbagelbot', icon: 'youtube' },
  { label: 'GitHub', href: SITE.github, icon: 'github' },
] as const;
