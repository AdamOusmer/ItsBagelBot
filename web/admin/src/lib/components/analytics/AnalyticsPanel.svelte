<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { Snippet } from 'svelte';
  import { getI18n } from '@bagel/kit/i18n/context';

  let {
    id, loading = false, empty = false, error, height, summary, footer, footerHeight, children
  }: {
    id: string; loading?: boolean; empty?: boolean; error?: string; height: string; summary?: Snippet;
    footer?: Snippet; footerHeight?: string; children?: Snippet;
  } = $props();
  const { t } = getI18n();
  const title = $derived(t(`admin.analytics.card.${id}`));
</script>

<Card as="section" aria-label={title} aria-busy={loading}>
  <Stack gap={3}>
    <Stack gap={1}>
      <CardHead {title} />
      <Text as="p" size="sm" tone="muted">{t(`admin.analytics.hint.${id}`)}</Text>
      {@render summary?.()}
    </Stack>
    <div class="panel-body" style:--panel-height={height}>
      {#if loading}
        <div aria-hidden="true"><Skeleton variant="block" height={height} /></div>
      {:else if error}
        <AlertBanner>{error}</AlertBanner>
      {:else if empty}
        <EmptyState title={t('admin.analytics.noRows')} />
      {:else}
        {@render children?.()}
      {/if}
    </div>
    {#if footerHeight}
      <div style:min-block-size={footerHeight}>
        {@render footer?.()}
      </div>
    {/if}
  </Stack>
</Card>

<style>
  .panel-body {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    min-block-size: var(--panel-height);
    min-inline-size: 0;
    align-content: start;
  }
</style>
