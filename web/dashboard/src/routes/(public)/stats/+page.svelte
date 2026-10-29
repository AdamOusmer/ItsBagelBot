<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';
  import { subscribe } from '@bagel/ui/lib/raf-loop';
  import { livePoll } from '@bagel/ui/lib/live-poll';
  import { onMount, untrack } from 'svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import CounterCard from '@bagel/ui/svelte/CounterCard.svelte';
  import CommunityCard from '@bagel/ui/svelte/CommunityCard.svelte';
  import RankingCard from '@bagel/ui/svelte/RankingCard.svelte';
  import StatsPageLayout from '$lib/components/stats/StatsPageLayout.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import PublicHead from '$lib/components/public/PublicHead.svelte';
  import type { PageData } from './$types';
  import { commandsHref } from '@bagel/kit/site-links';
  import { formatStatTotal, validBoards, validStats } from '$lib/stats-values';
  import { frameDelta, resumedAfterGap, rewindTotals, seedFrame, stepFrame, targetFrame } from '$lib/stats-odometer';
  import { isLoginShape } from '$lib/public-login';
  import { formatCounterValue } from '@bagel/kit/validation';

  let { data }: { data: PageData } = $props();

  const { t, locale } = getI18n();

  const POLL_MS = 2000;

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

  const emptyBoards = () => ({
    channels: [],
    feed: { total: '0', ranked: '0', entries: [] },
    degraded: true
  });

  let boards = $state(untrack(() => validBoards(data.boards) ?? emptyBoards()));

  let display = $state(seedFrame(seed));

  let snapAt = 0;
  let lastFrame = 0;
  let reduced = false;
  let missedWhileHidden = false;

  function tick(now: number): boolean {
    if (resumedAfterGap(now, lastFrame)) {
      snap(now);
      return true;
    }
    const dt = frameDelta(now, lastFrame);
    lastFrame = now;
    if (degraded) return true;
    display = stepFrame(display, targetFrame(live, snapAt, now), dt);
    return true;
  }

  function snap(now: number): void {
    lastFrame = now;
    display = targetFrame(live, snapAt, now);
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
    if (reduced || prev.degraded) {
      snap(snapAt);
      return;
    }
    display = rewindTotals(display, prev, next);
  }
  async function fetchJson(path: string): Promise<unknown> {
    const res = await fetch(path, { headers: { accept: 'application/json' } });
    if (!res.ok) throw new Error(`${path} ${res.status}`);
    return res.json();
  }

  function applyStats(result: PromiseSettledResult<unknown>, resync: boolean): void {
    if (result.status !== 'fulfilled') return;
    applySnapshot(result.value);
    if (resync) snap(performance.now());
  }

  async function poll(): Promise<boolean> {
    if (document.hidden) {
      missedWhileHidden = true;
      return false;
    }
    const resync = missedWhileHidden;
    missedWhileHidden = false;
    const [stats, next] = await Promise.allSettled([fetchJson('/stats/data'), fetchJson('/stats/boards')]);
    applyStats(stats, resync);
    if (next.status === 'fulfilled') boards = validBoards(next.value) ?? emptyBoards();
    return false;
  }

  onMount(() => {
    reduced = prefersReducedMotion();
    const now = performance.now();
    snapAt = now;
    snap(now);
    const stopOdometer = reduced ? () => {} : subscribe(tick);
    const stopPoll = livePoll(poll, {
      firstDelayMs: 0,
      delayMs: () => POLL_MS,
      timeoutMs: Infinity,
      refreshOnVisible: true
    });

    return () => {
      stopPoll();
      stopOdometer();
    };
  });

  const rateFmt = new Intl.NumberFormat(locale, { minimumFractionDigits: 1, maximumFractionDigits: 1 });
  const compactFmt = new Intl.NumberFormat(locale, {
    notation: 'compact',
    maximumFractionDigits: 2
  });
  const PENDING = t('stats.rateNa');

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
      tone: 'accent' as const,
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
      tone: 'warm' as const,
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
  const channelHref = (row: { id: string; name: string }) =>
    commandsHref(isLoginShape(row.name) ? row.name : row.id);

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
    Array.from({ length: 12 }, (_, i) => traffic[i]?.name || traffic[i]?.id || `ItsBagelBot-${i}`)
  );
  const feedArtworkNames = $derived(
    Array.from({ length: 3 }, (_, i) => feed.entries[i]?.name || feed.entries[i]?.id || `ItsBagelBot-feed-${i}`)
  );
  const friendPalettes = [
    { head: '#62a593', eye: '#111113' },
    { head: '#eadbbd', eye: '#111113' },
    { head: '#f6efe1', eye: '#111113' }
  ];
</script>

<PublicHead title={t('stats.title')} description={t('stats.metaDescription')} url="https://stats.itsbagelbot.com/" />

<main class="stats-page">
  <StatsPageLayout
    arrangement="playful"
    style={t('stats.pageHeadline').length > 9 ? '--stats-heading-size: clamp(30px, 9vw, 64px)' : undefined}
  >
    {#snippet heading()}
      <div class="stats-heading">
        <Tag tone="live" mark="solid" sweep>{t('stats.liveNote')}</Tag>
        <Heading level={1} class="stats-layout__title">{t('stats.pageHeadline')}<span class="ink-tan" aria-hidden="true">.</span></Heading>
        <Text size="sm" tone="muted">{t('stats.pageTagline')}</Text>
      </div>
    {/snippet}

    {#snippet crowd()}
      {#each crowdNames as name, i (`${i}:${name}`)}
        <span aria-hidden="true"><Bolota {name} size={100} palette={friendPalettes[i % friendPalettes.length]} motionState="idle" active gate /></span>
      {/each}
    {/snippet}

    {#snippet notice()}
      {#if degraded}
        <AlertBanner tone="warning">{t('stats.degraded')}</AlertBanner>
      {:else if boards.degraded}
        <AlertBanner tone="warning">{t('stats.boardsUnavailable')}</AlertBanner>
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
            <span aria-hidden="true"><Bolota name={tile.id} size={90} palette={friendPalettes[tile.tone === 'accent' ? 1 : 0]} active gate /></span>
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
          <div class="feed-foot"><Text size="xs" tone="muted">{t('stats.feedBoardEmpty')}</Text></div>
        {:else}
          <ol class="feed-list">
            {#each feed.entries as row, i (row.id)}
              <li>
                <Text as="span" size="xs" tone="muted" mono>{i + 1}</Text>
                <span aria-hidden="true"><Bolota name={row.name || row.id} size={38} /></span>
                <span class="feed-channel">
                  {#if row.name}
                    <a class="feed-link" href={channelHref(row)}>{row.name}</a>
                  {:else}
                    <Text as="span" size="sm" tone="muted">{t('stats.unknownChannel')}</Text>
                  {/if}
                </span>
                <Text as="span" size="xs" tone="accent" mono>{formatCounterValue(row.count, locale)}</Text>
              </li>
            {/each}
          </ol>
          <div class="feed-foot"><Text size="xs" tone="muted">{t('stats.feedRankedNote', { count: formatCounterValue(feed.ranked, locale) })}</Text></div>
        {/if}
      </CommunityCard>
    {/snippet}
  </StatsPageLayout>
</main>

<style>
  .stats-page { min-width: 0; }
  .stats-heading { display: flex; flex-direction: column; align-items: center; gap: 14px; }
  .ink-tan { color: var(--bb-tan); }
  .feeding-friends { display: flex; align-items: end; justify-content: center; }
  .feeding-friends :global(svg + svg) { margin-left: -5px; }
  .feed-heading { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; margin-bottom: 10px; }
  .feed-list { list-style: none; margin: 0; padding: 0; }
  .feed-list li { display: grid; grid-template-columns: 16px 38px minmax(0, 1fr) auto; align-items: center; gap: 9px; padding: 11px 0; border-bottom: 1px solid var(--bb-border); font-variant-numeric: tabular-nums; }
  .feed-channel { min-width: 0; overflow-wrap: anywhere; font-size: var(--bb-text-sm); }
  .feed-link { color: var(--bb-white); text-decoration: none; }
  .feed-link:hover { color: var(--bb-tan-light); }
  .feed-link:focus-visible { outline: 2px solid var(--bb-focus); outline-offset: 4px; }
  .feed-foot { margin-top: 14px; }
  @media (max-width: 420px) {
    .feed-list li { grid-template-columns: 12px 30px minmax(0, 1fr) auto; gap: 6px; }
    .feed-list li :global(svg) { width: 30px; height: 30px; }
  }
</style>
