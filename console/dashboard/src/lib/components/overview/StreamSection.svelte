<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // "This stream" — the page's headline panel. Left column is the stream itself
  // (live/offline, elapsed time, title, per-stream counters); right column is the
  // chat-volume curve.
  //
  // Honesty: three independent reads feed this one section and any of them can
  // be down. `meta.ok`, `counters.ok` and `volume.ok` are checked separately so a
  // failed counter read never blanks a healthy stream header, and a stream we
  // have simply never seen (`known: false`) reads as "not yet", not as an error.
  //
  // `now` arrives as a prop rather than being read here: this component stays
  // pure like every other component in this directory, and the page owns the one
  // interval that ticks it.
  import { getI18n } from '@bagel/shared/i18n/context';
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

  type Stat = { id: string; value: string; label: string; tone: 'plain' | 'green' | 'tan' };

  const stats = $derived.by<Stat[]>(() => [
    {
      id: 'messages',
      value: counters.messages.toLocaleString(),
      label: t('overview.statMessagesSeen'),
      tone: 'plain'
    },
    {
      id: 'answered',
      value: counters.answered.toLocaleString(),
      label: t('overview.statAnswered'),
      tone: 'green'
    },
    {
      id: 'mod',
      value: counters.modActions.toLocaleString(),
      label: t('overview.statModActions'),
      tone: 'tan'
    }
  ]);
</script>

<section class="ov-stream" aria-labelledby="ov-stream-h">
  <div class="ov-stream__glow" aria-hidden="true"></div>
  <div class="ov-stream__grid">
    <div class="ov-stream__main">
      <h2 id="ov-stream-h" class="ov-stream__eyebrow">{t('overview.thisStream')}</h2>

      {#if !meta.ok}
        <p class="ov-stream__notice">{t('overview.streamUnavailable')}</p>
      {:else if !meta.known}
        <p class="ov-stream__notice">{t('overview.streamNeverSeen')}</p>
      {:else if meta.live}
        <span class="ov-pill ov-pill--live">
          <span class="ov-dot ov-dot--live" aria-hidden="true"></span>
          {t('overview.streamLive')}
        </span>
        <div class="ov-stream__big">{elapsed}</div>
        {#if meta.title}<p class="ov-stream__title">{meta.title}</p>{/if}
        <p class="ov-stream__meta">
          {t('overview.streamStartedAt', {
            time: clockFace(meta.startedAt),
            n: meta.viewers.toLocaleString()
          })}
        </p>
      {:else}
        <span class="ov-pill">
          <span class="ov-dot" aria-hidden="true"></span>
          {t('overview.streamOffline')}
        </span>
        <div class="ov-stream__big">{sinceEnd}</div>
        <p class="ov-stream__title">
          {t('overview.streamLastRan', { d: formatDuration(meta.lastDurationMin) })}
        </p>
        <p class="ov-stream__meta">
          {t('overview.streamEndedAt', {
            time: clockFace(meta.endedAt),
            n: meta.peakViewers.toLocaleString()
          })}
        </p>
      {/if}

      <div class="ov-stream__spacer"></div>

      {#if counters.ok}
        <dl class="ov-stream__stats">
          {#each stats as s (s.id)}
            <div class="ov-stat">
              <dt class="ov-stat__value ov-stat--{s.tone}">{s.value}</dt>
              <dd class="ov-stat__label">{s.label}</dd>
            </div>
          {/each}
        </dl>
      {:else}
        <p class="ov-stream__notice ov-stream__notice--foot">{t('overview.countersUnavailable')}</p>
      {/if}
    </div>

    <div class="ov-stream__side">
      <ChatVolumeChart {volume} />
    </div>
  </div>
</section>

<style>
  .ov-stream {
    position: relative;
    overflow: hidden;
    background: var(--bb-card-bg);
    border: 1px solid var(--bb-border);
    border-radius: 8px;
    margin-bottom: var(--row-gap);
  }
  .ov-stream__glow {
    position: absolute;
    inset: 0;
    pointer-events: none;
    background: radial-gradient(circle at 92% 0%, rgba(82, 183, 136, 0.14), transparent 55%);
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
    font-family: var(--bb-font-mono);
    font-size: 11px;
    font-weight: 500;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--bb-muted);
    margin: 0 0 14px;
  }
  .ov-pill {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    align-self: flex-start;
    padding: 5px 12px 5px 10px;
    border-radius: 999px;
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid var(--bb-border);
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .ov-pill--live {
    background: var(--bb-status-success-bg);
    border-color: var(--bb-status-success-border);
    color: var(--bb-status-success-fg);
  }
  .ov-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    flex: none;
    background: var(--bb-muted);
  }
  .ov-dot--live {
    background: var(--bb-status-success);
    box-shadow: 0 0 6px var(--bb-status-success);
    animation: ov-pulse 2.4s ease-in-out infinite;
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
    margin: 6px 0 0;
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .ov-stream__notice {
    margin: 0;
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    line-height: 1.5;
    color: var(--bb-muted);
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
  .ov-stat {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .ov-stat__value {
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: 24px;
    letter-spacing: -0.02em;
    color: var(--bb-white);
    font-variant-numeric: tabular-nums;
    margin: 0;
  }
  .ov-stat--green {
    color: var(--bb-green-glow);
  }
  .ov-stat--tan {
    color: var(--bb-tan-light);
  }
  .ov-stat__label {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--bb-muted);
    margin: 0;
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
  @media (prefers-reduced-motion: reduce) {
    .ov-dot--live {
      animation: none;
    }
  }
</style>
