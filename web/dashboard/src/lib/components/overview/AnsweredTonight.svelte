<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AnsweredTonight } from '$lib/overview-live';

  const { t } = getI18n();

  let { answered }: { answered: AnsweredTonight } = $props();

  type Bar = { name: string; count: number; share: number; ramp: 1 | 2 | 3 };

  const rankShade = (i: number): Bar['ramp'] => (i === 0 ? 1 : i < 3 ? 2 : 3);

  const bars = $derived.by<Bar[]>(() => {
    const rows = answered.commands;
    if (!rows.length) return [];
    const top = Math.max(...rows.map((r) => r.count), 1);
    return rows.map((r, i) => ({
      name: r.name,
      count: r.count,
      share: Math.max(4, Math.round((r.count / top) * 100)) / 100,
      ramp: rankShade(i)
    }));
  });
</script>

<Card as="section" flush aria-labelledby="ov-ans-h">
  <div class="ov-ans__head">
    <Heading level={6} as="h2" variant="title" id="ov-ans-h">{t('overview.answeredTonight')}</Heading>
    <TextLink href="/commands" label={`${t('overview.answeredAll')} →`} />
  </div>

  {#if !answered.ok}
    <div class="ov-ans__empty"><Text size="sm" tone="muted">{t('overview.answeredUnavailable')}</Text></div>
  {:else if !bars.length}
    <div class="ov-ans__empty"><Text size="sm" tone="muted">{t('overview.answeredEmpty')}</Text></div>
  {:else}
    <ul class="ov-ans__list">
      {#each bars as bar (bar.name)}
        <li class="ov-ans__row">
          <div class="ov-ans__line">
            <Text as="span" size="sm" mono>{bar.name}</Text>
            <Text as="span" size="xs" mono tone="muted">{bar.count.toLocaleString()}</Text>
          </div>
          <ProgressBar value={bar.share} tone="success" ramp={bar.ramp} label={bar.name} aria-hidden="true" />
        </li>
      {/each}
    </ul>
  {/if}
</Card>

<style>
  .ov-ans__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 17px 20px 14px;
    border-bottom: 1px solid var(--bb-border);
  }
  .ov-ans__list {
    list-style: none;
    margin: 0;
    padding: 16px 20px 18px;
    display: flex;
    flex-direction: column;
    gap: 13px;
  }
  .ov-ans__row {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .ov-ans__line {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 10px;
  }
  .ov-ans__empty {
    padding: 16px 20px 20px;
  }
</style>
