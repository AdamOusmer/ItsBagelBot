<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';
  import { subscribe } from '@bagel/ui/lib/raf-loop';
  import { livePoll } from '@bagel/ui/lib/live-poll';
  import { onMount, untrack } from 'svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import DonateCard from '@bagel/ui/svelte/DonateCard.svelte';
  import LightField from '@bagel/ui/svelte/LightField.svelte';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import VisuallyHidden from '@bagel/ui/svelte/VisuallyHidden.svelte';
  import PublicHead from '#lib/components/public/PublicHead.svelte';
  import type { PageData } from './$types';
  import { SITE, commandsHref } from '@bagel/kit/site-links';
  import { formatStatTotal, validBoards, validStats } from '#lib/stats-values';
  import { frameDelta, resumedAfterGap, rewindTotals, seedFrame, stepFrame, targetFrame } from '#lib/stats-odometer';
  import { isLoginShape } from '#lib/public-login';
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
      tone: 'green' as const
    },
    {
      id: 'events',
      label: t('stats.eventsShortLabel'),
      ...compactTotal(live.events_total, display.events),
      detail: t('stats.processedTotal', { count: formatStatTotal(live.events_total, display.events, locale) }),
      rate: (live.event_rate_now ?? live.event_rate) === null ? PENDING : rateFmt.format(display.eventRate),
      average: live.event_rate === null ? null : rateFmt.format(live.event_rate),
      rateLabel: t('stats.eventRateLabel'),
      tone: 'tan' as const
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
        art: row.name || row.id,
        // Approximation is only for decorative bar width; sorting and labels stay exact.
        value: Number(row[rankKey]),
        valueLabel: formatCounterValue(row[rankKey], locale)
      }))
  );
  const rankingTop = $derived(rankingItems[0]?.value ?? 0);

  const feedTop = $derived(Number(feed.entries[0]?.count ?? 0));
  const share = (value: number, top: number): number => (top > 0 ? Math.max(0.04, value / top) : 0);

  // Artwork names come from the current boards when available. Seed-only
  // fallbacks are decorative, never sample channels or invented statistics.
  const artNames = (rows: { name: string; id: string }[], seed: string, count: number) =>
    Array.from({ length: count }, (_, i) => rows[i]?.name || rows[i]?.id || `ItsBagelBot-${seed}-${i}`);
  const crewNames = $derived(artNames(traffic, 'crew', 5));
  const trafficTrio = $derived(artNames(rankingItems.map((item) => ({ name: item.art, id: item.id })), 'traffic', 3));
  const feedTrio = $derived(artNames(feed.entries, 'feed', 3));

  const GREEN = { head: '#62a593', eye: '#111113' };
  const TAN = { head: '#eadbbd', eye: '#111113' };
  const PALE = { head: '#f6efe1', eye: '#111113' };
  const palettes = [GREEN, TAN, PALE];
  const tilePalette = { green: GREEN, tan: TAN };
  const donors = [
    { name: 'oven_mitt', size: 64, palette: PALE },
    { name: 'glazed_val', size: 88, palette: GREEN },
    { name: 'caramel_cass', size: 64, palette: TAN }
  ];
</script>

<PublicHead title={t('stats.title')} description={t('stats.metaDescription')} url="https://stats.itsbagelbot.com/" />

<div class="sky" aria-hidden="true">
  <LightField />
  <div class="bb-orb bb-orb--halo bb-orb--green orb orb--green"></div>
  <div class="bb-orb bb-orb--halo bb-orb--tan orb orb--tan"></div>
</div>

<main class="st">
  <header class="st-head">
    <div class="st-head__text">
      <span class="st-live"><span class="st-beacon" aria-hidden="true"></span><span class="st-live__label"><span data-fit="">{t('stats.liveNote')}</span></span></span>
      <h1 class="st-title"><span class="st-title__fit"><span data-fit=""><span class="st-word">{t('stats.pageHeadline')}</span></span></span></h1>
      <p class="st-tagline"><span data-fit="block">{t('stats.pageTagline')}</span></p>
    </div>
    <div class="st-crew" aria-hidden="true">
      {#each crewNames as name, i (`${i}:${name}`)}
        <span class="st-mate st-mate--{i}"><Bolota {name} size={i === 2 ? 150 : i % 2 ? 96 : 82} palette={palettes[i % palettes.length]} motionState="idle" follow active gate /></span>
      {/each}
    </div>
  </header>

  {#if degraded}
    <AlertBanner tone="warning">{t('stats.degraded')}</AlertBanner>
  {:else if boards.degraded}
    <AlertBanner tone="warning">{t('stats.boardsUnavailable')}</AlertBanner>
  {/if}

  <section class="st-counters">
    {#each tiles as tile (tile.id)}
      <div class="st-counter st-counter--{tile.tone}">
        <div class="st-counter__face" aria-hidden="true"><Bolota name={tile.id} size={92} palette={tilePalette[tile.tone]} motionState="idle" active gate /></div>
        <Card class="st-counter__card">
          <div class="st-counter__top">
            <span class="st-counter__label"><span data-fit="">{tile.label}</span></span>
            <span class="st-counter__period"><span data-fit="">{t('stats.allTime')}</span></span>
          </div>
          <div class="st-counter__value">{tile.value}<span class="st-counter__unit">{tile.unit}</span></div>
          <div class="st-counter__exact"><span data-fit="">{tile.detail}</span></div>
          <div class="st-rule"></div>
          <div class="st-counter__rate">
            <span class="st-counter__now"><span class="st-beacon" aria-hidden="true"></span><VisuallyHidden>{tile.rateLabel}</VisuallyHidden>{tile.rate}{#if tile.rate !== PENDING}<small class="st-counter__per">{t('stats.perSecond')}</small>{/if}</span>
            <span class="st-counter__avg"><span data-fit="block">{tile.average === null ? t('stats.rightNow') : `${t('stats.rightNow')} · ${t('stats.minuteAverage', { rate: tile.average })}`}</span></span>
          </div>
        </Card>
      </div>
    {/each}
  </section>

  <section class="st-boards">
    <div class="st-board">
      <div class="st-board__faces" aria-hidden="true">
        {#each trafficTrio as name, i (`${i}:${name}`)}
          <span style="--s: {i === 1 ? 84 : 62}px"><Bolota {name} size={i === 1 ? 84 : 62} palette={palettes[i]} motionState="idle" active gate /></span>
        {/each}
      </div>
      <Card class="st-board__card">
        <div class="st-board__head">
          <div class="st-board__intro">
            <span class="st-board__eyebrow"><span data-fit="">{t('stats.boardsEyebrow')}</span></span>
            <h2 class="st-board__title"><span data-fit="">{t('stats.trafficBoardTitle')}</span></h2>
          </div>
          <SegmentedControl options={[t('stats.colMessages'), t('stats.colEvents')]} bind:value={rankBy} label={t('stats.rankBy')} />
        </div>
        {#if rankingItems.length === 0}
          <p class="st-empty"><span data-fit="block">{t('stats.boardEmpty')}</span></p>
        {:else}
          <div class="st-table" role="table" aria-label={t('stats.trafficBoardTitle')}>
            <div class="st-row st-row--head" role="row">
              <span class="st-cell st-cell--rank" role="columnheader">{t('stats.colRank')}</span>
              <span class="st-cell" role="columnheader"><span data-fit="">{t('stats.colChannel')}</span></span>
              <span class="st-cell st-cell--num" role="columnheader"><span data-fit="">{rankBy}</span></span>
            </div>
            {#each rankingItems as item, i (item.id)}
              <div class="st-row" role="row" style="--share: {share(item.value, rankingTop)}">
                <span class="st-cell st-cell--rank" role="cell">{i + 1}</span>
                <span class="st-cell st-cell--name" role="cell">
                  <span class="st-face" aria-hidden="true"><Bolota name={item.art} size={36} /></span>
                  {#if item.href}<a class="st-name" href={item.href}>{item.label}</a>{:else}<span class="st-name st-name--muted">{item.label}</span>{/if}
                </span>
                <span class="st-cell st-cell--num st-cell--main" role="cell">
                  <span class="st-value">{item.valueLabel}</span>
                  <span class="st-meter" aria-hidden="true"><span class="st-meter__fill"></span></span>
                </span>
              </div>
            {/each}
          </div>
        {/if}
      </Card>
    </div>

    <div class="st-board st-board--feed">
      <div class="st-board__faces" aria-hidden="true">
        {#each feedTrio as name, i (`${i}:${name}`)}
          <span style="--s: {i === 1 ? 84 : 62}px"><Bolota {name} size={i === 1 ? 84 : 62} palette={palettes[(i + 1) % palettes.length]} expression="excited" motionState="idle" active gate /></span>
        {/each}
      </div>
      <Card class="st-board__card" tone="accent">
        <div class="st-board__head">
          <div class="st-board__intro">
            <span class="st-board__eyebrow st-board__eyebrow--tan"><span data-fit="">{t('stats.feedBoardTitle')}</span></span>
            <h2 class="st-board__title"><span data-fit="">{t('stats.feedingsTitle')}</span></h2>
          </div>
          <div class="st-feed-total">
            <span class="st-feed-total__num">{formatCounterValue(feed.total, locale)}</span>
            <span class="st-feed-total__sub"><span data-fit="">{t('stats.allTime')}</span></span>
          </div>
        </div>
        {#if feed.entries.length === 0}
          <p class="st-empty"><span data-fit="block">{t('stats.feedBoardEmpty')}</span></p>
        {:else}
          <div class="st-table" role="table" aria-label={t('stats.feedingsTitle')}>
            <div class="st-row st-row--head" role="row">
              <span class="st-cell st-cell--rank" role="columnheader">{t('stats.colRank')}</span>
              <span class="st-cell" role="columnheader"><span data-fit="">{t('stats.topFeeders')}</span></span>
              <span class="st-cell st-cell--num" role="columnheader"><span data-fit="">{t('stats.feedings')}</span></span>
            </div>
            {#each feed.entries as row, i (row.id)}
              <div class="st-row" role="row" style="--share: {share(Number(row.count), feedTop)}">
                <span class="st-cell st-cell--rank" role="cell">{i + 1}</span>
                <span class="st-cell st-cell--name" role="cell">
                  <span class="st-face" aria-hidden="true"><Bolota name={row.name || row.id} size={36} /></span>
                  {#if row.name}<a class="st-name" href={channelHref(row)}>{row.name}</a>{:else}<span class="st-name st-name--muted">{t('stats.unknownChannel')}</span>{/if}
                </span>
                <span class="st-cell st-cell--num st-cell--main" role="cell">
                  <span class="st-value st-value--tan">{formatCounterValue(row.count, locale)}</span>
                  <span class="st-meter st-meter--tan" aria-hidden="true"><span class="st-meter__fill"></span></span>
                </span>
              </div>
            {/each}
          </div>
          <p class="st-feed-note"><span data-fit="">{t('stats.feedRankedNote', { count: formatCounterValue(feed.ranked, locale) })}</span></p>
        {/if}
      </Card>
    </div>
  </section>

  <DonateCard
    class="st-donate"
    label={t('leaderboard.donateLabel')}
    title={t('leaderboard.donateTitle')}
    text={t('leaderboard.donateText')}
    cta={t('leaderboard.donateCta')}
    href={SITE.kofi}
    note={t('leaderboard.donateNote')}
  >
    {#each donors as donor (donor.name)}
      <span style="--s: {donor.size}px"><Bolota name={donor.name} size={donor.size} palette={donor.palette} expression="excited" motionState="idle" active gate /></span>
    {/each}
  </DonateCard>
</main>

<style>
  .sky {
    position: fixed;
    inset: 0;
    z-index: 0;
    overflow: hidden;
    pointer-events: none;
  }

  .orb { --bb-orb-opacity: 0.16; }
  .orb--green { width: 520px; height: 520px; top: -140px; right: -80px; }
  .orb--tan { width: 420px; height: 420px; bottom: -120px; left: -100px; --bb-orb-pulse-delay: 400ms; }

  .st {
    position: relative;
    z-index: 1;
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-8);
    width: min(100% - 32px, 1180px);
    margin-inline: auto;
    padding: calc(var(--bb-nav-height) + var(--bb-space-8)) 0 var(--bb-space-section);
  }

  .st-head {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1.1fr);
    align-items: end;
    gap: var(--bb-space-6);
  }

  .st-head__text {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--bb-space-3);
    padding-bottom: var(--bb-space-5);
    min-width: 0;
  }

  .st-live {
    display: flex;
    align-items: center;
    gap: var(--bb-space-2);
    width: min(100%, 14rem);
    font-size: var(--bb-text-md);
    font-weight: 700;
    letter-spacing: 0.02em;
    color: var(--bb-green-glow);
    text-shadow: 0 0 18px rgba(var(--bb-green-glow-rgb), 0.45);
  }

  .st-live__label {
    display: flex;
    flex: 1 1 0;
    min-width: 0;
  }

  .st-title {
    width: 100%;
    margin: 0;
    font-family: var(--bb-font-display);
    font-size: clamp(2.75rem, 6vw, 4.5rem);
    font-weight: 500;
    letter-spacing: var(--bb-tracking-display);
    line-height: 1.04;
  }

  .st-title__fit {
    display: flex;
    width: 100%;
  }

  .st-word {
    display: inline-block;
    background: linear-gradient(180deg, var(--bb-white) 40%, rgba(var(--bb-tan-light-rgb), 0.55));
    background-clip: text;
    -webkit-background-clip: text;
    color: transparent;
    filter: drop-shadow(0 0 36px rgba(var(--bb-tan-rgb), 0.18));
  }

  .st-tagline {
    width: min(100%, 40ch);
    height: calc(2 * 1lh);
    margin: 0;
    font-size: var(--bb-text-md);
    line-height: 1.5;
    color: var(--bb-muted-light);
  }

  .st-crew {
    position: relative;
    height: 210px;
  }

  .st-mate {
    position: absolute;
    bottom: 0;
    animation: st-bob 6.5s ease-in-out infinite alternate;
  }

  .st-mate--0 { left: 4%; bottom: 30px; animation-delay: -1s; }
  .st-mate--1 { left: 20%; animation-delay: -2.6s; }
  .st-mate--2 { left: 38%; animation-delay: -0.4s; }
  .st-mate--3 { left: 66%; animation-delay: -3.3s; }
  .st-mate--4 { left: 82%; bottom: 36px; animation-delay: -1.8s; }

  .st-beacon {
    position: relative;
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--bb-green-glow);
    box-shadow: 0 0 10px var(--bb-green-glow);
  }

  .st-beacon::after {
    content: "";
    position: absolute;
    inset: -4px;
    border-radius: 50%;
    border: 1px solid var(--bb-green-glow);
    animation: st-ping 2.4s var(--bb-ease-out-expo) infinite;
  }

  .st-rule {
    height: 1px;
    margin: var(--bb-space-3) 0 var(--bb-space-2);
    background: var(--bb-border);
  }

  .st-counters {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--bb-space-5);
    padding-top: var(--bb-space-5);
  }

  .st-counter {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .st-counter__face {
    position: relative;
    z-index: 2;
    display: flex;
    height: 92px;
    margin-bottom: -42px;
    padding-left: var(--bb-space-6);
  }

  .st-counter :global(.st-counter__card) {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-2);
    padding: calc(var(--bb-space-7) + 8px) var(--bb-space-7) var(--bb-space-6);
  }

  .st-counter--green :global(.st-counter__card) { --card-halo-rgb: var(--bb-halo-green-rgb); }

  .st-counter__top {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: var(--bb-space-3);
  }

  .st-counter__label {
    display: flex;
    flex: 1 1 0;
    min-width: 0;
    font-weight: 600;
  }

  .st-counter--green .st-counter__label { color: var(--bb-tier-free); }
  .st-counter--tan .st-counter__label { color: var(--bb-tier-paid); }

  .st-counter__period {
    display: flex;
    justify-content: flex-end;
    width: 7rem;
    font-size: var(--bb-text-xs);
    color: var(--bb-muted);
  }

  .st-counter__value {
    display: flex;
    align-items: baseline;
    gap: var(--bb-space-2);
    margin-top: var(--bb-space-3);
    font-family: var(--bb-font-display);
    font-size: clamp(3.5rem, 7vw, 5.5rem);
    font-weight: 500;
    font-variant-numeric: tabular-nums;
    letter-spacing: -0.05em;
    line-height: 1;
    color: var(--bb-paper);
  }

  .st-counter__unit {
    font-size: 0.42em;
    letter-spacing: -0.02em;
    color: var(--bb-muted-light);
  }

  .st-counter__exact {
    display: flex;
    font: var(--bb-text-sm)/1.4 var(--bb-font-mono);
    color: var(--bb-muted);
  }

  .st-counter__rate {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--bb-space-3);
  }

  .st-counter__now {
    display: inline-flex;
    flex: none;
    align-items: center;
    gap: var(--bb-space-2);
    font-family: var(--bb-font-display);
    font-size: 1.6rem;
    font-weight: 500;
    font-variant-numeric: tabular-nums;
    color: var(--bb-paper);
  }

  .st-counter__per {
    font-size: var(--bb-text-sm);
    color: var(--bb-muted);
  }

  .st-counter__avg {
    flex: 1 1 0;
    min-width: 0;
    height: calc(2 * 1lh);
    font-size: var(--bb-text-sm);
    text-align: right;
    color: var(--bb-muted);
  }

  .st-boards {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    align-items: stretch;
    gap: var(--bb-space-5);
    padding-top: var(--bb-space-5);
  }

  .st-board {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .st-board__faces {
    position: relative;
    z-index: 2;
    display: flex;
    align-items: flex-end;
    height: 84px;
    margin-bottom: -40px;
    padding-left: var(--bb-space-6);
  }

  .st-board__faces > span {
    width: var(--s);
    height: var(--s);
  }

  .st-board__faces > span + span { margin-left: -12px; }

  .st-board :global(.st-board__card) {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-4);
    padding: calc(var(--bb-space-7) + 6px) var(--bb-space-6) var(--bb-space-5);
  }

  .st-board__head {
    display: flex;
    justify-content: space-between;
    align-items: flex-end;
    gap: var(--bb-space-4);
    min-height: 64px;
  }

  .st-board__intro {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-1);
    width: min(100%, 16rem);
    min-width: 0;
  }

  .st-board__eyebrow {
    display: flex;
    font-size: var(--bb-text-xs);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--bb-green-glow);
  }

  .st-board__eyebrow--tan { color: var(--bb-tan-light); }

  .st-board__title {
    display: flex;
    margin: 0;
    font-family: var(--bb-font-display);
    font-size: clamp(1.5rem, 2.4vw, 2rem);
    font-weight: 500;
    letter-spacing: -0.03em;
    color: var(--bb-paper);
  }

  .st-feed-total {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
  }

  .st-feed-total__num {
    font-family: var(--bb-font-display);
    font-size: clamp(1.8rem, 3vw, 2.4rem);
    font-weight: 500;
    font-variant-numeric: tabular-nums;
    letter-spacing: -0.04em;
    line-height: 1;
    color: var(--bb-tan-light);
  }

  .st-feed-total__sub {
    display: flex;
    justify-content: flex-end;
    width: 7rem;
    font-size: var(--bb-text-xs);
    color: var(--bb-muted);
  }

  .st-table {
    display: flex;
    flex-direction: column;
    margin-inline: calc(var(--bb-space-3) * -1);
  }

  .st-row {
    display: grid;
    grid-template-columns: 2.25rem minmax(0, 1fr) 9rem;
    align-items: center;
    gap: var(--bb-space-3);
    min-height: 56px;
    padding: 0 var(--bb-space-3);
    border-radius: var(--bb-radius-sm);
    transition: background var(--bb-dur-fast) ease, box-shadow var(--bb-dur-fast) ease;
  }

  .st-row + .st-row:not(:hover) { box-shadow: inset 0 1px 0 rgba(var(--bb-white-rgb), 0.05); }
  .st-row:not(.st-row--head):hover { background: var(--bb-elev-2); box-shadow: var(--bb-depth-1); }
  .st-row:not(.st-row--head):hover + .st-row { box-shadow: none; }

  .st-row--head {
    min-height: 30px;
    font-size: var(--bb-text-xs);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }

  .st-cell {
    display: flex;
    min-width: 0;
  }

  .st-cell--rank {
    font-family: var(--bb-font-display);
    font-size: 1.15rem;
    font-weight: 500;
    color: var(--bb-muted-light);
  }

  .st-cell--num { justify-content: flex-end; }

  .st-row--head .st-cell--rank {
    font: inherit;
    color: inherit;
  }

  .st-cell--name {
    align-items: center;
    gap: var(--bb-space-3);
  }

  .st-face {
    flex: none;
    width: 36px;
    height: 36px;
  }

  .st-name {
    min-width: 0;
    overflow: hidden;
    font-weight: 600;
    color: var(--bb-paper);
    text-decoration: none;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .st-name:hover { color: var(--bb-tan-light); }

  .st-name:focus-visible {
    outline: 2px solid var(--bb-focus);
    outline-offset: 3px;
  }

  .st-name--muted { color: var(--bb-muted); }

  .st-cell--main {
    flex-direction: column;
    align-items: flex-end;
    gap: 6px;
  }

  .st-value {
    font-family: var(--bb-font-display);
    font-size: 1.15rem;
    font-weight: 500;
    font-variant-numeric: tabular-nums;
    color: var(--bb-paper);
  }

  .st-value--tan { color: var(--bb-tan-light); }

  .st-meter {
    width: 100%;
    height: 4px;
    overflow: hidden;
    border-radius: var(--bb-radius-pill);
    background: rgba(var(--bb-white-rgb), 0.06);
  }

  .st-meter__fill {
    display: block;
    width: calc(var(--share) * 100%);
    height: 100%;
    margin-left: auto;
    background: var(--bb-green-glow);
    opacity: 0.75;
  }

  .st-meter--tan .st-meter__fill { background: var(--bb-tan-light); }

  .st-empty {
    width: min(100%, 22rem);
    height: calc(3 * 1lh);
    margin: var(--bb-space-6) auto;
    font-size: var(--bb-text-sm);
    text-align: center;
    color: var(--bb-muted);
  }

  .st :global(.st-donate) {
    margin-top: calc(var(--bb-space-section) * 0.6);
  }

  .st-feed-note {
    display: flex;
    margin: 0;
    font-size: var(--bb-text-xs);
    color: var(--bb-muted);
  }

  @keyframes st-bob {
    from { translate: 0 0; }
    to { translate: 0 -7px; }
  }

  @keyframes st-ping {
    from { opacity: 0.8; transform: scale(0.6); }
    to { opacity: 0; transform: scale(1.8); }
  }

  @media (prefers-reduced-motion: reduce) {
    .st-mate,
    .st-beacon::after { animation: none; }
  }

  @media (max-width: 960px) {
    .st-head,
    .st-boards { grid-template-columns: minmax(0, 1fr); gap: var(--bb-space-8); }
    .st-crew { height: 170px; }
  }

  @media (max-width: 760px) {
    .st { padding-top: calc(var(--bb-nav-height) + var(--bb-space-6)); }
    .st-counters { grid-template-columns: minmax(0, 1fr); gap: var(--bb-space-8); }
    .st-counter :global(.st-counter__card) { padding-inline: var(--bb-space-5); }
    .st-board :global(.st-board__card) { padding-inline: var(--bb-space-4); }
    .st-mate { scale: 0.7; transform-origin: bottom center; }
    .st-tagline { height: calc(3 * 1lh); }
  }
</style>
