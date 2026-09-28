// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const STARTER_IDS = ['discord', 'socials', 'lurk', 'rules', 'schedule', 'hydrate'] as const;
export type StarterId = (typeof STARTER_IDS)[number];

export interface Starter {
  name: string;
  response: string;
}
