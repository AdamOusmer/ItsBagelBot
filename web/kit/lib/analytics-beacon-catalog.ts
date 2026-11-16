// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const PUBLIC_PAGES = ['commands', 'leaderboard', 'songqueue', 'queue', 'stats', 'login'] as const;
export const MARKETING_CTAS = ['nav', 'hero', 'playground', 'finale', 'pricing', 'footer', 'landing', 'compare', 'guide', 'import', 'builder', 'app'] as const;

export type PublicPage = typeof PUBLIC_PAGES[number];
export type MarketingCTA = typeof MARKETING_CTAS[number];

export type BeaconEvent =
  | { event: 'public.views'; dim: PublicPage }
  | { event: 'marketing.cta'; dim: MarketingCTA };
