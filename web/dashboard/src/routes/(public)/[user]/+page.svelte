<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import DonateCard from '@bagel/ui/svelte/DonateCard.svelte';
  import LightField from '@bagel/ui/svelte/LightField.svelte';
  import Pager from '@bagel/ui/svelte/Pager.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import { untrack } from 'svelte';
  import { goto } from '$app/navigation';
  import { page as route } from '$app/state';
  import { formatPointValue, getI18n } from '@bagel/kit';
  import { SITE, commandsHref } from '@bagel/kit/site-links';
  import PublicHead from '#lib/components/public/PublicHead.svelte';
  import { boardHref, cleanQuery, emptyCopyKeys, MAX_QUERY_LENGTH } from '#lib/leaderboard-board';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();

  const { t, locale } = getI18n();

  type Viewer = PageData['podium'][number];

  const GREEN = { head: '#62a593', eye: '#111113' };
  const TAN = { head: '#eadbbd', eye: '#111113' };
  const PALE = { head: '#f6efe1', eye: '#111113' };
  const donors = [
    { name: 'oven_mitt', size: 64, palette: PALE },
    { name: 'glazed_val', size: 88, palette: GREEN },
    { name: 'caramel_cass', size: 64, palette: TAN }
  ];
  const PODIUM_ORDER = [1, 0, 2];

  const rowName = (v: Viewer) => v.viewerName || v.viewerLogin || t('leaderboard.anonymousViewer');
  const artName = (v: Viewer) => v.viewerLogin || v.viewerId;

  const totalFmt = new Intl.NumberFormat(locale, { maximumFractionDigits: 0 });
  const hoursFmt = (seconds: number): string => {
    const hours = seconds / 3600;
    return hours < 10
      ? new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(hours)
      : totalFmt.format(Math.round(hours));
  };

  const topPoints = $derived(Number(data.podium[0]?.points ?? 0));
  const podium = $derived(
    PODIUM_ORDER.flatMap((i) => (data.podium[i] ? [{ viewer: data.podium[i], rank: i + 1 }] : []))
  );
  const titleWords = $derived(data.channelName.split(' '));
  const titleSize = $derived(data.channelName.length > 16 ? 'long' : data.channelName.length > 9 ? 'mid' : '');

  let search = $state(untrack(() => data.query));
  let searchInput = $state<HTMLInputElement>();

  $effect(() => {
    const served = data.query;
    untrack(() => {
      if (document.activeElement !== searchInput) search = served;
    });
  });

  function showQuery(value: string): Promise<void> {
    const query = cleanQuery(value);
    if (query === data.query && data.page === 1) return Promise.resolve();
    return goto(boardHref({ query, page: 1 }, route.url.pathname), { replaceState: true, reset: false });
  }

  function submitSearch(event: SubmitEvent): void {
    event.preventDefault();
    void showQuery(search);
  }

  const resultsLine = $derived(
    data.rows.length === 0
      ? t('leaderboard.resultsNone')
      : t(data.query ? 'leaderboard.resultsMatches' : 'leaderboard.resultsRange', {
          from: totalFmt.format(data.range.from),
          to: totalFmt.format(data.range.to),
          total: totalFmt.format(data.range.total)
        })
  );
  const pageLabel = $derived(
    t('leaderboard.pageOf', { page: totalFmt.format(data.page), pages: totalFmt.format(data.pages) })
  );

  const shareOf = (points: number | string): number => (topPoints > 0 ? Math.max(0.04, Number(points) / topPoints) : 0);

  const emptyCopy = $derived.by(() => {
    const keys = emptyCopyKeys({ query: data.query, degraded: data.degraded, busy: data.busy, podiumSize: data.podium.length });
    if (!keys) return null;
    return { title: keys.title && t(`leaderboard.${keys.title}`), body: t(`leaderboard.${keys.body}`, { channel: data.channelName }) };
  });

  const channelHref = $derived(commandsHref(data.login));
</script>

<PublicHead
  title={t('leaderboard.title', { channel: data.channelName })}
  description={t('leaderboard.metaDescription', { channel: data.channelName })}
  url="https://leaderboard.itsbagelbot.com/{data.login}"
/>

<div class="sky" aria-hidden="true">
  <LightField />
  <div class="bb-orb bb-orb--halo bb-orb--green orb orb--green"><i class="bb-orb__pulse"></i></div>
  <div class="bb-orb bb-orb--halo bb-orb--tan orb orb--tan"><i class="bb-orb__pulse"></i></div>
</div>

<main class="lb">
  <header class="lb-head">
    <span class="lb-eyebrow"><span data-fit="">{t('leaderboard.eyebrow')}</span></span>
    <h1 class="lb-title{titleSize ? ` lb-title--${titleSize}` : ''}">
      {#each titleWords as word, i (i)}{i > 0 ? ' ' : ''}<span class="lb-word">{word}</span>{/each}
    </h1>
    <p class="lb-tagline"><span data-fit="block">{t('leaderboard.tagline')}</span></p>
    <div class="lb-visit" aria-hidden={!data.commandsPage}>
      {#if data.commandsPage}
        <TextLink variant="arrow" tone="go" href={channelHref} label={t('leaderboard.visitChannel')}><span data-fit="">{t('leaderboard.visitChannel')}</span></TextLink>
      {/if}
    </div>
  </header>

  {#if data.degraded}
    <AlertBanner tone="warning">{t('leaderboard.degraded')}</AlertBanner>
  {/if}

  {#if podium.length}
    <section class="lb-podium" aria-label={t('leaderboard.boardTitle')}>
      {#each podium as spot (spot.viewer.viewerId)}
        <div class="lb-spot lb-spot--{spot.rank}">
          <div class="lb-spot__face" aria-hidden="true">
            <Bolota name={artName(spot.viewer)} size={spot.rank === 1 ? 110 : 84} motionState="idle" active gate />
          </div>
          <Card class="lb-spot__card" tone={spot.rank === 1 ? 'accent' : undefined} raised={spot.rank === 1}>
            <div class="lb-spot__top">
              <span class="lb-spot__rank">{spot.rank}</span>
              <span class="lb-spot__name">{rowName(spot.viewer)}</span>
            </div>
            <div class="lb-spot__points"><span data-fit="">{formatPointValue(spot.viewer.points, locale)}</span></div>
            <div class="lb-spot__unit"><span data-fit="">{data.currencyName}</span></div>
            <div class="lb-spot__rule"></div>
            <div class="lb-spot__meta">
              <span class="lb-spot__meta-label"><span data-fit="">{t('leaderboard.colWatched')}</span></span>
              <span class="lb-spot__meta-value">{hoursFmt(spot.viewer.watchSeconds)} {t('leaderboard.watchUnit')}</span>
            </div>
          </Card>
        </div>
      {/each}
    </section>
  {/if}

  <Card class="lb-board">
    <div class="lb-board__head">
      <div class="lb-board__intro">
        <h2 class="lb-board__title"><span data-fit="">{t('leaderboard.boardTitle')}</span></h2>
        <p class="lb-board__note"><span data-fit="">{t('leaderboard.boardNote')}</span></p>
      </div>
      <form class="lb-search" method="GET" role="search" onsubmit={submitSearch}>
        <SearchInput
          fill
          tall
          name="q"
          bind:value={search}
          bind:element={searchInput}
          debounceMs={250}
          onValueChange={showQuery}
          maxlength={MAX_QUERY_LENGTH}
          autocomplete="off"
          enterkeyhint="search"
          placeholder={t('leaderboard.searchPlaceholder')}
          aria-label={t('leaderboard.searchLabel')}
        />
      </form>
    </div>

    {#if data.rows.length}
      <div class="lb-table" role="table" aria-label={t('leaderboard.boardTitle')}>
        <div class="lb-row lb-row--head" role="row">
          <span class="lb-cell lb-cell--rank" role="columnheader">#</span>
          <span class="lb-cell lb-cell--viewer" role="columnheader"><span data-fit="">{t('leaderboard.colViewer')}</span></span>
          <span class="lb-cell lb-cell--hours" role="columnheader"><span data-fit="">{t('leaderboard.colWatched')}</span></span>
          <span class="lb-cell lb-cell--points" role="columnheader"><span data-fit="">{data.currencyName}</span></span>
        </div>
        {#each data.rows as viewer, i (viewer.viewerId)}
          <div class="lb-row" role="row" style="--share: {shareOf(viewer.points)}">
            <span class="lb-cell lb-cell--rank" role="cell">{viewer.rank ?? ''}</span>
            <span class="lb-cell lb-cell--viewer" role="cell">
              <span class="lb-face" aria-hidden="true"><Bolota name={artName(viewer)} size={40} /></span>
              <span class="lb-name">{rowName(viewer)}</span>
            </span>
            <span class="lb-cell lb-cell--hours" role="cell">{hoursFmt(viewer.watchSeconds)} <small class="lb-unit">{t('leaderboard.watchUnit')}</small></span>
            <span class="lb-cell lb-cell--points" role="cell">
              <span class="lb-points">{formatPointValue(viewer.points, locale)}</span>
              <span class="lb-meter" aria-hidden="true"><span class="lb-meter__fill"></span></span>
            </span>
          </div>
        {/each}
      </div>
    {:else if emptyCopy}
      <div class="lb-empty">
        {#if emptyCopy.title}<p class="lb-empty__title"><span data-fit="">{emptyCopy.title}</span></p>{/if}
        <p class="lb-empty__body"><span data-fit="block">{emptyCopy.body}</span></p>
      </div>
    {/if}

    <div class="lb-board__foot">
      <p class="lb-results" role="status" aria-live="polite"><span data-fit="">{resultsLine}</span></p>
      <Pager
        fixed
        data-sveltekit-reset="false"
        label={pageLabel}
        fit
        prevHref={boardHref({ query: data.query, page: data.page - 1 }, route.url.pathname)}
        nextHref={boardHref({ query: data.query, page: data.page + 1 }, route.url.pathname)}
        hasPrev={data.page > 1}
        hasNext={data.page < data.pages}
      />
    </div>
  </Card>

  <p class="lb-earn"><span data-fit="block">{t('leaderboard.earnNote')}</span></p>

  <DonateCard
    class="lb-donate"
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

  .lb {
    position: relative;
    z-index: 1;
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-8);
    width: min(100% - 32px, 1080px);
    margin-inline: auto;
    padding: calc(var(--bb-nav-height) + var(--bb-space-8)) 0 var(--bb-space-section);
  }

  .lb-head {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--bb-space-3);
  }

  .lb-eyebrow {
    display: flex;
    width: min(100%, 16rem);
    font-size: var(--bb-text-md);
    font-weight: 700;
    letter-spacing: 0.02em;
    color: var(--bb-green-glow);
    text-shadow: 0 0 18px rgba(var(--bb-green-glow-rgb), 0.45);
  }

  .lb-title {
    margin: 0;
    font-family: var(--bb-font-display);
    font-size: clamp(2.5rem, 6vw, 4.5rem);
    font-weight: 500;
    letter-spacing: var(--bb-tracking-display);
    line-height: 1.04;
  }

  .lb-title--mid { font-size: clamp(2.25rem, 4.8vw, 3.5rem); }
  .lb-title--long { font-size: clamp(1.9rem, 3.6vw, 2.75rem); }

  .lb-word {
    display: inline-block;
    background: linear-gradient(180deg, var(--bb-white) 40%, rgba(var(--bb-tan-light-rgb), 0.55));
    background-clip: text;
    -webkit-background-clip: text;
    color: transparent;
    filter: drop-shadow(0 0 36px rgba(var(--bb-tan-rgb), 0.18));
  }

  .lb-tagline {
    width: min(100%, 46ch);
    height: calc(2 * 1lh);
    margin: 0;
    font-size: var(--bb-text-md);
    line-height: 1.6;
    color: var(--bb-muted-light);
  }

  .lb-visit {
    display: flex;
    align-items: center;
    width: min(100%, 20rem);
    min-height: 1.75rem;
  }

  .lb-podium {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    align-items: end;
    gap: var(--bb-space-5);
    padding-top: var(--bb-space-6);
  }

  .lb-spot {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .lb-spot--1 { transform: translateY(-28px); }

  .lb-spot__face {
    position: relative;
    z-index: 2;
    display: flex;
    justify-content: center;
    height: 84px;
    margin-bottom: -38px;
  }

  .lb-spot--1 .lb-spot__face {
    height: 110px;
    margin-bottom: -48px;
  }

  .lb-spot :global(.lb-spot__card) {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-2);
    padding: calc(var(--bb-space-7) + 6px) var(--bb-space-6) var(--bb-space-6);
  }

  .lb-spot--2 :global(.lb-spot__card) { --card-halo-rgb: var(--bb-tier-vip-rgb); --card-halo: 0.5; }
  .lb-spot--3 :global(.lb-spot__card) { --card-halo-rgb: var(--bb-halo-green-rgb); --card-halo: 0.45; }

  .lb-spot__top {
    display: flex;
    align-items: baseline;
    gap: var(--bb-space-3);
    min-width: 0;
  }

  .lb-spot__rank {
    flex: none;
    font-family: var(--bb-font-mono);
    font-size: var(--bb-text-sm);
    color: var(--bb-muted);
  }

  .lb-spot--1 .lb-spot__rank { color: var(--bb-tier-paid); }
  .lb-spot--2 .lb-spot__rank { color: var(--bb-tier-vip); }
  .lb-spot--3 .lb-spot__rank { color: var(--bb-green-glow); }

  .lb-spot__name {
    min-width: 0;
    overflow: hidden;
    font-weight: 600;
    color: var(--bb-paper);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .lb-spot__points {
    display: flex;
    margin-top: var(--bb-space-3);
    font-family: var(--bb-font-display);
    font-size: clamp(2.25rem, 3.6vw, 3rem);
    font-weight: 500;
    letter-spacing: -0.05em;
    line-height: 1;
    color: var(--bb-paper);
  }

  .lb-spot--1 .lb-spot__points { font-size: clamp(2.6rem, 4.4vw, 3.6rem); }

  .lb-spot__unit {
    display: flex;
    font-size: var(--bb-text-sm);
    color: var(--bb-muted);
  }

  .lb-spot__rule {
    height: 1px;
    margin: var(--bb-space-3) 0 var(--bb-space-1);
    background: var(--bb-border);
  }

  .lb-spot__meta {
    display: flex;
    justify-content: space-between;
    gap: var(--bb-space-3);
    font-size: var(--bb-text-sm);
    color: var(--bb-muted);
  }

  .lb-spot__meta-label {
    display: flex;
    flex: 1 1 0;
    min-width: 0;
  }

  .lb-spot__meta-value {
    flex: none;
    color: var(--bb-paper);
  }

  .lb :global(.lb-board) {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-5);
    padding: var(--bb-space-6) var(--bb-space-6) var(--bb-space-5);
  }

  .lb-board__head {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    justify-content: space-between;
    gap: var(--bb-space-5);
  }

  .lb-board__intro {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-1);
    width: min(100%, 26rem);
  }

  .lb-board__title {
    display: flex;
    margin: 0;
    font-family: var(--bb-font-display);
    font-size: clamp(1.6rem, 2.6vw, 2.1rem);
    font-weight: 500;
    letter-spacing: -0.03em;
    color: var(--bb-paper);
  }

  .lb-board__note {
    display: flex;
    margin: 0;
    font-size: var(--bb-text-sm);
    color: var(--bb-muted);
  }

  .lb-search {
    width: 300px;
    max-width: 100%;
  }

  .lb-table {
    display: flex;
    flex-direction: column;
    margin-inline: calc(var(--bb-space-3) * -1);
  }

  .lb-row {
    display: grid;
    grid-template-columns: 3.5rem minmax(0, 1fr) 7rem 11rem;
    align-items: center;
    gap: var(--bb-space-4);
    min-height: 64px;
    padding: 0 var(--bb-space-3);
    border-radius: var(--bb-radius-sm);
    transition: background var(--bb-dur-fast) ease, box-shadow var(--bb-dur-fast) ease;
  }

  .lb-row + .lb-row:not(:hover) { box-shadow: inset 0 1px 0 rgba(var(--bb-white-rgb), 0.05); }
  .lb-row:not(.lb-row--head):hover { background: var(--bb-elev-2); box-shadow: var(--bb-depth-1); }
  .lb-row:not(.lb-row--head):hover + .lb-row { box-shadow: none; }

  .lb-row--head {
    min-height: 36px;
    font-size: var(--bb-text-xs);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }

  .lb-cell--rank {
    font-family: var(--bb-font-display);
    font-size: 1.25rem;
    font-weight: 500;
    font-variant-numeric: tabular-nums;
    color: var(--bb-muted-light);
  }

  .lb-cell--viewer {
    display: flex;
    align-items: center;
    gap: var(--bb-space-3);
    min-width: 0;
  }

  .lb-face {
    flex: none;
    width: 40px;
    height: 40px;
  }

  .lb-name {
    min-width: 0;
    overflow: hidden;
    font-size: 1.05rem;
    font-weight: 600;
    color: var(--bb-paper);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .lb-cell--hours {
    display: flex;
    justify-content: flex-end;
    align-items: baseline;
    gap: 4px;
    font-size: 1rem;
    font-variant-numeric: tabular-nums;
    color: var(--bb-muted-light);
  }

  .lb-unit {
    font-size: var(--bb-text-xs);
    color: var(--bb-muted);
  }

  .lb-cell--points {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 6px;
  }

  .lb-row--head .lb-cell--rank,
  .lb-row--head .lb-cell--hours,
  .lb-row--head .lb-cell--points {
    font: inherit;
    color: inherit;
  }

  .lb-points {
    font-family: var(--bb-font-display);
    font-size: 1.25rem;
    font-weight: 500;
    letter-spacing: -0.02em;
    font-variant-numeric: tabular-nums;
    color: var(--bb-paper);
  }

  .lb-meter {
    width: 100%;
    height: 4px;
    overflow: hidden;
    border-radius: var(--bb-radius-pill);
    background: rgba(var(--bb-white-rgb), 0.06);
  }

  .lb-meter__fill {
    display: block;
    width: calc(var(--share) * 100%);
    height: 100%;
    margin-left: auto;
    border-radius: inherit;
    background: var(--bb-green-glow);
    opacity: 0.75;
  }

  .lb-empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--bb-space-2);
    width: min(100%, 360px);
    margin: var(--bb-space-6) auto;
    text-align: center;
  }

  .lb-empty__title {
    display: flex;
    justify-content: center;
    width: 100%;
    margin: 0;
    color: var(--bb-paper);
  }

  .lb-empty__body {
    width: 100%;
    height: calc(3 * 1lh);
    margin: 0;
    font-size: var(--bb-text-sm);
    color: var(--bb-muted);
  }

  .lb-board__foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--bb-space-4);
  }

  .lb-results {
    display: flex;
    flex: 1 1 0;
    min-width: 0;
    margin: 0;
    font: var(--bb-text-xs)/1.4 var(--bb-font-mono);
    color: var(--bb-muted);
  }

  .lb-earn {
    width: min(100%, 44rem);
    height: 1lh;
    margin: 0 auto;
    font-size: var(--bb-text-sm);
    text-align: center;
    color: var(--bb-muted);
  }

  .lb :global(.lb-donate) {
    margin-top: calc(var(--bb-space-section) * 0.6);
  }

  @media (max-width: 760px) {
    .lb { padding-top: calc(var(--bb-nav-height) + var(--bb-space-6)); }
    .lb-tagline { height: calc(3 * 1lh); }
    .lb-podium { grid-template-columns: minmax(0, 1fr); gap: var(--bb-space-8); }
    .lb-spot--1 { order: -1; transform: none; }
    .lb :global(.lb-board) { padding: var(--bb-space-5) var(--bb-space-4) var(--bb-space-4); }
    .lb-row { grid-template-columns: 2.25rem minmax(0, 1fr) 6.5rem; gap: var(--bb-space-3); padding: 0 var(--bb-space-2); }
    .lb-cell--hours { display: none; }
    .lb-board__foot { flex-direction: column; align-items: stretch; }
    .lb-earn { height: calc(2 * 1lh); }
  }
</style>
