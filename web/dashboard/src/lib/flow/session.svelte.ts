// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { SvelteMap } from 'svelte/reactivity';
import type { FlowPreview } from './thumb';

export const savedEnabled = new SvelteMap<string, boolean>();
export const savedPreviews = new SvelteMap<string, FlowPreview>();
export const goveeKey = $state({ saved: false });
