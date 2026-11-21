<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { page } from '$app/state';
  import { getI18n } from '@bagel/kit/i18n/context';
  import MarkBadge from '@bagel/ui/svelte/MarkBadge.svelte';
  import { premiumLocked } from './premium-lock';

  let { size = 36 }: { size?: number } = $props();

  const { t } = getI18n();
  const locked = $derived(premiumLocked(page.data));
</script>

{#if locked}
  <MarkBadge src="/premium-logo.png" size="xs" tone="premium" style="--badge: {size}px; --badge-fill: 0.86"
    role="img" aria-label={t('billing.premium')} title={t('billing.premiumOnly')} />
{/if}
