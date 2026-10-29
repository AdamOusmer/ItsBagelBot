<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import AuroraBg from '@bagel/ui/svelte/AuroraBg.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import LightField from '@bagel/ui/svelte/LightField.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import Pager from '@bagel/ui/svelte/Pager.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import CounterCard from '@bagel/ui/svelte/CounterCard.svelte';
  import RankingCard from '@bagel/ui/svelte/RankingCard.svelte';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import { untrack } from 'svelte';
  import { goto } from '$app/navigation';
  import { page as route } from '$app/state';
  import { formatPointValue, getI18n } from '@bagel/kit';
  import PublicHead from '$lib/components/public/PublicHead.svelte';
  import StatsPageLayout from '$lib/components/stats/StatsPageLayout.svelte';
  import { boardHref, cleanQuery, emptyCopyKeys, MAX_QUERY_LENGTH } from '$lib/leaderboard-board';
  import type { PageData } from './$types';
  import { commandsHref } from '@bagel/kit/site-links';

  let { data }: { data: PageData } = $props();

  const { t, locale } = getI18n();

  type Viewer = PageData['podium'][number];

  const friendPalettes = [
    { head: '#62a593', eye: '#111113' },
    { head: '#eadbbd', eye: '#111113' },
    { head: '#f6efe1', eye: '#111113' }
  ];
  const podiumLooks = [
    { tone: 'warm', appearance: 'solid', palette: 0 },
    { tone: 'silver', appearance: 'solid', palette: 1 },
    { tone: 'accent', appearance: 'soft', palette: 2 }
  ] as const;

  const rowName = (v: Viewer) => v.viewerName || v.viewerLogin || t('leaderboard.anonymousViewer');
  const artName = (v: Viewer) => v.viewerLogin || v.viewerId;

  const totalFmt = new Intl.NumberFormat(locale, { maximumFractionDigits: 0 });
  const hoursFmt = (seconds: number): string => {
    const hours = seconds / 3600;
    return hours < 10
      ? new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(hours)
      : totalFmt.format(Math.round(hours));
  };

  const podium = $derived(data.podium);
  const topPoints = $derived(Number(podium[0]?.points ?? 0));

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
    return goto(boardHref({ query, page: 1 }, route.url.pathname), { replaceState: true, keepFocus: true, noScroll: true });
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

  const podiumCards = $derived(
    podium.map((viewer, i) => ({
      viewer,
      look: podiumLooks[i],
      rank: String(i + 1),
      value: formatPointValue(viewer.points, locale)
    }))
  );

  const rankingItems = $derived(
    data.rows.map((viewer) => ({
      id: viewer.viewerId,
      label: rowName(viewer),
      value: Number(viewer.points),
      valueLabel: formatPointValue(viewer.points, locale),
      secondary: `${hoursFmt(viewer.watchSeconds)} ${t('leaderboard.watchUnit')}`,
      position: String(viewer.rank ?? 0),
      art: artName(viewer)
    }))
  );
  const artByItem = $derived(new Map(rankingItems.map((item) => [item.id, item.art])));

  const crowdNames = $derived(
    Array.from({ length: 12 }, (_, i) => {
      const viewer = [...podium, ...data.rows][i];
      return viewer ? artName(viewer) : `${data.login}-${i}`;
    })
  );

  const emptyCopy = $derived.by(() => {
    const keys = emptyCopyKeys({ query: data.query, degraded: data.degraded, busy: data.busy, podiumSize: podium.length });
    if (!keys) return null;
    return { title: keys.title && t(`leaderboard.${keys.title}`), body: t(`leaderboard.${keys.body}`, { channel: data.channelName }) };
  });

  const channelHref = $derived(commandsHref(data.login));
  const titleSize = $derived(data.channelName.length > 16 ? 'long' : data.channelName.length > 9 ? 'mid' : '');
</script>

<PublicHead
  title={t('leaderboard.title', { channel: data.channelName })}
  description={t('leaderboard.metaDescription', { channel: data.channelName })}
  url="https://leaderboard.itsbagelbot.com/{data.login}"
/>

<AuroraBg />
<div class="starfield" aria-hidden="true"><LightField /></div>

<main class="lb-page">
  <StatsPageLayout arrangement="podium">
    {#snippet heading()}
      <div class="lb-heading">
        <div class="lb-eyebrow" data-fit-box>
          <Tag tone="live" mark="solid" sweep contain><span data-fit="">{t('leaderboard.eyebrow')}</span></Tag>
        </div>
        <Heading level={1} class="stats-layout__title{titleSize ? ` lb-title--${titleSize}` : ''}">{data.channelName}</Heading>
        <div class="lb-tagline" data-fit-box>
          <Text size="sm" tone="muted"><span data-fit="block">{t('leaderboard.tagline')}</span></Text>
        </div>
        <div class="lb-visit" data-fit-box aria-hidden={!data.commandsPage}>
          {#if data.commandsPage}
            <TextLink variant="arrow" tone="go" class="visit" href={channelHref} label={t('leaderboard.visitChannel')}><span data-fit="">{t('leaderboard.visitChannel')}</span></TextLink>
          {/if}
        </div>
      </div>
    {/snippet}

    {#snippet crowd()}
      {#each crowdNames as name, i (`${i}:${name}`)}
        <span aria-hidden="true"><Bolota {name} size={100} palette={friendPalettes[i % friendPalettes.length]} motionState="idle" active gate /></span>
      {/each}
    {/snippet}

    {#snippet notice()}
      {#if data.degraded}
        <AlertBanner tone="warning">{t('leaderboard.degraded')}</AlertBanner>
      {/if}
    {/snippet}

    {#snippet counters()}
      {#each podiumCards as card (card.viewer.viewerId)}
        <CounterCard
          spot
          label={rowName(card.viewer)}
          value={card.value}
          detail={data.currencyName}
          rate={hoursFmt(card.viewer.watchSeconds)}
          rateUnit={t('leaderboard.watchUnit')}
          rateLabel={t('leaderboard.colWatched')}
          period={card.rank}
          tone={card.look.tone}
          appearance={card.look.appearance}
          style="--counter-card-value-size: {card.value.length > 7 ? 'clamp(26px, 13cqi, 48px)' : 'clamp(32px, 20cqi, 64px)'}"
        >
          {#snippet artwork()}
            <span aria-hidden="true"><Bolota name={artName(card.viewer)} size={90} palette={friendPalettes[card.look.palette]} active gate /></span>
          {/snippet}
        </CounterCard>
      {/each}
    {/snippet}

    {#snippet ranking()}
      <RankingCard
        board
        title={t('leaderboard.boardTitle')}
        description={t('leaderboard.boardNote')}
        items={rankingItems}
        scale={topPoints}
      >
        {#snippet actions()}
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
        {/snippet}
        {#snippet leading(item)}
          <span aria-hidden="true"><Bolota name={artByItem.get(item.id) ?? item.id} size={48} /></span>
        {/snippet}
        {#snippet empty()}
          {#if emptyCopy}
            <div class="lb-empty">
              <div class="lb-empty-title" data-fit-box>
                <Text as="div" size="sm"><span data-fit="">{emptyCopy.title}</span></Text>
              </div>
              <div class="lb-empty-body" data-fit-box>
                <Text as="div" size="xs" tone="muted"><span data-fit="block">{emptyCopy.body}</span></Text>
              </div>
            </div>
          {/if}
        {/snippet}
        {#snippet footer()}
          <div class="lb-results" role="status" aria-live="polite" data-fit-box>
            <Text as="span" size="xs" tone="muted" mono><span data-fit="">{resultsLine}</span></Text>
          </div>
          <Pager
            fixed
            data-sveltekit-noscroll=""
            label={pageLabel}
            fit
            prevHref={boardHref({ query: data.query, page: data.page - 1 }, route.url.pathname)}
            nextHref={boardHref({ query: data.query, page: data.page + 1 }, route.url.pathname)}
            hasPrev={data.page > 1}
            hasNext={data.page < data.pages}
          />
        {/snippet}
      </RankingCard>
    {/snippet}

    {#snippet footer()}
      <div class="lb-foot">
        <Tag tone="live" mark="solid" sweep class="bb-tag--wrap earn"><span class="earn-note" data-fit-box><span data-fit="block">{t('leaderboard.earnNote')}</span></span></Tag>
      </div>
    {/snippet}
  </StatsPageLayout>
</main>

<style>
  .starfield {
    position: fixed;
    inset: 0;
    z-index: 0;
    pointer-events: none;
  }

  .lb-page {
    position: relative;
    z-index: 1;
    min-width: 0;
  }

  .lb-heading { display: flex; flex-direction: column; align-items: center; gap: 14px; width: min(100%, 60rem); }
  .lb-eyebrow { display: flex; justify-content: center; width: 11rem; min-width: 0; }
  .lb-heading :global(.lb-title--mid) { font-size: clamp(40px, 5.4vw, 68px); }
  .lb-heading :global(.lb-title--long) { font-size: clamp(32px, 3.8vw, 48px); }
  @media (max-width: 650px) {
    .lb-heading :global(.stats-layout__title) { font-size: clamp(30px, 10vw, 52px); }
  }

  .lb-tagline { width: min(100%, 36rem); height: calc(2 * 1lh); font-size: var(--bb-text-sm); }
  .lb-visit { display: flex; align-items: center; justify-content: center; width: min(100%, 20rem); min-height: 1.75rem; }
  .lb-visit :global(.visit) { max-width: 100%; }
  @media (max-width: 650px) {
    .lb-tagline { height: calc(4 * 1lh); }
  }

  .lb-search { width: 260px; max-width: 100%; }
  @media (max-width: 650px) {
    .lb-search { width: 100%; }
  }

  .lb-empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--bb-space-2);
    margin: var(--bb-space-6) auto 0;
    width: min(100%, 360px);
    text-align: center;
  }
  .lb-empty-title { width: 100%; height: 1lh; }
  .lb-empty-body { width: 100%; height: calc(3 * 1lh); }

  .lb-results { height: 1lh; margin-bottom: var(--bb-space-2); text-align: center; font-size: var(--bb-text-xs); }
  .lb-results > :global(*) { display: block; }

  .lb-foot { display: flex; align-items: center; justify-content: center; }
  .lb-foot { width: min(100%, 44rem); margin-inline: auto; }
  .lb-foot :global(.earn) { max-width: 100%; }
  .earn-note { display: block; height: 1lh; text-align: center; }
  @media (max-width: 720px) {
    .earn-note { height: calc(2 * 1lh); }
  }
</style>
