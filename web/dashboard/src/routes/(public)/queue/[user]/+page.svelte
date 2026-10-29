<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import AuroraBg from '@bagel/ui/svelte/AuroraBg.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import LightField from '@bagel/ui/svelte/LightField.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import CounterCard from '@bagel/ui/svelte/CounterCard.svelte';
  import RankingCard from '@bagel/ui/svelte/RankingCard.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import { livePoll } from '@bagel/ui/lib/live-poll';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import { onMount, untrack } from 'svelte';
  import { page as route } from '$app/state';
  import { getI18n } from '@bagel/kit';
  import PublicHead from '#lib/components/public/PublicHead.svelte';
  import StatsPageLayout from '#lib/components/stats/StatsPageLayout.svelte';
  import { MAX_QUEUE_ENTRIES, parseSnapshot, waitedSeconds } from '#lib/queue-snapshot';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();

  const { t, locale } = getI18n();

  const POLL_MS = 3000;
  const CLOCK_MS = 5000;

  let snapshot = $state(untrack(() => data.snapshot));
  let trouble = $state(untrack(() => data.snapshot.degraded));
  let nowMs = $state(untrack(() => data.snapshot.at));
  let clockOffset = 0;

  const countFmt = new Intl.NumberFormat(locale, { maximumFractionDigits: 0 });
  const unitFmt = (unit: 'hour' | 'minute') => new Intl.NumberFormat(locale, { style: 'unit', unit, unitDisplay: 'narrow' });
  const hoursFmt = unitFmt('hour');
  const minutesFmt = unitFmt('minute');

  function waitLabel(seconds: number): string {
    const minutes = Math.floor(seconds / 60);
    if (minutes < 1) return t('queuePage.justJoined');
    const hours = Math.floor(minutes / 60);
    const rest = minutesFmt.format(minutes % 60);
    return hours > 0 ? `${hoursFmt.format(hours)} ${rest}` : minutesFmt.format(minutes);
  }

  const waits = $derived(snapshot.entries.map((entry) => waitedSeconds(entry, nowMs)));
  const longestWait = $derived(Math.max(1, ...waits));
  const upNext = $derived(snapshot.entries[0]);
  const shown = $derived(snapshot.entries.length);

  const rankingItems = $derived(
    snapshot.entries.map((entry, i) => ({
      id: entry.login,
      label: entry.login,
      value: waits[i],
      valueLabel: waitLabel(waits[i]),
      secondary: i === 0 ? t('queuePage.upNext') : t('queuePage.ahead', { count: countFmt.format(i) }),
      position: String(i + 1)
    }))
  );

  let search = $state('');
  const needle = $derived(search.trim().toLowerCase());
  const visibleItems = $derived(
    needle ? rankingItems.filter((item) => item.label.toLowerCase().includes(needle)) : rankingItems
  );
  const noMatch = $derived(needle !== '' && rankingItems.length > 0 && visibleItems.length === 0);

  const statusLine = $derived.by(() => {
    if (trouble) return t('queuePage.degraded');
    if (snapshot.total > shown) return t('queuePage.moreNote', { shown: countFmt.format(shown), total: countFmt.format(snapshot.total) });
    return '';
  });

  const emptyCopy = $derived.by(() => {
    if (trouble) return { title: t('queuePage.degradedTitle'), body: t('queuePage.degraded') };
    if (noMatch) return { title: t('queuePage.noMatchTitle'), body: t('queuePage.noMatchBody') };
    return { title: t('queuePage.emptyTitle'), body: t('queuePage.emptyBody') };
  });

  const joinsOpen = $derived(snapshot.open && !snapshot.degraded);
  const statusText = $derived(
    t(snapshot.degraded ? 'queuePage.statusUnknown' : snapshot.open ? 'queuePage.statusOpen' : 'queuePage.statusClosed')
  );
  const upNextName = $derived(upNext?.login ?? t('queuePage.nobodyYet'));
  const upNextDetail = $derived(upNext ? waitLabel(waits[0]) : t('queuePage.emptyTitle'));
  const titleSize = $derived(data.channelName.length > 16 ? 'long' : data.channelName.length > 9 ? 'mid' : '');
  const crowdNames = $derived(Array.from({ length: 12 }, (_, i) => `${data.login}-${i}`));
  const friendPalettes = [
    { head: '#62a593', eye: '#111113' },
    { head: '#eadbbd', eye: '#111113' },
    { head: '#f6efe1', eye: '#111113' }
  ];

  function apply(raw: unknown): void {
    const next = parseSnapshot(raw);
    trouble = !next || next.degraded;
    if (!next || next.degraded) return;
    clockOffset = Math.max(clockOffset, next.at - Date.now());
    snapshot = next;
    nowMs = Date.now() + clockOffset;
  }

  async function poll(): Promise<boolean> {
    if (document.hidden) return false;
    try {
      const res = await fetch(`${route.url.pathname.replace(/\/$/, '')}/data`, { headers: { accept: 'application/json' } });
      apply(res.ok ? await res.json() : null);
    } catch {
      trouble = true;
    }
    return false;
  }

  onMount(() => {
    clockOffset = snapshot.at - Date.now();
    const clock = setInterval(() => (nowMs = Date.now() + clockOffset), CLOCK_MS);
    const stopPoll = livePoll(poll, {
      firstDelayMs: POLL_MS,
      delayMs: () => POLL_MS,
      timeoutMs: Infinity,
      refreshOnVisible: true
    });
    return () => {
      clearInterval(clock);
      stopPoll();
    };
  });
</script>

<PublicHead
  title={t('queuePage.title', { channel: data.channelName })}
  description={t('queuePage.metaDescription', { channel: data.channelName })}
  url="https://queue.itsbagelbot.com/{data.login}"
/>

<AuroraBg />
<div class="starfield" aria-hidden="true"><LightField /></div>

<main class="q-page">
  <StatsPageLayout arrangement="playful">
    {#snippet heading()}
      <div class="q-heading">
        <div class="q-tags">
          <div class="q-tag" data-fit-box>
            <Tag tone="live" mark="solid" sweep contain><span data-fit="">{t('queuePage.eyebrow')}</span></Tag>
          </div>
          <div class="q-tag" data-fit-box role="status">
            <Tag tone={joinsOpen ? 'success' : 'inactive'} mark="solid" contain><span data-fit="">{statusText}</span></Tag>
          </div>
        </div>
        <Heading level={1} class="stats-layout__title{titleSize ? ` q-title--${titleSize}` : ''}">{data.channelName}</Heading>
        <div class="q-note" data-fit-box>
          <Text size="sm" tone="muted"><span data-fit="block">{t('queuePage.tagline')}</span></Text>
        </div>
        <div class="q-note" data-fit-box>
          <Text size="sm"><span data-fit="block">{t('queuePage.joinHint')}</span></Text>
        </div>
      </div>
    {/snippet}

    {#snippet crowd()}
      {#each crowdNames as name, i (`${i}:${name}`)}
        <span aria-hidden="true"><Bolota {name} size={100} palette={friendPalettes[i % friendPalettes.length]} motionState="idle" active gate /></span>
      {/each}
    {/snippet}

    {#snippet counters()}
      <CounterCard
        label={t('queuePage.inLine')}
        value={countFmt.format(snapshot.total)}
        detail={statusText}
        period={t('queuePage.live')}
        rate={null}
        tone="accent"
        appearance="solid"
      />
      <CounterCard
        spot
        label={t('queuePage.upNext')}
        value={upNextName}
        detail={upNextDetail}
        period={t('queuePage.live')}
        rate={null}
        tone="warm"
        appearance="solid"
        style="--counter-card-value-size: {upNextName.length > 7 ? 'clamp(26px, 13cqi, 48px)' : 'clamp(32px, 20cqi, 64px)'}"
      />
    {/snippet}

    {#snippet ranking()}
      <div class="q-list">
        <RankingCard
          board
          title={t('queuePage.boardTitle')}
          description={t('queuePage.boardNote', { max: countFmt.format(MAX_QUEUE_ENTRIES) })}
          items={visibleItems}
          scale={longestWait}
        >
          {#snippet actions()}
            <form class="q-search" role="search" onsubmit={(event) => event.preventDefault()}>
              <SearchInput
                fill
                tall
                name="q"
                bind:value={search}
                autocomplete="off"
                enterkeyhint="search"
                placeholder={t('queuePage.searchPlaceholder')}
                aria-label={t('queuePage.searchLabel')}
              />
            </form>
          {/snippet}
          {#snippet leading(item)}
            <span aria-hidden="true"><Bolota name={item.id} size={48} /></span>
          {/snippet}
          {#snippet empty()}
            <div class="q-empty">
              <div class="q-empty-title" data-fit-box>
                <Text as="div" size="sm"><span data-fit="">{emptyCopy.title}</span></Text>
              </div>
              <div class="q-empty-body" data-fit-box>
                <Text as="div" size="xs" tone="muted"><span data-fit="block">{emptyCopy.body}</span></Text>
              </div>
            </div>
          {/snippet}
          {#snippet footer()}
            <div class="q-status" role="status" aria-live="polite" data-fit-box>
              <Text as="span" size="xs" tone="muted" mono><span data-fit="">{statusLine}</span></Text>
            </div>
          {/snippet}
        </RankingCard>
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

  .q-page {
    position: relative;
    z-index: 1;
    min-width: 0;
  }

  .q-heading { display: flex; flex-direction: column; align-items: center; gap: 14px; width: min(100%, 60rem); }
  .q-heading :global(.q-title--mid) { font-size: clamp(40px, 5.4vw, 68px); }
  .q-heading :global(.q-title--long) { font-size: clamp(32px, 3.8vw, 48px); }
  @media (max-width: 650px) {
    .q-heading :global(.stats-layout__title) { font-size: clamp(30px, 10vw, 52px); }
  }

  .q-tags { display: flex; flex-wrap: wrap; justify-content: center; gap: var(--bb-space-2); }
  .q-tag { display: flex; justify-content: center; width: 11rem; min-width: 0; }

  .q-note { width: min(100%, 36rem); height: calc(2 * 1lh); font-size: var(--bb-text-sm); }
  @media (max-width: 650px) {
    .q-note { height: calc(4 * 1lh); }
  }

  .q-page :global(.stats-layout__ranking) { grid-column: 1 / -1; }
  .q-list { display: grid; min-height: 24rem; }

  .q-search { width: 260px; max-width: 100%; }
  @media (max-width: 650px) {
    .q-search { width: 100%; }
  }

  .q-empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--bb-space-2);
    margin: var(--bb-space-6) auto 0;
    width: min(100%, 360px);
    text-align: center;
  }
  .q-empty-title { width: 100%; height: 1lh; }
  .q-empty-body { width: 100%; height: calc(3 * 1lh); }

  .q-status { height: 1lh; margin-bottom: var(--bb-space-2); text-align: center; font-size: var(--bb-text-xs); }
  .q-status > :global(*) { display: block; }
</style>
