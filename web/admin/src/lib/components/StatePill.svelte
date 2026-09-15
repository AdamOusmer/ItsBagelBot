<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Domain colours stay here; the shared Badge owns the hairline label.
  //
  // shape="pill" plus --badge-bg is the retired status chip: a 999px filled
  // lozenge. Status is a tag you read now (tags.css: hairline, no frame). The
  // --badge-tone seam is the same one PermBadge uses, because the colour
  // comes from admin data (tier / role / serving / giveaway state) the
  // library must not know about. --badge-bg is omitted on purpose: it only
  // exists to paint the pill fill, and there is no fill.
  //
  // `shape` is still accepted because giveaway and user rows pass
  // shape="tag". It is ignored: both values are the hairline Badge default.
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import type { Snippet } from 'svelte';

  let {
    tone,
    shape: _shape = 'tag',
    children
  }: {
    shape?: 'tag' | 'pill';
    tone:
      | 'free'
      | 'paid'
      | 'vip'
      | 'banned'
      | 'inactive'
      | 'neutral'
      | 'moderator'
      | 'admin'
      | 'owner'
      | 'positive'
      | 'warning'
      | 'danger';
    children: Snippet;
  } = $props();
</script>

<Badge class="state-badge {tone}">{@render children()}</Badge>

<style>
  :global(.state-badge.free) {
    --badge-tone: var(--bb-tier-free);
    --badge-tone-rule: var(--bb-tier-free-border);
  }
  :global(.state-badge.paid) {
    --badge-tone: var(--bb-tier-paid);
    --badge-tone-rule: var(--bb-tier-paid-border);
  }
  :global(.state-badge.vip) {
    --badge-tone: var(--bb-tier-vip);
    --badge-tone-rule: var(--bb-tier-vip-border);
  }
  :global(.state-badge.banned) {
    --badge-tone: var(--bb-tier-banned);
    --badge-tone-rule: var(--bb-tier-banned-border);
  }
  :global(.state-badge.inactive) {
    --badge-tone: var(--bb-tier-inactive);
    --badge-tone-rule: var(--bb-tier-inactive-border);
  }
  :global(.state-badge.neutral),
  :global(.state-badge.moderator) {
    --badge-tone: var(--bb-muted);
    --badge-tone-rule: var(--glass-border);
  }
  /* The staff ladder borrows the tier palette rather than inventing a second
     three-step scale: the eye already reads tan < silver as "higher" from the
     user rows, and two palettes for two ladders is how they end up disagreeing
     about which colour means "most authority". */
  :global(.state-badge.admin) {
    --badge-tone: var(--bb-tier-paid);
    --badge-tone-rule: var(--bb-tier-paid-border);
  }
  :global(.state-badge.owner) {
    --badge-tone: var(--bb-tier-vip);
    --badge-tone-rule: var(--bb-tier-vip-border);
  }
  :global(.state-badge.positive) {
    --badge-tone: var(--bb-green-light, #74c69d);
    --badge-tone-rule: rgba(82, 183, 136, 0.3);
  }
  :global(.state-badge.warning) {
    --badge-tone: #f2c879;
    --badge-tone-rule: rgba(242, 200, 121, 0.3);
  }
  :global(.state-badge.danger) {
    --badge-tone: #f28c8c;
    --badge-tone-rule: rgba(242, 140, 140, 0.3);
  }
</style>
