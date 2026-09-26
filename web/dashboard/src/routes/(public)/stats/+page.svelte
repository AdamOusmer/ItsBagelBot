<script lang="ts">
  import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { onMount, untrack } from 'svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import CounterCard from '@bagel/ui/svelte/CounterCard.svelte';
  import CommunityCard from '@bagel/ui/svelte/CommunityCard.svelte';
  import RankingCard from '@bagel/ui/svelte/RankingCard.svelte';
  import StatsPageLayout from '@bagel/ui/svelte/StatsPageLayout.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import type { PageData } from './$types';
  import { commandsHref } from '@bagel/kit/site-links';
  import { visibleEventSource } from '$lib/visible-stream';
  import { exactDisplay, formatStatTotal, validBoards, validStats } from '$lib/stats-values';
  import { formatCounterValue } from '@bagel/kit/validation';

  let { data }: { data: PageData } = $props();

  const { t, locale } = getI18n();

  const POLL_MS = 5000;
  const INTRO_MS = 900;
  const TAU_MS = 200;
  const RATE_TAU_MS = 300;
  const MAX_DT_MS = 250;
  const MAX_PROJECT_S = 10;
  const MIN_CRAWL = 0.25;

  const seed = untrack(() => validStats(data.stats) ?? {
    messages_total: '0',
    events_total: '0',
    msg_rate: null,
    event_rate: null,
    msg_rate_now: null,
    event_rate_now: null,
    degraded: true
  });
  let live = $state(seed);
  let degraded = $state(seed.degraded);

  let boards = $state(untrack(() => validBoards(data.boards) ?? {
    channels: [],
    feed: { total: '0', ranked: '0', entries: [] },
    degraded: true
  }));

  let display = $state({
    messages: exactDisplay(Number(seed.messages_total)),
    events: exactDisplay(Number(seed.events_total)),
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
      messages: exactDisplay(Number(live.messages_total) + msgAvg * secs),
      events: exactDisplay(Number(live.events_total) + eventAvg * secs),
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
    return exactDisplay(Math.max(lerp(cur, target, k), cur + rate * (dt / 1000) * MIN_CRAWL));
  }

  function tick(now: number): void {
    const dt = Math.min(now - lastFrame, MAX_DT_MS);
    lastFrame = now;
    if (degraded) {
      raf = requestAnimationFrame(tick);
      return;
    }
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

  function applySnapshot(raw: unknown): void {
    const next = validStats(raw);
    if (!next) {
      degraded = true;
      return;
    }
    degraded = next.degraded;
    if (next.degraded) return;
    const prev = live;
    live = next;
    snapAt = performance.now();
    if (reduced) {
      snap(snapAt);
      return;
    }
    if (BigInt(next.messages_total) < BigInt(prev.messages_total)) display.messages = exactDisplay(Number(next.messages_total));
    if (BigInt(next.events_total) < BigInt(prev.events_total)) display.events = exactDisplay(Number(next.events_total));
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
      boards = validBoards(await res.json()) ?? {
        channels: [], feed: { total: '0', ranked: '0', entries: [] }, degraded: true
      };
    } catch {
    }
  }

  function attachStream(es: EventSource, reopened: boolean): void {
    let resync = reopened;
    es.onopen = () => (streamDown = false);
    es.onerror = () => (streamDown = true);
    es.onmessage = (ev) => {
      try {
        applySnapshot(JSON.parse(ev.data));
      } catch {
        return;
      }
      if (!resync) return;
      resync = false;
      snap(performance.now());
    };
    es.addEventListener('boards', (ev) => {
      try {
        boards = validBoards(JSON.parse((ev as MessageEvent<string>).data)) ?? {
          channels: [], feed: { total: '0', ranked: '0', entries: [] }, degraded: true
        };
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

  const rateFmt = new Intl.NumberFormat(locale, { maximumFractionDigits: 1 });
  const compactFmt = new Intl.NumberFormat(locale, {
    notation: 'compact',
    maximumFractionDigits: 2
  });
  const PENDING = '-';

  // Keep the locale's compact suffix separate for the larger counter type.
  // The exact odometer reading remains printed below it, so compact rounding
  // never hides the live count (or substitutes a made-up scale).
  function compactTotal(raw: string, animated: number): { value: string; unit: string } {
    const exact = BigInt(raw);
    const value = exact > BigInt(Number.MAX_SAFE_INTEGER) ? exact : Math.round(animated);
    const parts = compactFmt.formatToParts(value);
    return {
      value: parts.filter((part) => part.type !== 'compact').map((part) => part.value).join('').trim(),
      unit: parts.find((part) => part.type === 'compact')?.value ?? ''
    };
  }

  const tiles = $derived([
    {
      id: 'messages',
      label: t('stats.messagesShortLabel'),
      ...compactTotal(live.messages_total, display.messages),
      detail: t('stats.processedTotal', { count: formatStatTotal(live.messages_total, display.messages, locale) }),
      rate: (live.msg_rate_now ?? live.msg_rate) === null ? PENDING : rateFmt.format(display.msgRate),
      average: live.msg_rate === null ? null : rateFmt.format(live.msg_rate),
      rateLabel: t('stats.messageRateLabel'),
      tone: 'green' as const,
      tilt: 'left' as const
    },
    {
      id: 'events',
      label: t('stats.eventsShortLabel'),
      ...compactTotal(live.events_total, display.events),
      detail: t('stats.processedTotal', { count: formatStatTotal(live.events_total, display.events, locale) }),
      rate: (live.event_rate_now ?? live.event_rate) === null ? PENDING : rateFmt.format(display.eventRate),
      average: live.event_rate === null ? null : rateFmt.format(live.event_rate),
      rateLabel: t('stats.eventRateLabel'),
      tone: 'tan' as const,
      tilt: 'right' as const
    }
  ]);

  const traffic = $derived(boards.channels);
  const feed = $derived(boards.feed);
  let rankBy = $state(t('stats.colMessages'));
  const rankKey = $derived(rankBy === t('stats.colEvents') ? 'events' : 'messages');

  // Both boards label a channel with the name the fleet stored for it, which is
  // a display name: localized names are not necessarily valid Twitch logins.
  // Use the id for those names, and never turn an unknown name into a bad link.
  const LOGIN_SHAPE = /^[A-Za-z0-9_]{1,25}$/;
  const channelHref = (row: { id: string; name: string }) =>
    commandsHref(LOGIN_SHAPE.test(row.name) ? row.name : row.id);

  // The server sends the message-ranked subset. The Events control only sorts
  // that same set; its localized note does not claim a global event ranking.
  const rankingItems = $derived(
    [...traffic]
      .sort((a, b) => {
        const difference = BigInt(b[rankKey]) - BigInt(a[rankKey]) || BigInt(b.messages) - BigInt(a.messages);
        return difference > 0n ? 1 : difference < 0n ? -1 : 0;
      })
      .map((row) => ({
        id: row.id,
        label: row.name || t('stats.unknownChannel'),
        href: row.name ? channelHref(row) : undefined,
        // Approximation is only for decorative bar width; sorting and labels stay exact.
        value: Number(row[rankKey]),
        valueLabel: formatCounterValue(row[rankKey], locale),
        secondary: t(rankKey === 'messages' ? 'stats.eventsCount' : 'stats.messagesCount', {
          count: formatCounterValue(row[rankKey === 'messages' ? 'events' : 'messages'], locale)
        })
      }))
  );

  // The crowd uses names from the current board when available. Seed-only
  // fallbacks are decorative, never sample channels or invented statistics.
  const crowdNames = $derived(
    Array.from({ length: 6 }, (_, i) => traffic[i]?.name || traffic[i]?.id || `ItsBagelBot-${i}`)
  );
  const feedArtworkNames = $derived(
    Array.from({ length: 3 }, (_, i) => feed.entries[i]?.name || feed.entries[i]?.id || `ItsBagelBot-feed-${i}`)
  );
  const friendPalettes = [
    { head: '#52b788', eye: '#0a0a0a' },
    { head: '#e0c49a', eye: '#0a0a0a' },
    { head: '#f5e6cf', eye: '#0a0a0a' }
  ];
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

<main class="stats-page">
  <StatsPageLayout
    arrangement="playful"
    style={t('stats.pageHeadline').length > 9 ? '--bb-stats-heading-size: clamp(30px, 9vw, 64px)' : undefined}
  >
    {#snippet heading()}
      <div class="stats-heading">
        <span class="bb-tag bb-tag--live"><i class="bb-mark" aria-hidden="true"></i>{t('stats.liveNote')}<i class="bb-sweep" aria-hidden="true"></i></span>
        <Heading level={1}>{t('stats.pageHeadline')}<span class="ink-tan" aria-hidden="true">.</span></Heading>
        <Text size="sm" tone="muted">{t('stats.pageTagline')}</Text>
      </div>
    {/snippet}

    {#snippet crowd()}
      {#each crowdNames as name, i (`${i}:${name}`)}
        <span aria-hidden="true"><Bolota {name} size={100} palette={friendPalettes[i % friendPalettes.length]} active gate /></span>
      {/each}
    {/snippet}

    {#snippet notice()}
      {#if degraded}
        <AlertBanner variant="warn">{t('stats.degraded')}</AlertBanner>
      {:else if boards.degraded}
        <AlertBanner variant="warn">{t('stats.boardsUnavailable')}</AlertBanner>
      {/if}
    {/snippet}

    {#snippet counters()}
      {#each tiles as tile (tile.id)}
        <CounterCard
          label={tile.label}
          value={tile.value}
          unit={tile.unit}
          detail={tile.detail}
          rate={tile.rate}
          rateUnit={tile.rate === PENDING ? '' : t('stats.perSecond')}
          rateLabel={tile.average === null ? t('stats.rightNow') : `${t('stats.rightNow')} · ${t('stats.minuteAverage', { rate: tile.average })}`}
          period={t('stats.allTime')}
          tone={tile.tone}
          tilt={tile.tilt}
          appearance="solid"
          style={tile.value.length > 7 ? '--counter-card-value-size: clamp(32px, 12cqi, 84px)' : undefined}
          aria-description={tile.rateLabel}
        >
          {#snippet artwork()}
            <span aria-hidden="true"><Bolota name={tile.id} size={90} palette={friendPalettes[tile.tone === 'green' ? 1 : 0]} active gate /></span>
          {/snippet}
        </CounterCard>
      {/each}
    {/snippet}

    {#snippet ranking()}
      <RankingCard
        title={t('stats.trafficBoardTitle')}
        description={t('stats.trafficBoardNote')}
        items={rankingItems}
        emptyLabel={t('stats.boardEmpty')}
      >
        {#snippet actions()}
          <SegmentedControl
            options={[t('stats.colMessages'), t('stats.colEvents')]}
            bind:value={rankBy}
            label={t('stats.rankBy')}
          />
        {/snippet}
        {#snippet leading(item)}
          <span aria-hidden="true"><Bolota name={item.href ? item.label : item.id} size={48} /></span>
        {/snippet}
      </RankingCard>
    {/snippet}

    {#snippet community()}
      <CommunityCard
        title={t('stats.feedingsTitle')}
        subtitle={t('stats.feedBoardNote')}
        total={formatCounterValue(feed.total, locale)}
        period={t('stats.allTime')}
      >
        {#snippet artwork()}
          <div class="feeding-friends" aria-hidden="true">
            {#each feedArtworkNames as name, i (`${i}:${name}`)}
              <Bolota {name} size={i === 1 ? 78 : 65} palette={friendPalettes[i]} active gate />
            {/each}
          </div>
        {/snippet}
        <div class="feed-heading"><Text size="sm">{t('stats.topFeeders')}</Text><Text size="xs" tone="muted">{t('stats.feedings')}</Text></div>
        {#if feed.entries.length === 0}
          <p class="empty">{t('stats.feedBoardEmpty')}</p>
        {:else}
          <ol class="feed-list">
            {#each feed.entries as row, i (row.id)}
              <li>
                <span class="feed-rank">{i + 1}</span>
                <span aria-hidden="true"><Bolota name={row.name || row.id} size={38} /></span>
                <span class="feed-channel">
                  {#if row.name}
                    <a class="feed-link" href={channelHref(row)}>{row.name}</a>
                  {:else}
                    <span class="unnamed">{t('stats.unknownChannel')}</span>
                  {/if}
                </span>
                <span class="feed-count">{formatCounterValue(row.count, locale)}</span>
              </li>
            {/each}
          </ol>
          <p class="feed-note">{t('stats.feedRankedNote', { count: formatCounterValue(feed.ranked, locale) })}</p>
        {/if}
      </CommunityCard>
    {/snippet}
  </StatsPageLayout>
</main>

<style>
  .stats-page { min-width: 0; }
  .stats-heading { display: flex; flex-direction: column; align-items: flex-start; gap: 14px; }
  .ink-tan { color: var(--bb-tan); }
  .feeding-friends { display: flex; align-items: end; justify-content: center; }
  .feeding-friends :global(svg + svg) { margin-left: -5px; }
  .feed-heading { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; margin-bottom: 10px; }
  .feed-list { list-style: none; margin: 0; padding: 0; }
  .feed-list li { display: grid; grid-template-columns: 16px 38px minmax(0, 1fr) auto; align-items: center; gap: 9px; padding: 11px 0; border-bottom: 1px solid var(--bb-border); }
  .feed-rank { color: var(--bb-muted); font-family: var(--bb-font-mono); font-size: 11px; }
  .feed-channel { min-width: 0; overflow-wrap: anywhere; font-size: 13px; }
  .feed-link { color: var(--bb-white); text-decoration: none; }
  .feed-link:hover { color: var(--bb-tan-light); }
  .feed-link:focus-visible { outline: 2px solid var(--bb-green-glow); outline-offset: 4px; }
  .feed-count { font-family: var(--bb-font-mono); font-size: 12px; color: var(--bb-tan-light); font-variant-numeric: tabular-nums; }
  .feed-note, .empty { color: var(--bb-muted); margin: 14px 0 0; font-size: 12px; line-height: 1.5; }
  .unnamed { color: var(--bb-muted); }
  @media (max-width: 420px) {
    .feed-list li { grid-template-columns: 12px 30px minmax(0, 1fr) auto; gap: 6px; }
    .feed-list li :global(svg) { width: 30px; height: 30px; }
    .feed-count { font-size: 11px; }
  }
</style>
