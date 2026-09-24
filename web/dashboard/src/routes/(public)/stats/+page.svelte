<script lang="ts">
  import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { onMount, untrack } from 'svelte';
  import { AuroraBg, Heading, LightField, AlertBanner, Card, Stack, Text, getI18n } from '@bagel/kit';
  import type { PageData } from './$types';
  import { commandsHref } from '@bagel/kit/site-links';
  import { visibleEventSource } from '$lib/visible-stream';

  let { data }: { data: PageData } = $props();

  const { t, locale } = getI18n();

  const POLL_MS = 5000;
  const INTRO_MS = 900;
  const TAU_MS = 200;
  const RATE_TAU_MS = 300;
  const MAX_DT_MS = 250;
  const MAX_PROJECT_S = 10;
  const MIN_CRAWL = 0.25;

  const seed = untrack(() => data.stats);
  type Snapshot = typeof seed;

  let live = $state(seed);
  let degraded = $state(seed.degraded);

  let boards = $state(untrack(() => data.boards));

  let display = $state({
    messages: seed.messages_total,
    events: seed.events_total,
    msgRate: seed.msg_rate_now ?? seed.msg_rate ?? 0,
    eventRate: seed.event_rate_now ?? seed.event_rate ?? 0
  });

  type Frame = typeof display;

  let snapAt = 0;
  let introAt = 0;
  let lastFrame = 0;
  let raf = 0;
  let reduced = false;
  let streamDown = false;

  function targetFrame(now: number): Frame {
    const secs = Math.min(Math.max(0, now - snapAt) / 1000, MAX_PROJECT_S);
    const msgAvg = live.msg_rate ?? 0;
    const eventAvg = live.event_rate ?? 0;
    return {
      messages: live.messages_total + msgAvg * secs,
      events: live.events_total + eventAvg * secs,
      msgRate: live.msg_rate_now ?? msgAvg,
      eventRate: live.event_rate_now ?? eventAvg
    };
  }

  function lerp(from: number, to: number, eased: number): number {
    return from + (to - from) * eased;
  }

  function closingFraction(now: number, dt: number): number {
    const p = (now - introAt) / INTRO_MS;
    if (p >= 1) return 1 - Math.exp(-dt / TAU_MS);
    const before = Math.max(0, (now - dt - introAt) / INTRO_MS);
    return 1 - Math.pow((1 - p) / (1 - before), 4);
  }

  function advance(cur: number, target: number, rate: number, dt: number, k: number): number {
    return Math.max(lerp(cur, target, k), cur + rate * (dt / 1000) * MIN_CRAWL);
  }

  function tick(now: number): void {
    const dt = Math.min(now - lastFrame, MAX_DT_MS);
    lastFrame = now;
    const target = targetFrame(now);
    const k = closingFraction(now, dt);
    const rateK = 1 - Math.exp(-dt / RATE_TAU_MS);
    display = {
      messages: advance(display.messages, target.messages, target.msgRate, dt, k),
      events: advance(display.events, target.events, target.eventRate, dt, k),
      msgRate: lerp(display.msgRate, target.msgRate, rateK),
      eventRate: lerp(display.eventRate, target.eventRate, rateK)
    };
    raf = requestAnimationFrame(tick);
  }

  function snap(now: number): void {
    lastFrame = now;
    display = targetFrame(now);
  }

  function applySnapshot(next: Snapshot): void {
    degraded = next.degraded;
    if (next.degraded) return;
    const prev = live;
    live = next;
    snapAt = performance.now();
    if (reduced) {
      snap(snapAt);
      return;
    }
    if (next.messages_total < prev.messages_total) display.messages = next.messages_total;
    if (next.events_total < prev.events_total) display.events = next.events_total;
  }

  async function refresh(): Promise<void> {
    if (document.hidden) return;
    try {
      const res = await fetch('/stats/data', { headers: { accept: 'application/json' } });
      if (!res.ok) return;
      applySnapshot(await res.json());
    } catch {
    }
  }

  async function refreshBoards(): Promise<void> {
    if (document.hidden) return;
    try {
      const res = await fetch('/stats/boards', { headers: { accept: 'application/json' } });
      if (!res.ok) return;
      boards = await res.json();
    } catch {
    }
  }

  function attachStream(es: EventSource, reopened: boolean): void {
    let resync = reopened;
    es.onopen = () => (streamDown = false);
    es.onerror = () => (streamDown = true);
    es.onmessage = (ev) => {
      try {
        applySnapshot(JSON.parse(ev.data) as Snapshot);
      } catch {
        return;
      }
      if (!resync) return;
      resync = false;
      snap(performance.now());
    };
    es.addEventListener('boards', (ev) => {
      try {
        boards = JSON.parse((ev as MessageEvent<string>).data) as typeof boards;
      } catch {
      }
    });
  }

  onMount(() => {
    reduced = prefersReducedMotion();
    const now = performance.now();
    snapAt = now;
    introAt = now;
    lastFrame = now;
    if (reduced) {
      snap(now);
    } else {
      display = { messages: 0, events: 0, msgRate: 0, eventRate: 0 };
      raf = requestAnimationFrame(tick);
    }

    const stop = visibleEventSource('/stats/stream', attachStream);
    const timer = setInterval(() => {
      if (!streamDown) return;
      void refresh();
      void refreshBoards();
    }, POLL_MS);

    const onVisible = () => {
      if (document.hidden) return;
      snap(performance.now());
      if (!reduced) {
        cancelAnimationFrame(raf);
        raf = requestAnimationFrame(tick);
      }
      if (!streamDown) return;
      void refresh();
      void refreshBoards();
    };
    document.addEventListener('visibilitychange', onVisible);

    return () => {
      stop();
      clearInterval(timer);
      document.removeEventListener('visibilitychange', onVisible);
      cancelAnimationFrame(raf);
    };
  });

  const totalFmt = new Intl.NumberFormat(locale, { maximumFractionDigits: 0 });
  const rateFmt = new Intl.NumberFormat(locale, { maximumFractionDigits: 1 });

  const PENDING = '-';

  const headline = $derived(t('stats.headline').split(' '));

  const tiles = $derived([
    {
      label: t('stats.messagesLabel'),
      value: totalFmt.format(Math.round(display.messages)),
      rate: live.msg_rate === null ? null : rateFmt.format(display.msgRate),
      average: live.msg_rate === null ? null : rateFmt.format(live.msg_rate),
      rateLabel: t('stats.messageRateLabel')
    },
    {
      label: t('stats.eventsLabel'),
      value: totalFmt.format(Math.round(display.events)),
      rate: live.event_rate === null ? null : rateFmt.format(display.eventRate),
      average: live.event_rate === null ? null : rateFmt.format(live.event_rate),
      rateLabel: t('stats.eventRateLabel')
    }
  ]);

  const traffic = $derived(boards.channels);
  const feed = $derived(boards.feed);

  const LOGIN_SHAPE = /^[A-Za-z0-9_]{1,25}$/;
  const channelHref = (row: { id: string; name: string }) =>
    commandsHref(LOGIN_SHAPE.test(row.name) ? row.name : row.id);
</script>

<svelte:head>
  <title>{t('stats.title')}</title>
  <meta name="description" content={t('stats.metaDescription')} />
  <link rel="canonical" href="https://stats.itsbagelbot.com/" />
  <meta property="og:url" content="https://stats.itsbagelbot.com/" />
  <meta property="og:title" content={t('stats.title')} />
  <meta property="og:description" content={t('stats.metaDescription')} />
  <meta name="twitter:title" content={t('stats.title')} />
  <meta name="twitter:description" content={t('stats.metaDescription')} />
</svelte:head>

<AuroraBg />
<div class="starfield" aria-hidden="true"><LightField /></div>

<main class="stats-page">
  <header class="hero">
    <div class="eyebrow reveal" style="--i:0">{t('stats.eyebrow')}</div>
    <h1 class="headline">
      {#each headline as word, i (i)}
        <span
          class="word reveal"
          style="--i:{1 + i * 0.5}"
          class:tan={i === 0}
          class:green={i === headline.length - 1}>{word}&nbsp;</span
        >
      {/each}
    </h1>
    <p class="lede reveal" style="--i:4">{t('stats.tagline')}</p>
  </header>

  {#if degraded}
    <div class="notice reveal" style="--i:4.5">
      <AlertBanner variant="warn">{t('stats.degraded')}</AlertBanner>
    </div>
  {/if}

  <section class="tiles" aria-label={t('stats.headline')}>
    {#each tiles as tile, i (tile.label)}
      <div class="tile-wrap reveal" style="--i:{5 + i * 0.5}">
        <Card atmo hover class="tile">
          {#snippet band()}
            <div class="tile-head">
              <span class="label">{tile.label}</span>
            </div>
          {/snippet}
          <Stack gap={3} class="tile-body">
          <div class="value">
            <span class="num">{tile.value}</span>
          </div>
          <div class="rate">
            {#if tile.rate === null}
              <span class="rate-num">{PENDING}</span>
            {:else}
              <span class="rate-num">{tile.rate}</span><small class="unit">{t('stats.perSecond')}</small>
            {/if}
            <span class="rate-label">{tile.rateLabel} · {t('stats.rightNow')}</span>
            {#if tile.average !== null}
              <span class="rate-label">{t('stats.minuteAverage', { rate: tile.average })}</span>
            {/if}
          </div>
          </Stack>
        </Card>
      </div>
    {/each}
  </section>

  <section class="boards" aria-label={t('stats.boardsEyebrow')}>
    <div class="board-wrap reveal" style="--i:6">
      <Card atmo class="board" label={t('stats.trafficBoardCh')}>
        {#snippet band()}
          <header class="board-head">
            <div class="board-titles">
              <Heading level={2} class="board-title">{t('stats.trafficBoardTitle')}</Heading>
              <Text size="sm" tone="muted" class="board-note">{t('stats.trafficBoardNote')}</Text>
            </div>
          </header>
        {/snippet}
        {#if traffic.length === 0}
          <p class="empty">{t('stats.boardEmpty')}</p>
        {:else}
          <div class="table-scroll">
            <table>
              <thead>
                <tr>
                  <th class="rank" scope="col">{t('stats.colRank')}</th>
                  <th scope="col">{t('stats.colChannel')}</th>
                  <th class="n" scope="col">{t('stats.colMessages')}</th>
                  <th class="n" scope="col">{t('stats.colEvents')}</th>
                </tr>
              </thead>
              <tbody>
                {#each traffic as row, i (row.id)}
                  <tr>
                    <td class="rank">{i + 1}</td>
                    <td class="chan bb-prose">
                      {#if row.name}
                        <a href={channelHref(row)}>{row.name}</a>
                      {:else}
                        <span class="unnamed">{t('stats.unknownChannel')}</span>
                      {/if}
                    </td>
                    <td class="n">{totalFmt.format(row.messages)}</td>
                    <td class="n">{totalFmt.format(row.events)}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </Card>
    </div>

    <div class="board-wrap reveal" style="--i:6.5">
      <Card atmo class="board" label={t('stats.feedBoardCh')}>
        {#snippet band()}
          <header class="board-head">
            <div class="board-titles">
              <Heading level={2} class="board-title">{t('stats.feedBoardTitle')}</Heading>
              <Text size="sm" tone="muted" class="board-note">{t('stats.feedBoardNote')}</Text>
            </div>
          </header>
        {/snippet}
        <div class="feed-total">
          <span class="num tan">{totalFmt.format(feed.total)}</span>
          <span class="label">{t('stats.feedTotalLabel')}</span>
        </div>
        {#if feed.entries.length === 0}
          <p class="empty">{t('stats.feedBoardEmpty')}</p>
        {:else}
          <ol class="feed-list">
            {#each feed.entries as row, i (row.id)}
              <li>
                <span class="rank">{i + 1}</span>
                <span class="chan bb-prose">
                  {#if row.name}
                    <a href={channelHref(row)}>{row.name}</a>
                  {:else}
                    <span class="unnamed">{t('stats.unknownChannel')}</span>
                  {/if}
                </span>
                <span class="n">{totalFmt.format(row.count)}</span>
              </li>
            {/each}
          </ol>
          <p class="ranked">{t('stats.feedRankedNote', { count: totalFmt.format(feed.ranked) })}</p>
        {/if}
      </Card>
    </div>
  </section>

  <footer class="foot reveal" style="--i:7.5">
    <span class="bb-tag bb-tag--live"><i class="bb-mark" aria-hidden="true"></i>{t('stats.liveNote')}<i class="bb-sweep" aria-hidden="true"></i></span>
  </footer>
</main>

<style>
  .starfield {
    position: fixed;
    inset: 0;
    z-index: 0;
    pointer-events: none;
  }

  .stats-page {
    position: relative;
    z-index: 1;
    min-height: calc(100vh - 76px);
    max-width: var(--bb-content-max);
    margin: 0 auto;
    padding: calc(76px + env(safe-area-inset-top, 0px) + clamp(40px, 8vh, 96px)) var(--bb-space-5)
      var(--bb-space-8);
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-6);
  }

  .hero {
    text-align: center;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--bb-space-3);
  }

  .eyebrow {
    font-family: var(--bb-font-mono);
    font-size: 12px;
    letter-spacing: var(--bb-tracking-eyebrow);
    text-transform: uppercase;
    color: var(--bb-green-glow);
  }

  .headline {
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: clamp(34px, 6vw, 72px);
    line-height: 1.02;
    letter-spacing: var(--bb-tracking-tight);
    color: var(--bb-white);
    margin: 0;
    max-width: 16ch;
  }
  .headline .word { display: inline-block; }
  .headline .tan { color: var(--bb-tan); }
  .headline .green { color: var(--bb-green); }

  .lede {
    font-family: var(--bb-font-body);
    font-size: clamp(15px, 1.6vw, 18px);
    line-height: 1.6;
    color: var(--bb-muted);
    margin: 0;
    max-width: 58ch;
  }

  .notice { max-width: 640px; width: 100%; margin: 0 auto; }

  .tiles {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--bb-space-4);
  }

  .tile-wrap { min-width: 0; }

  .tiles {
    --card-pad: clamp(24px, 3.4vw, 40px);
    --card-band-h: calc(70px * var(--d, 1));
    --card-band-pad: calc(14px * var(--d, 1)) var(--card-pad);
  }
  :global(.tile) { height: 100%; min-width: 0; }
  :global(.tile-body) { min-width: 0; container-type: inline-size; }
  :global(.tile)::before {
    content: '';
    position: absolute;
    inset: 0 0 auto;
    height: 1px;
    background: linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.14), transparent);
  }

  .tile-head { display: flex; align-items: center; gap: var(--bb-space-3); min-width: 0; }

  .label {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: var(--bb-tracking-eyebrow);
    text-transform: uppercase;
    color: var(--bb-muted);
    line-height: 1.4;
  }

  .value { display: flex; align-items: baseline; gap: 6px; min-width: 0; }

  .num {
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: clamp(20px, 8cqi, 60px);
    line-height: 1;
    letter-spacing: var(--bb-tracking-tight);
    color: var(--bb-white);
    font-variant-numeric: tabular-nums;
    overflow-wrap: anywhere;
  }

  .unit {
    font-family: var(--bb-font-mono);
    font-size: clamp(13px, 1.4vw, 17px);
    color: var(--bb-muted);
  }

  .rate {
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: 6px;
  }

  .rate-num {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: clamp(16px, 1.8vw, 22px);
    line-height: 1;
    color: var(--bb-green-glow);
    font-variant-numeric: tabular-nums;
  }

  .rate-label {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: var(--bb-tracking-eyebrow);
    text-transform: uppercase;
    color: var(--bb-muted);
  }

  .boards {
    display: grid;
    grid-template-columns: minmax(0, 1.35fr) minmax(0, 1fr);
    gap: var(--bb-space-4);
    align-items: start;
  }

  .board-wrap { min-width: 0; }

  .boards {
    --card-pad: clamp(20px, 2.4vw, 30px);
    --card-band-h: calc(112px * var(--d, 1));
    --card-band-pad: calc(16px * var(--d, 1)) var(--card-pad);
  }
  :global(.board) { height: 100%; min-width: 0; }

  .board-head { display: flex; align-items: flex-start; gap: var(--bb-space-3); min-width: 0; }
  .board-titles { min-width: 0; }

  :global(.board-title) { font-size: clamp(18px, 2vw, 22px); letter-spacing: var(--bb-tracking-tight); }
  :global(.board-note) { margin-top: 4px; }

  .empty {
    font-family: var(--bb-font-body);
    font-size: 14px;
    line-height: 1.6;
    color: var(--bb-muted);
    margin: 0;
  }

  .table-scroll { overflow-x: auto; margin: 0 calc(-1 * var(--bb-space-2)); padding: 0 var(--bb-space-2); }

  table { width: 100%; border-collapse: collapse; }

  th {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: var(--bb-tracking-eyebrow);
    text-transform: uppercase;
    color: var(--bb-muted);
    font-weight: 500;
    text-align: left;
    padding: 0 var(--bb-space-3) var(--bb-space-2) 0;
    white-space: nowrap;
  }

  td {
    font-family: var(--bb-font-body);
    font-size: 14px;
    color: var(--bb-white);
    padding: var(--bb-space-2) var(--bb-space-3) var(--bb-space-2) 0;
    border-top: 1px solid var(--bb-border);
    white-space: nowrap;
  }

  th:last-child, td:last-child { padding-right: 0; }

  .rank {
    font-family: var(--bb-font-mono);
    font-size: 12px;
    color: var(--bb-muted);
    width: 2.5ch;
    font-variant-numeric: tabular-nums;
  }

  .n { text-align: right; font-variant-numeric: tabular-nums; }
  th.n { text-align: right; padding-right: 0; }

  .chan { min-width: 0; }
  .unnamed { color: var(--bb-muted); font-style: italic; }

  .feed-total { display: flex; flex-direction: column; gap: 4px; }
  .feed-total .num {
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: clamp(28px, 4vw, 44px);
    line-height: 1;
    letter-spacing: var(--bb-tracking-tight);
    font-variant-numeric: tabular-nums;
  }
  .feed-total .num.tan { color: var(--bb-tan-light); }
  .feed-total .label {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: var(--bb-tracking-eyebrow);
    text-transform: uppercase;
    color: var(--bb-muted);
  }

  .feed-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; }
  .feed-list li {
    display: flex;
    align-items: baseline;
    gap: var(--bb-space-3);
    padding: var(--bb-space-2) 0;
    border-top: 1px solid var(--bb-border);
    font-family: var(--bb-font-body);
    font-size: 14px;
    color: var(--bb-white);
  }
  .feed-list .chan { flex: 1 1 auto; overflow-wrap: anywhere; }
  .feed-list .n { flex: 0 0 auto; }

  .ranked {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.08em;
    color: var(--bb-muted);
    margin: 0;
  }

  .foot {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--bb-space-2);
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }

  @media (max-width: 900px) {
    .boards { grid-template-columns: minmax(0, 1fr); }
  }

  @media (max-width: 720px) {
    .tiles { grid-template-columns: minmax(0, 1fr); }
  }

</style>
