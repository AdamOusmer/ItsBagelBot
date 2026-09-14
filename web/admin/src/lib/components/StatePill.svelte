<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Domain colors stay here; the shared Badge owns the non-interactive pill.
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import type { Snippet } from 'svelte';

  let {
    tone,
    children
  }: {
    tone:
      | 'free'
      | 'paid'
      | 'vip'
      | 'banned'
      | 'inactive'
      | 'neutral'
      | 'moderator'
      | 'admin'
      | 'owner';
    children: Snippet;
  } = $props();
</script>

<Badge shape="pill" class="state-pill {tone}">{@render children()}</Badge>

<style>
  :global(.state-pill.free) {
    --badge-tone: var(--bb-tier-free);
    --badge-bg: var(--bb-tier-free-bg);
    --badge-tone-rule: var(--bb-tier-free-border);
  }
  :global(.state-pill.paid) {
    --badge-tone: var(--bb-tier-paid);
    --badge-bg: var(--bb-tier-paid-bg);
    --badge-tone-rule: var(--bb-tier-paid-border);
  }
  :global(.state-pill.vip) {
    --badge-tone: var(--bb-tier-vip);
    --badge-bg: var(--bb-tier-vip-bg);
    --badge-tone-rule: var(--bb-tier-vip-border);
  }
  :global(.state-pill.banned) {
    --badge-tone: var(--bb-tier-banned);
    --badge-bg: var(--bb-tier-banned-bg);
    --badge-tone-rule: var(--bb-tier-banned-border);
  }
  :global(.state-pill.inactive) {
    --badge-tone: var(--bb-tier-inactive);
    --badge-bg: var(--bb-tier-inactive-bg);
    --badge-tone-rule: var(--bb-tier-inactive-border);
  }
  :global(.state-pill.neutral),
  :global(.state-pill.moderator) {
    --badge-tone: var(--bb-muted);
    --badge-bg: rgba(255, 255, 255, 0.03);
    --badge-tone-rule: var(--glass-border);
  }
  /* The staff ladder borrows the tier palette rather than inventing a second
     three-step scale: the eye already reads tan < silver as "higher" from the
     user rows, and two palettes for two ladders is how they end up disagreeing
     about which colour means "most authority". */
  :global(.state-pill.admin) {
    --badge-tone: var(--bb-tier-paid);
    --badge-bg: var(--bb-tier-paid-bg);
    --badge-tone-rule: var(--bb-tier-paid-border);
  }
  :global(.state-pill.owner) {
    --badge-tone: var(--bb-tier-vip);
    --badge-bg: var(--bb-tier-vip-bg);
    --badge-tone-rule: var(--bb-tier-vip-border);
  }
</style>
