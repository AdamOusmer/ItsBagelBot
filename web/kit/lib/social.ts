// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Where the bot lives off its own site.
//
// These five URLs were a `const socials` array inside the marketing site's
// SocialRail component. The rail moved into @bagel/ui, which cannot hold them:
// a design library that ships a corner stack of brand marks must not also ship
// this bot's Discord invite. They land here rather than back in the marketing
// site because they are the same five accounts the docs footer and the console
// link to, and having one list is the point.
//
// `brandX` and not `x`: the icon set has one `x`, and it is the stroke glyph
// every dismissable surface uses as a close button. See ui/scripts/gen-icons.mjs.

import type { IconName } from '@bagel/ui/lib/icons';

export interface SocialLink {
  label: string;
  href: string;
  icon: IconName;
}

export const SOCIAL_LINKS: readonly SocialLink[] = [
  { label: 'Discord', href: 'https://discord.gg/SZ2remwSDv', icon: 'discord' },
  { label: 'X', href: 'https://x.com/itsbagelbot', icon: 'brandX' },
  { label: 'TikTok', href: 'https://tiktok.com/@itsbagelbot', icon: 'tiktok' },
  { label: 'YouTube', href: 'https://youtube.com/@itsbagelbot', icon: 'youtube' },
  { label: 'GitHub', href: 'https://github.com/AdamOusmer/ItsBagelBot', icon: 'github' },
] as const;
