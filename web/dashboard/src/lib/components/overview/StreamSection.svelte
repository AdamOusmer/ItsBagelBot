<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import StatTile from '@bagel/ui/svelte/StatTile.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { formatCounterValue } from '@bagel/kit/validation';
  import ChatVolumeChart from './ChatVolumeChart.svelte';
  import {
    formatDuration,
    minutesSince,
    clockFace,
    type StreamMeta,
    type StreamCounters,
    type ChatVolume
  } from '$lib/overview-live';

  const { t } = getI18n();

  let {
    meta,
    counters,
    volume,
    now
  }: {
    meta: StreamMeta;
    counters: StreamCounters;
    volume: ChatVolume;
    now: number;
  } = $props();

  const elapsed = $derived(formatDuration(minutesSince(meta.startedAt, now)));
  const sinceEnd = $derived(formatDuration(minutesSince(meta.endedAt, now)));

  type Stat = { id: string; value: string; label: string; tone?: 'positive' | 'accent' };

  const stats = $derived.by<Stat[]>(() => [
    {
      id: 'messages',
      value: formatCounterValue(counters.messages),
      label: t('overview.statMessagesSeen')
    },
    {
      id: 'answered',
      value: formatCounterValue(counters.answered),
      label: t('overview.statAnswered'),
      tone: 'positive'
    },
    {
      id: 'mod',
      value: formatCounterValue(counters.modActions),
      label: t('overview.statModActions'),
      tone: 'accent'
    }
  ]);
</script>

<section class="ov-stream" aria-labelledby="ov-stream-h">
  <Card flush>
    <div class="ov-stream__glow" aria-hidden="true"></div>
    <div class="ov-stream__grid">
      <div class="ov-stream__main">
        <div class="ov-stream__eyebrow">
          <Heading level={6} as="h2" variant="label" id="ov-stream-h">{t('overview.thisStream')}</Heading>
        </div>

        {#if !meta.ok}
          <div class="ov-stream__notice"><Text size="sm" tone="muted">{t('overview.streamUnavailable')}</Text></div>
        {:else if !meta.known}
          <div class="ov-stream__notice"><Text size="sm" tone="muted">{t('overview.streamNeverSeen')}</Text></div>
        {:else if meta.live}
          <Tag tone="live" mark="solid" sweep>{t('overview.streamLive')}</Tag>
          <div class="ov-stream__big">{elapsed}</div>
          {#if meta.title}<p class="ov-stream__title">{meta.title}</p>{/if}
          <div class="ov-stream__meta">
            <Label mono as="span">
              {t('overview.streamStartedAt', {
                time: clockFace(meta.startedAt),
                n: meta.viewers.toLocaleString()
              })}
            </Label>
          </div>
        {:else}
          <Tag tone="quiet" mark="hollow">{t('overview.streamOffline')}</Tag>
          <div class="ov-stream__big">{sinceEnd}</div>
          <p class="ov-stream__title">
            {t('overview.streamLastRan', { d: formatDuration(meta.lastDurationMin) })}
          </p>
          <div class="ov-stream__meta">
            <Label mono as="span">
              {t('overview.streamEndedAt', {
                time: clockFace(meta.endedAt),
                n: meta.peakViewers.toLocaleString()
              })}
            </Label>
          </div>
        {/if}

        <div class="ov-stream__spacer"></div>

        {#if counters.ok}
          <div class="ov-stream__stats">
            {#each stats as s (s.id)}
              <StatTile inline static tone={s.tone} label={s.label} value={s.value} />
            {/each}
          </div>
        {:else}
          <div class="ov-stream__notice ov-stream__notice--foot">
            <Text size="sm" tone="muted">{t('overview.countersUnavailable')}</Text>
          </div>
        {/if}
      </div>

      <div class="ov-stream__side">
        <ChatVolumeChart {volume} />
      </div>
    </div>
  </Card>
</section>

<style>
  .ov-stream {
    margin-bottom: var(--row-gap);
  }
  .ov-stream__glow {
    position: absolute;
    inset: 0;
    pointer-events: none;
    background: radial-gradient(circle at 92% 0%, rgba(var(--bb-green-glow-rgb), 0.14), transparent 55%);
  }
  .ov-stream__grid {
    position: relative;
    display: grid;
    grid-template-columns: 420px 1fr;
    gap: 30px;
    padding: 22px 24px 20px;
  }
  .ov-stream__main,
  .ov-stream__side {
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .ov-stream__eyebrow {
    margin-bottom: 14px;
  }
  .ov-stream__big {
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: 68px;
    line-height: 1;
    letter-spacing: -0.03em;
    color: var(--bb-white);
    margin: 16px 0 0;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .ov-stream__title {
    margin: 12px 0 0;
    font-family: var(--bb-font-body);
    font-size: 14.5px;
    line-height: 1.45;
    color: var(--bb-white);
    max-width: 34ch;
    text-wrap: pretty;
  }
  .ov-stream__meta {
    margin-top: 6px;
  }
  .ov-stream__notice {
    max-width: 40ch;
  }
  .ov-stream__notice--foot {
    margin-top: 22px;
    padding-top: 18px;
    border-top: 1px solid var(--bb-border);
  }
  .ov-stream__spacer {
    flex: 1;
  }
  .ov-stream__stats {
    display: flex;
    gap: 22px;
    margin: 22px 0 0;
    padding-top: 18px;
    border-top: 1px solid var(--bb-border);
  }

  @media (max-width: 900px) {
    .ov-stream__grid {
      grid-template-columns: 1fr;
      gap: 22px;
    }
    .ov-stream__big {
      font-size: 52px;
    }
  }
  @media (max-width: 560px) {
    .ov-stream__stats {
      flex-wrap: wrap;
      gap: 16px 22px;
    }
  }
</style>
