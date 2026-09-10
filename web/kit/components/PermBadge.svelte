<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The bot half of the badge: a Perm in, a label and a tone out.
  //
  // Was Badge.svelte, and it was the whole component -- the generic label
  // shape and the permission ladder in one file, in @bagel/kit's ancestor.
  // The shape is @bagel/ui/svelte/Badge.svelte now; what stays here is the
  // part that could never move, because `Perm` is a bot domain type
  // (../lib/types.ts) and the six strings below are bot copy. A design
  // library that ships `.bb-tag--broadcaster` has stopped being reusable.
  //
  // The seam is --badge-tone (colour) and --badge-tone-rule (its hairline).
  // House palette, unchanged by the move: steel for everyone, tan for subs,
  // SILVER for VIP (it shipped purple #d9aaff by mistake; VIP is never purple
  // here), green deepening with mod authority. The first three read the
  // account-tier tokens (../styles/tokens.css) so they cannot drift from the
  // same tiers spelled elsewhere; the mod ramp keeps its own alphas because
  // it encodes authority depth, not a tier.
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import type { Perm } from '../lib/types';

  let { perm }: { perm: Perm } = $props();

  const label: Record<Perm, string> = {
    everyone: 'Everyone',
    sub: 'Subs',
    vip: 'VIPs',
    mod: 'Mods',
    lead_mod: 'Lead Mods',
    broadcaster: 'Broadcaster'
  };

  const tone: Record<Perm, [string, string]> = {
    everyone: ['var(--bb-tier-inactive)', 'var(--bb-tier-inactive-border)'],
    sub: ['var(--bb-tier-paid)', 'var(--bb-tier-paid-border)'],
    vip: ['var(--bb-tier-vip)', 'var(--bb-tier-vip-border)'],
    mod: ['var(--bb-green-glow)', 'rgba(82,183,136,0.30)'],
    lead_mod: ['var(--bb-green-glow)', 'rgba(82,183,136,0.45)'],
    broadcaster: ['var(--bb-green-glow)', 'rgba(82,183,136,0.60)']
  };

  const swatch = $derived(tone[perm] ?? tone.everyone);
</script>

<Badge
  dashed={perm === 'lead_mod'}
  style="--badge-tone:{swatch[0]};--badge-tone-rule:{swatch[1]}"
>{label[perm] ?? perm}</Badge>
