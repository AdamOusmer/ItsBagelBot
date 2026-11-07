<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import LightField from '@bagel/ui/svelte/LightField.svelte';
  import { livePoll } from '@bagel/ui/lib/live-poll';
  import Bolota from '@bagel/ui/svelte/Bolota.svelte';
  import { onMount, untrack } from 'svelte';
  import { page as route } from '$app/state';
  import { getI18n } from '@bagel/kit';
  import PublicHead from '#lib/components/public/PublicHead.svelte';
  import { MAX_SONG_ROWS, parseSongSnapshot } from '#lib/songqueue-snapshot';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();

  const { t, locale } = getI18n();

  const POLL_MS = 3000;

  let snapshot = $state(untrack(() => data.snapshot));
  let trouble = $state(untrack(() => data.snapshot.degraded));

  const countFmt = new Intl.NumberFormat(locale, { maximumFractionDigits: 0 });

  const current = $derived(snapshot.current);
  const rows = $derived(snapshot.up.map((song, i) => ({ ...song, position: i + 1 })));
  const titleWords = $derived(data.channelName.split(' '));
  const titleSize = $derived(data.channelName.length > 16 ? 'long' : data.channelName.length > 9 ? 'mid' : '');

  const statusLine = $derived.by(() => {
    if (trouble) return t('songPage.degraded');
    if (snapshot.total > rows.length) return t('songPage.moreNote', { shown: countFmt.format(rows.length), total: countFmt.format(snapshot.total) });
    return '';
  });

  const emptyCopy = $derived(
    trouble
      ? { title: t('songPage.degradedTitle'), body: t('songPage.degraded') }
      : { title: t('songPage.emptyTitle'), body: t('songPage.emptyBody') }
  );

  function apply(raw: unknown): void {
    const next = parseSongSnapshot(raw);
    trouble = !next || next.degraded;
    if (!next || next.degraded) return;
    snapshot = next;
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

  onMount(() =>
    livePoll(poll, {
      firstDelayMs: POLL_MS,
      delayMs: () => POLL_MS,
      timeoutMs: Infinity,
      refreshOnVisible: true
    })
  );
</script>

<PublicHead
  title={t('songPage.title', { channel: data.channelName })}
  description={t('songPage.metaDescription', { channel: data.channelName })}
  url="https://queue.itsbagelbot.com/sr/{data.login}"
/>

<div class="sky" aria-hidden="true">
  <LightField />
  <div class="bb-orb bb-orb--halo bb-orb--green orb orb--green"><i class="bb-orb__pulse"></i></div>
  <div class="bb-orb bb-orb--halo bb-orb--tan orb orb--tan"><i class="bb-orb__pulse"></i></div>
</div>

<main class="sq">
  <header class="sq-head">
    <span class="sq-eyebrow"><span class="sq-beacon" aria-hidden="true"></span><span class="sq-eyebrow__label"><bb-fit>{t('songPage.eyebrow')}</bb-fit></span></span>
    <h1 class="sq-title{titleSize ? ` sq-title--${titleSize}` : ''}">
      {#each titleWords as word, i (i)}{i > 0 ? ' ' : ''}<span class="sq-word">{word}</span>{/each}
    </h1>
    <p class="sq-tagline"><bb-fit mode="block">{t('songPage.tagline')}</bb-fit></p>
    <p class="sq-hint"><bb-fit mode="block">{t('songPage.requestHint')}</bb-fit></p>
  </header>

  <section class="sq-spots">
    <div class="sq-spot sq-spot--next">
      <div class="sq-spot__face" aria-hidden="true">
        {#if current}<Bolota name={current.requester || data.login} size={110} motionState="idle" active gate />{/if}
      </div>
      <Card class="sq-spot__card" tone="accent" raised>
        <span class="sq-spot__label"><bb-fit>{t('songPage.nowPlaying')}</bb-fit></span>
        <div class="sq-spot__value"><bb-fit>{current?.title ?? t('songPage.nothingPlaying')}</bb-fit></div>
        <div class="sq-spot__rule"></div>
        <div class="sq-spot__meta"><bb-fit>{current?.artists ?? ''}</bb-fit></div>
      </Card>
    </div>
    <div class="sq-spot">
      <Card class="sq-spot__card">
        <span class="sq-spot__label"><bb-fit>{t('songPage.inQueue')}</bb-fit></span>
        <div class="sq-spot__value"><bb-fit>{countFmt.format(snapshot.total)}</bb-fit></div>
        <div class="sq-spot__rule"></div>
        <div class="sq-spot__meta" role="status"><bb-fit>{rows[0]?.title ?? ''}</bb-fit></div>
      </Card>
    </div>
  </section>

  <Card class="sq-board">
    <div class="sq-board__intro">
      <h2 class="sq-board__title"><bb-fit>{t('songPage.boardTitle')}</bb-fit></h2>
      <p class="sq-board__note"><bb-fit>{t('songPage.boardNote', { max: countFmt.format(MAX_SONG_ROWS) })}</bb-fit></p>
    </div>

    {#if rows.length}
      <div class="sq-table" role="table" aria-label={t('songPage.boardTitle')}>
        <div class="sq-row sq-row--head" role="row">
          <span class="sq-cell sq-cell--rank" role="columnheader">#</span>
          <span class="sq-cell" role="columnheader"><bb-fit>{t('songPage.colSong')}</bb-fit></span>
          <span class="sq-cell sq-cell--who" role="columnheader"><bb-fit>{t('songPage.colRequester')}</bb-fit></span>
        </div>
        {#each rows as row (row.position)}
          <div class="sq-row" class:sq-row--next={row.position === 1} role="row">
            <span class="sq-cell sq-cell--rank" role="cell">{row.position}</span>
            <span class="sq-cell sq-cell--song" role="cell">
              <span class="sq-name">{row.title}</span>
              <span class="sq-artist">{row.artists}</span>
            </span>
            <span class="sq-cell sq-cell--who" role="cell">
              <span class="sq-face" aria-hidden="true"><Bolota name={row.requester || data.login} size={32} /></span>
              <span class="sq-requester">{row.requester}</span>
            </span>
          </div>
        {/each}
      </div>
    {:else}
      <div class="sq-empty">
        <p class="sq-empty__title"><bb-fit>{emptyCopy.title}</bb-fit></p>
        <p class="sq-empty__body"><bb-fit mode="block">{emptyCopy.body}</bb-fit></p>
      </div>
    {/if}

    <p class="sq-status" role="status" aria-live="polite"><bb-fit>{statusLine}</bb-fit></p>
  </Card>
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

  .sq {
    position: relative;
    z-index: 1;
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-8);
    width: min(100% - 32px, 1080px);
    margin-inline: auto;
    padding: calc(var(--bb-nav-height) + var(--bb-space-8)) 0 var(--bb-space-section);
  }

  .sq-head {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--bb-space-3);
  }

  .sq-eyebrow {
    display: flex;
    align-items: center;
    gap: var(--bb-space-2);
    width: min(100%, 16rem);
    font-size: var(--bb-text-md);
    font-weight: 700;
    letter-spacing: 0.02em;
    color: var(--bb-green-glow);
    text-shadow: 0 0 18px rgba(var(--bb-green-glow-rgb), 0.45);
  }

  .sq-eyebrow__label {
    display: flex;
    flex: 1 1 0;
    min-width: 0;
  }

  .sq-beacon {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--bb-green-glow);
    box-shadow: 0 0 10px rgba(var(--bb-green-glow-rgb), 0.8);
  }

  .sq-title {
    margin: 0;
    font-family: var(--bb-font-display);
    font-size: clamp(2.5rem, 6vw, 4.5rem);
    font-weight: 500;
    letter-spacing: var(--bb-tracking-display);
    line-height: 1.04;
  }

  .sq-title--mid { font-size: clamp(2.25rem, 4.8vw, 3.5rem); }
  .sq-title--long { font-size: clamp(1.9rem, 3.6vw, 2.75rem); }

  .sq-word {
    display: inline-block;
    background: linear-gradient(180deg, var(--bb-white) 40%, rgba(var(--bb-tan-light-rgb), 0.55));
    background-clip: text;
    -webkit-background-clip: text;
    color: transparent;
    filter: drop-shadow(0 0 36px rgba(var(--bb-tan-rgb), 0.18));
  }

  .sq-tagline,
  .sq-hint {
    width: min(100%, 46ch);
    margin: 0;
    font-size: var(--bb-text-md);
    line-height: 1.6;
  }

  .sq-tagline { height: calc(2 * 1lh); color: var(--bb-muted-light); }
  .sq-hint { height: 1lh; font-size: var(--bb-text-sm); color: var(--bb-tan-light); }

  .sq-spots {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    align-items: end;
    gap: var(--bb-space-5);
    padding-top: var(--bb-space-6);
  }

  .sq-spot {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .sq-spot__face {
    position: relative;
    z-index: 2;
    display: flex;
    justify-content: center;
    height: 110px;
    margin-bottom: -48px;
  }

  .sq-spot :global(.sq-spot__card) {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-2);
    padding: calc(var(--bb-space-7) + 6px) var(--bb-space-6) var(--bb-space-6);
  }

  .sq-spot__label {
    display: flex;
    font-size: var(--bb-text-sm);
    font-weight: 600;
    color: var(--bb-paper);
  }

  .sq-spot--next .sq-spot__label { color: var(--bb-tier-paid); }

  .sq-spot__value {
    display: flex;
    margin-top: var(--bb-space-3);
    font-family: var(--bb-font-display);
    font-size: clamp(2.25rem, 3.6vw, 3rem);
    font-weight: 500;
    letter-spacing: -0.04em;
    line-height: 1.1;
    color: var(--bb-paper);
  }

  .sq-spot__rule {
    height: 1px;
    margin: var(--bb-space-3) 0 var(--bb-space-1);
    background: var(--bb-border);
  }

  .sq-spot__meta {
    display: flex;
    min-height: 1lh;
    font-size: var(--bb-text-sm);
    color: var(--bb-muted);
  }

  .sq :global(.sq-board) {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-5);
    padding: var(--bb-space-6) var(--bb-space-6) var(--bb-space-5);
  }

  .sq-board__intro {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-1);
    width: min(100%, 26rem);
  }

  .sq-board__title {
    display: flex;
    margin: 0;
    font-family: var(--bb-font-display);
    font-size: clamp(1.6rem, 2.6vw, 2.1rem);
    font-weight: 500;
    letter-spacing: -0.03em;
    color: var(--bb-paper);
  }

  .sq-board__note {
    display: flex;
    margin: 0;
    font-size: var(--bb-text-sm);
    color: var(--bb-muted);
  }

  .sq-table {
    display: flex;
    flex-direction: column;
    margin-inline: calc(var(--bb-space-3) * -1);
  }

  .sq-row {
    display: grid;
    grid-template-columns: 3.5rem minmax(0, 1fr) 14rem;
    align-items: center;
    gap: var(--bb-space-4);
    min-height: 64px;
    padding: 0 var(--bb-space-3);
    border-radius: var(--bb-radius-sm);
    transition: background var(--bb-dur-fast) ease, box-shadow var(--bb-dur-fast) ease;
  }

  .sq-row + .sq-row:not(:hover) { box-shadow: inset 0 1px 0 rgba(var(--bb-white-rgb), 0.05); }
  .sq-row:not(.sq-row--head):hover { background: var(--bb-elev-2); box-shadow: var(--bb-depth-1); }
  .sq-row:not(.sq-row--head):hover + .sq-row { box-shadow: none; }

  .sq-row--head {
    min-height: 36px;
    font-size: var(--bb-text-xs);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }

  .sq-cell--rank {
    font-family: var(--bb-font-display);
    font-size: 1.25rem;
    font-weight: 500;
    font-variant-numeric: tabular-nums;
    color: var(--bb-muted-light);
  }

  .sq-row--next .sq-cell--rank { color: var(--bb-tier-paid); }

  .sq-row--head .sq-cell--rank {
    font: inherit;
    color: inherit;
  }

  .sq-cell--song {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .sq-cell--who {
    display: flex;
    align-items: center;
    gap: var(--bb-space-3);
    min-width: 0;
  }

  .sq-face {
    flex: none;
    width: 32px;
    height: 32px;
  }

  .sq-name,
  .sq-artist,
  .sq-requester {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .sq-name {
    font-size: 1.05rem;
    font-weight: 600;
    color: var(--bb-paper);
  }

  .sq-artist {
    font-size: var(--bb-text-sm);
    color: var(--bb-muted);
  }

  .sq-requester {
    font-size: var(--bb-text-sm);
    color: var(--bb-muted-light);
  }

  .sq-empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--bb-space-2);
    width: min(100%, 360px);
    margin: var(--bb-space-6) auto;
    text-align: center;
  }

  .sq-empty__title {
    display: flex;
    justify-content: center;
    width: 100%;
    margin: 0;
    color: var(--bb-paper);
  }

  .sq-empty__body {
    width: 100%;
    height: calc(3 * 1lh);
    margin: 0;
    font-size: var(--bb-text-sm);
    color: var(--bb-muted);
  }

  .sq-status {
    display: flex;
    justify-content: center;
    height: 1lh;
    margin: 0;
    font: var(--bb-text-xs)/1.4 var(--bb-font-mono);
    color: var(--bb-muted);
  }

  @media (max-width: 760px) {
    .sq { padding-top: calc(var(--bb-nav-height) + var(--bb-space-6)); }
    .sq-tagline { height: calc(3 * 1lh); }
    .sq-hint { height: calc(2 * 1lh); }
    .sq-spots { grid-template-columns: minmax(0, 1fr); gap: var(--bb-space-6); }
    .sq :global(.sq-board) { padding: var(--bb-space-5) var(--bb-space-4) var(--bb-space-4); }
    .sq-row { grid-template-columns: 2.25rem minmax(0, 1fr) 8rem; gap: var(--bb-space-3); padding: 0 var(--bb-space-2); }
  }
</style>
