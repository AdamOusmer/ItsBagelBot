<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import PublicStreamerHeader from './PublicStreamerHeader.svelte';
  import LightField from '@bagel/ui/svelte/LightField.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import Bolota from '@bagel/ui/svelte/Bolota.svelte';

  type Row = { key: string; position: number; name: string; detail?: string; face: string; wait: string; share: number; search: string };
  type Copy = { title: string; body: string };

  type Props = {
    eyebrow: string;
    channelName: string;
    tagline: string;
    hint: string;
    next: { label: string; value: string; meta: string; face: string | null };
    count: { label: string; value: string; status: string; open: boolean };
    board: { title: string; note: string; colName: string; colWait: string; searchLabel: string; searchPlaceholder: string };
    rows: Row[];
    trouble: boolean;
    statusLine: string;
    empty: Copy;
    noMatch: Copy;
  };

  let { eyebrow, channelName, tagline, hint, next, count, board, rows, trouble, statusLine, empty, noMatch }: Props = $props();

  let search = $state('');
  const needle = $derived(search.trim().toLowerCase());
  const visibleRows = $derived(needle ? rows.filter((row) => row.search.includes(needle)) : rows);
  const emptyCopy = $derived(!trouble && needle !== '' && rows.length > 0 ? noMatch : empty);
  const titleWords = $derived(channelName.split(' '));
  const titleSize = $derived(channelName.length > 16 ? 'long' : channelName.length > 9 ? 'mid' : '');
</script>

<div class="sky" aria-hidden="true">
  <LightField />
  <div class="bb-orb bb-orb--halo bb-orb--green orb orb--green"><i class="bb-orb__pulse"></i></div>
  <div class="bb-orb bb-orb--halo bb-orb--tan orb orb--tan"><i class="bb-orb__pulse"></i></div>
</div>

<main class="qp">
  <PublicStreamerHeader>
    <div class="qp-intro">
      <span class="qp-eyebrow"><span class="qp-beacon" aria-hidden="true"></span><span class="qp-eyebrow__label"><bb-fit>{eyebrow}</bb-fit></span></span>
      <h1 class="qp-title{titleSize ? ` qp-title--${titleSize}` : ''}">
        {#each titleWords as word, i (i)}{i > 0 ? ' ' : ''}<span class="qp-word">{word}</span>{/each}
      </h1>
      <p class="qp-tagline"><bb-fit mode="block">{tagline}</bb-fit></p>
      <p class="qp-hint"><bb-fit mode="block">{hint}</bb-fit></p>
    </div>
  </PublicStreamerHeader>

  <section class="qp-spots">
    <div class="qp-spot qp-spot--next">
      <div class="qp-spot__face" aria-hidden="true">
        {#if next.face}<Bolota name={next.face} size={110} motionState="idle" active gate />{/if}
      </div>
      <Card class="qp-spot__card" tone="accent" raised>
        <span class="qp-spot__label"><bb-fit>{next.label}</bb-fit></span>
        <div class="qp-spot__value"><bb-fit>{next.value}</bb-fit></div>
        <div class="qp-spot__rule"></div>
        <div class="qp-spot__meta"><bb-fit>{next.meta}</bb-fit></div>
      </Card>
    </div>
    <div class="qp-spot">
      <Card class="qp-spot__card">
        <span class="qp-spot__label"><bb-fit>{count.label}</bb-fit></span>
        <div class="qp-spot__value"><bb-fit>{count.value}</bb-fit></div>
        <div class="qp-spot__rule"></div>
        <div class="qp-spot__meta" role="status">
          <span class="qp-dot" class:qp-dot--open={count.open} aria-hidden="true"></span>
          <span class="qp-spot__status"><bb-fit>{count.status}</bb-fit></span>
        </div>
      </Card>
    </div>
  </section>

  <Card class="qp-board">
    <div class="qp-board__head">
      <div class="qp-board__intro">
        <h2 class="qp-board__title"><bb-fit>{board.title}</bb-fit></h2>
        <p class="qp-board__note"><bb-fit>{board.note}</bb-fit></p>
      </div>
      <form class="qp-search" role="search" onsubmit={(event) => event.preventDefault()}>
        <SearchInput
          fill
          tall
          name="q"
          bind:value={search}
          autocomplete="off"
          enterkeyhint="search"
          placeholder={board.searchPlaceholder}
          aria-label={board.searchLabel}
        />
      </form>
    </div>

    {#if visibleRows.length}
      <div class="qp-table" role="table" aria-label={board.title}>
        <div class="qp-row qp-row--head" role="row">
          <span class="qp-cell qp-cell--rank" role="columnheader">#</span>
          <span class="qp-cell" role="columnheader"><bb-fit>{board.colName}</bb-fit></span>
          <span class="qp-cell qp-cell--wait" role="columnheader"><bb-fit>{board.colWait}</bb-fit></span>
        </div>
        {#each visibleRows as row (row.key)}
          <div class="qp-row" class:qp-row--next={row.position === 1} role="row" style="--share: {row.share}">
            <span class="qp-cell qp-cell--rank" role="cell">{row.position}</span>
            <span class="qp-cell qp-cell--viewer" role="cell">
              <span class="qp-face" aria-hidden="true"><Bolota name={row.face} size={40} /></span>
              {#if row.detail}
                <span class="qp-text">
                  <span class="qp-name">{row.name}</span>
                  <span class="qp-detail">{row.detail}</span>
                </span>
              {:else}
                <span class="qp-name">{row.name}</span>
              {/if}
            </span>
            <span class="qp-cell qp-cell--wait" role="cell">
              <span class="qp-wait">{row.wait}</span>
              <span class="qp-meter" aria-hidden="true"><span class="qp-meter__fill"></span></span>
            </span>
          </div>
        {/each}
      </div>
    {:else}
      <div class="qp-empty">
        <p class="qp-empty__title"><bb-fit>{emptyCopy.title}</bb-fit></p>
        <p class="qp-empty__body"><bb-fit mode="block">{emptyCopy.body}</bb-fit></p>
      </div>
    {/if}

    <p class="qp-status" role="status" aria-live="polite"><bb-fit>{statusLine}</bb-fit></p>
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

  .qp {
    position: relative;
    z-index: 1;
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-6);
    width: min(100% - 32px, 1080px);
    margin-inline: auto;
    padding: calc(var(--bb-nav-height) + var(--bb-space-8)) 0 var(--bb-space-section);
  }

  .qp-intro {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--bb-space-3);
  }

  .qp-eyebrow {
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

  .qp-eyebrow__label {
    display: flex;
    flex: 1 1 0;
    min-width: 0;
  }

  .qp-beacon {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--bb-green-glow);
    box-shadow: 0 0 10px rgba(var(--bb-green-glow-rgb), 0.8);
  }

  .qp-title {
    margin: 0;
    font-family: var(--bb-font-display);
    font-size: clamp(2.5rem, 6vw, 4.5rem);
    font-weight: 500;
    letter-spacing: var(--bb-tracking-display);
    line-height: 1.04;
  }

  .qp-title--mid { font-size: clamp(2.25rem, 4.8vw, 3.5rem); }
  .qp-title--long { font-size: clamp(1.9rem, 3.6vw, 2.75rem); }

  .qp-word {
    display: inline-block;
    background: linear-gradient(180deg, var(--bb-white) 40%, rgba(var(--bb-tan-light-rgb), 0.55));
    background-clip: text;
    -webkit-background-clip: text;
    color: transparent;
    filter: drop-shadow(0 0 36px rgba(var(--bb-tan-rgb), 0.18));
  }

  .qp-tagline,
  .qp-hint {
    width: min(100%, 46ch);
    margin: 0;
    font-size: var(--bb-text-md);
    line-height: 1.6;
  }

  .qp-tagline { color: var(--bb-muted-light); }
  .qp-hint { font-size: var(--bb-text-sm); color: var(--bb-tan-light); }

  .qp-spots {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    align-items: end;
    gap: var(--bb-space-5);
    padding-top: var(--bb-space-4);
  }

  .qp-spot {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .qp-spot__face {
    position: relative;
    z-index: 2;
    display: flex;
    justify-content: center;
    height: 110px;
    margin-bottom: -48px;
  }

  .qp-spot :global(.qp-spot__card) {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-2);
    padding: calc(var(--bb-space-7) + 6px) var(--bb-space-6) var(--bb-space-6);
  }

  .qp-spot__label {
    display: flex;
    font-size: var(--bb-text-sm);
    font-weight: 600;
    color: var(--bb-paper);
  }

  .qp-spot--next .qp-spot__label { color: var(--bb-tier-paid); }

  .qp-spot__value {
    display: flex;
    margin-top: var(--bb-space-3);
    font-family: var(--bb-font-display);
    font-size: clamp(2.25rem, 3.6vw, 3rem);
    font-weight: 500;
    letter-spacing: -0.04em;
    line-height: 1.1;
    color: var(--bb-paper);
  }

  .qp-spot__rule {
    height: 1px;
    margin: var(--bb-space-3) 0 var(--bb-space-1);
    background: var(--bb-border);
  }

  .qp-spot__meta {
    display: flex;
    align-items: center;
    gap: var(--bb-space-2);
    min-height: 1lh;
    font-size: var(--bb-text-sm);
    color: var(--bb-muted);
  }

  .qp-spot__status {
    display: flex;
    flex: 1 1 0;
    min-width: 0;
  }

  .qp-dot {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--bb-tier-inactive);
  }

  .qp-dot--open {
    background: var(--bb-tier-free);
    box-shadow: 0 0 10px rgba(var(--bb-green-glow-rgb), 0.7);
  }

  .qp :global(.qp-board) {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-5);
    padding: var(--bb-space-6) var(--bb-space-6) var(--bb-space-5);
  }

  .qp-board__head {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    justify-content: space-between;
    gap: var(--bb-space-5);
  }

  .qp-board__intro {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-1);
    width: min(100%, 26rem);
  }

  .qp-board__title {
    display: flex;
    margin: 0;
    font-family: var(--bb-font-display);
    font-size: clamp(1.6rem, 2.6vw, 2.1rem);
    font-weight: 500;
    letter-spacing: -0.03em;
    color: var(--bb-paper);
  }

  .qp-board__note {
    display: flex;
    margin: 0;
    font-size: var(--bb-text-sm);
    color: var(--bb-muted);
  }

  .qp-search {
    width: 300px;
    max-width: 100%;
  }

  .qp-table {
    display: flex;
    flex-direction: column;
    margin-inline: calc(var(--bb-space-3) * -1);
  }

  .qp-row {
    display: grid;
    grid-template-columns: 3.5rem minmax(0, 1fr) 11rem;
    align-items: center;
    gap: var(--bb-space-4);
    min-height: 64px;
    padding: 0 var(--bb-space-3);
    border-radius: var(--bb-radius-sm);
    transition: background var(--bb-dur-fast) ease, box-shadow var(--bb-dur-fast) ease;
  }

  .qp-row + .qp-row:not(:hover) { box-shadow: inset 0 1px 0 rgba(var(--bb-white-rgb), 0.05); }
  .qp-row:not(.qp-row--head):hover { background: var(--bb-elev-2); box-shadow: var(--bb-depth-1); }
  .qp-row:not(.qp-row--head):hover + .qp-row { box-shadow: none; }

  .qp-row--head {
    min-height: 36px;
    font-size: var(--bb-text-xs);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }

  .qp-cell--rank {
    font-family: var(--bb-font-display);
    font-size: 1.25rem;
    font-weight: 500;
    font-variant-numeric: tabular-nums;
    color: var(--bb-muted-light);
  }

  .qp-row--next .qp-cell--rank { color: var(--bb-tier-paid); }

  .qp-cell--viewer {
    display: flex;
    align-items: center;
    gap: var(--bb-space-3);
    min-width: 0;
  }

  .qp-face {
    flex: none;
    width: 40px;
    height: 40px;
  }

  .qp-name {
    min-width: 0;
    overflow: hidden;
    font-size: 1.05rem;
    font-weight: 600;
    color: var(--bb-paper);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .qp-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .qp-detail {
    min-width: 0;
    overflow: hidden;
    font-size: var(--bb-text-sm);
    color: var(--bb-muted);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .qp-cell--wait {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 6px;
  }

  .qp-row--head .qp-cell--rank,
  .qp-row--head .qp-cell--wait {
    font: inherit;
    color: inherit;
  }

  .qp-wait {
    font-size: 1rem;
    font-variant-numeric: tabular-nums;
    color: var(--bb-paper);
    white-space: nowrap;
  }

  .qp-meter {
    width: 100%;
    height: 4px;
    overflow: hidden;
    border-radius: var(--bb-radius-pill);
    background: rgba(var(--bb-white-rgb), 0.06);
  }

  .qp-meter__fill {
    display: block;
    width: calc(var(--share) * 100%);
    height: 100%;
    margin-left: auto;
    border-radius: inherit;
    background: var(--bb-green-glow);
    opacity: 0.75;
  }

  .qp-empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--bb-space-2);
    width: min(100%, 360px);
    margin: var(--bb-space-6) auto;
    text-align: center;
  }

  .qp-empty__title {
    display: flex;
    justify-content: center;
    width: 100%;
    margin: 0;
    color: var(--bb-paper);
  }

  .qp-empty__body {
    width: 100%;
    height: calc(3 * 1lh);
    margin: 0;
    font-size: var(--bb-text-sm);
    color: var(--bb-muted);
  }

  .qp-status {
    display: flex;
    justify-content: center;
    height: 1lh;
    margin: 0;
    font: var(--bb-text-xs)/1.4 var(--bb-font-mono);
    color: var(--bb-muted);
  }

  @media (max-width: 760px) {
    .qp { padding-top: calc(var(--bb-nav-height) + var(--bb-space-6)); }
    .qp-spots { grid-template-columns: minmax(0, 1fr); gap: var(--bb-space-6); }
    .qp :global(.qp-board) { padding: var(--bb-space-5) var(--bb-space-4) var(--bb-space-4); }
    .qp-row { grid-template-columns: 2.25rem minmax(0, 1fr) 7rem; gap: var(--bb-space-3); padding: 0 var(--bb-space-2); }
    .qp-search { width: 100%; }
  }
</style>
