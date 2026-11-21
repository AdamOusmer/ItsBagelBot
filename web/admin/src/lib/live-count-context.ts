// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { createContext } from 'svelte';

type LiveCountContext = {
  readonly value: number | null;
  update: (request: Promise<number | null>, count: number | null) => void;
};

export const [getLiveCount, provideLiveCount] = createContext<LiveCountContext>();
