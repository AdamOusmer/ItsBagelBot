<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Domain colors stay here; the shared Badge owns the label/pill geometry.
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import type { Snippet } from 'svelte';

  type StateTone =
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

  type BadgeStyle = { tone: string; background: string; rule: string };

  const BADGE_STYLES: Record<StateTone, BadgeStyle> = {
    free: { tone: 'var(--bb-tier-free)', background: 'var(--bb-tier-free-bg)', rule: 'var(--bb-tier-free-border)' },
    paid: { tone: 'var(--bb-tier-paid)', background: 'var(--bb-tier-paid-bg)', rule: 'var(--bb-tier-paid-border)' },
    vip: { tone: 'var(--bb-tier-vip)', background: 'var(--bb-tier-vip-bg)', rule: 'var(--bb-tier-vip-border)' },
    banned: { tone: 'var(--bb-tier-banned)', background: 'var(--bb-tier-banned-bg)', rule: 'var(--bb-tier-banned-border)' },
    inactive: { tone: 'var(--bb-tier-inactive)', background: 'var(--bb-tier-inactive-bg)', rule: 'var(--bb-tier-inactive-border)' },
    neutral: { tone: 'var(--bb-muted)', background: 'rgba(255, 255, 255, 0.03)', rule: 'var(--glass-border)' },
    moderator: { tone: 'var(--bb-muted)', background: 'rgba(255, 255, 255, 0.03)', rule: 'var(--glass-border)' },
    admin: { tone: 'var(--bb-tier-paid)', background: 'var(--bb-tier-paid-bg)', rule: 'var(--bb-tier-paid-border)' },
    owner: { tone: 'var(--bb-tier-vip)', background: 'var(--bb-tier-vip-bg)', rule: 'var(--bb-tier-vip-border)' },
    positive: { tone: 'var(--bb-green-light, #74c69d)', background: 'rgba(82,183,136,.1)', rule: 'rgba(82,183,136,.3)' },
    warning: { tone: '#f2c879', background: 'rgba(242,200,121,.1)', rule: 'rgba(242,200,121,.3)' },
    danger: { tone: '#f28c8c', background: 'rgba(242,140,140,.1)', rule: 'rgba(242,140,140,.3)' }
  };

  let {
    tone,
    shape = 'pill',
    children
  }: {
    shape?: 'tag' | 'pill';
    tone: StateTone;
    children: Snippet;
  } = $props();

  const badgeStyle = $derived(BADGE_STYLES[tone]);
</script>

<Badge
  {shape}
  style="--badge-tone:{badgeStyle.tone};--badge-bg:{badgeStyle.background};--badge-tone-rule:{badgeStyle.rule}"
>{@render children()}</Badge>
