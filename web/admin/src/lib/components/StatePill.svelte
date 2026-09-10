<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The tier badge and the state chip are the same mark in different colours,
  // so they are one component. Both are plain <span>s, NOT the shared Chip:
  // Chip is a <button>, and these sit inside ManagementRow's primary button.
  // A button inside a button is invalid markup and a screen-reader trap -- the
  // exact defect ManagementRow was built to stop.
  //
  // Colour is a --bb-tier-* token, never a literal, so the dashboard's perm
  // badges and this roster cannot drift apart again. VIP is silver.
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

<span class="pill {tone}">{@render children()}</span>

<style>
  .pill {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    padding: 2px 8px;
    border-radius: var(--bb-radius-pill);
    border: 1px solid transparent;
    white-space: nowrap;
  }
  .free {
    color: var(--bb-tier-free);
    background: var(--bb-tier-free-bg);
    border-color: var(--bb-tier-free-border);
  }
  .paid {
    color: var(--bb-tier-paid);
    background: var(--bb-tier-paid-bg);
    border-color: var(--bb-tier-paid-border);
  }
  .vip {
    color: var(--bb-tier-vip);
    background: var(--bb-tier-vip-bg);
    border-color: var(--bb-tier-vip-border);
  }
  .banned {
    color: var(--bb-tier-banned);
    background: var(--bb-tier-banned-bg);
    border-color: var(--bb-tier-banned-border);
  }
  .inactive {
    color: var(--bb-tier-inactive);
    background: var(--bb-tier-inactive-bg);
    border-color: var(--bb-tier-inactive-border);
  }
  .neutral,
  .moderator {
    color: var(--bb-muted);
    background: rgba(255, 255, 255, 0.03);
    border-color: var(--glass-border);
  }
  /* The staff ladder borrows the tier palette rather than inventing a second
     three-step scale: the eye already reads tan < silver as "higher" from the
     user rows, and two palettes for two ladders is how they end up disagreeing
     about which colour means "most authority". */
  .admin {
    color: var(--bb-tier-paid);
    background: var(--bb-tier-paid-bg);
    border-color: var(--bb-tier-paid-border);
  }
  .owner {
    color: var(--bb-tier-vip);
    background: var(--bb-tier-vip-bg);
    border-color: var(--bb-tier-vip-border);
  }
</style>
