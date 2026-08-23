<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { AuroraBg, LightField, AlertBanner, Card, Icon, getI18n } from '@bagel/shared';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();

  const { t, locale } = getI18n();

  // Display identity per row: stored display name wins, then login, then a
  // neutral placeholder for rows whose accrual never carried identity.
  const rowName = (v: { viewerName: string; viewerLogin: string; viewerId: string }) =>
    v.viewerName || v.viewerLogin || t('leaderboard.anonymousViewer');

  const totalFmt = new Intl.NumberFormat(locale, { maximumFractionDigits: 0 });
  const hoursFmt = (seconds: number): string => {
    const hours = seconds / 3600;
    return hours < 10
      ? new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(hours)
      : totalFmt.format(Math.round(hours));
  };

  // The podium is presentation over data: the same rows, first three by rank.
  const podium = $derived(data.top.slice(0, 3));
  const rest = $derived(data.top.slice(3));

  // The hero's channel name links to its public command page, which lives on
  // the static /user/<login> route — reachable on this host like any other.
  const channelHref = $derived(`/user/${data.login}`);
</script>

<svelte:head>
  <title>{t('leaderboard.title', { channel: data.channelName })}</title>
  <meta name="description" content={t('leaderboard.metaDescription', { channel: data.channelName })} />
  <!-- The page answers only on the leaderboard subdomain; that origin is the
       one shares and search engines should converge on. -->
  <link rel="canonical" href="https://leaderboard.itsbagelbot.com/{data.login}" />
  <meta property="og:url" content="https://leaderboard.itsbagelbot.com/{data.login}" />
  <meta property="og:title" content={t('leaderboard.title', { channel: data.channelName })} />
  <meta property="og:description" content={t('leaderboard.metaDescription', { channel: data.channelName })} />
  <meta name="twitter:title" content={t('leaderboard.title', { channel: data.channelName })} />
  <meta name="twitter:description" content={t('leaderboard.metaDescription', { channel: data.channelName })} />
</svelte:head>

<AuroraBg />
<div class="starfield" aria-hidden="true"><LightField /></div>

<main class="lb-page">
  <header class="hero">
    <div class="eyebrow reveal" style="--i:0">{t('leaderboard.eyebrow')}</div>
    <h1 class="headline">
      <span class="word pre reveal" style="--i:0.5">{t('leaderboard.headlinePrefix')}</span>
      <span class="word reveal" style="--i:1">{data.channelName}&nbsp;</span>
    </h1>
    <p class="lede reveal" style="--i:2">{t('leaderboard.tagline', { channel: data.channelName })}</p>
    <a class="channel-link reveal" style="--i:2.5" href={channelHref}>{t('leaderboard.visitChannel')}</a>
  </header>

  {#if data.degraded}
    <div class="notice reveal" style="--i:3">
      <AlertBanner variant="warn" icon="clock">{t('leaderboard.degraded')}</AlertBanner>
    </div>
  {:else if data.top.length === 0}
    <div class="podium-wrap reveal" style="--i:3">
      <Card class="empty-card">
        <span class="ico gem" aria-hidden="true"><Icon name="gem" size={18} /></span>
        <h2>{t('leaderboard.emptyTitle')}</h2>
        <p>{t('leaderboard.emptyBody', { channel: data.channelName })}</p>
      </Card>
    </div>
  {:else}
    <!-- The podium: ranks two and three flank the leader via CSS order, like a
         real podium; narrow screens collapse back to rank order. -->
    <section class="podium" aria-label={t('leaderboard.podiumLabel')}>
      {#each podium as viewer, i (viewer.viewerId)}
        <div class="spot-wrap reveal place-{i + 1}" style="--i:{3 + i * 0.5}">
          <Card hover class="spot">
            <span class="medal medal-{i + 1}" aria-hidden="true">{i + 1}</span>
            <span class="avatar" aria-hidden="true">{rowName(viewer).slice(0, 2)}</span>
            <span class="name" title={viewer.viewerLogin || viewer.viewerName}>{rowName(viewer)}</span>
            <span class="points">
              <span class="num">{totalFmt.format(viewer.points)}</span>
              <span class="currency">{data.currencyName}</span>
            </span>
            <span class="watched">
              <Icon name="clock" size={12} />
              {hoursFmt(viewer.watchSeconds)}&nbsp;{t('leaderboard.watchUnit')}
            </span>
          </Card>
        </div>
      {/each}
    </section>

    <section class="board-wrap reveal" style="--i:5" aria-label={t('leaderboard.boardLabel')}>
      <Card class="board">
        <header class="board-head">
          <span class="ico" aria-hidden="true"><Icon name="users" size={16} /></span>
          <div class="board-titles">
            <h2>{t('leaderboard.boardTitle')}</h2>
            <p>{t('leaderboard.boardNote')}</p>
          </div>
        </header>
        {#if rest.length === 0}
          <p class="solo-note">{t('leaderboard.soloNote')}</p>
        {:else}
          <div class="table-scroll">
            <table>
              <thead>
                <tr>
                  <th class="rank" scope="col">{t('leaderboard.colRank')}</th>
                  <th scope="col">{t('leaderboard.colViewer')}</th>
                  <th class="n" scope="col">{t('leaderboard.colWatched')}</th>
                  <th class="n" scope="col">{t('leaderboard.colPoints')}</th>
                </tr>
              </thead>
              <tbody>
                {#each rest as viewer, i (viewer.viewerId)}
                  <tr>
                    <td class="rank">{i + 4}</td>
                    <td class="viewer">
                      <span class="avatar-sm" aria-hidden="true">{rowName(viewer).slice(0, 1)}</span>
                      <span class="viewer-name">{rowName(viewer)}</span>
                    </td>
                    <td class="n muted">
                      <Icon name="clock" size={11} />
                      {hoursFmt(viewer.watchSeconds)}&nbsp;{t('leaderboard.watchUnit')}
                    </td>
                    <td class="n points-cell">{totalFmt.format(viewer.points)}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
          <p class="ranked-note">{t('leaderboard.rankedNote', { count: totalFmt.format(data.top.length) })}</p>
        {/if}
      </Card>
    </section>
  {/if}

  <footer class="foot reveal" style="--i:7">
    <span class="pip" aria-hidden="true"></span>
    <span>{t('leaderboard.earnNote')}</span>
  </footer>
</main>

<style>
  /* Mote field sits above the aurora (z-index 0) but below content (z-index 1),
     the same stacking the stats page uses. */
  .starfield {
    position: fixed;
    inset: 0;
    z-index: 0;
    pointer-events: none;
  }

  .lb-page {
    position: relative;
    z-index: 1;
    min-height: calc(100vh - 76px);
    max-width: var(--bb-content-max);
    margin: 0 auto;
    padding: calc(76px + env(safe-area-inset-top, 0px) + clamp(40px, 8vh, 88px)) var(--bb-space-5)
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
    font-size: clamp(34px, 6vw, 68px);
    line-height: 1.04;
    letter-spacing: var(--bb-tracking-tight);
    color: var(--bb-white);
    margin: 0;
    max-width: 24ch;
    overflow-wrap: anywhere;
  }
  /* The lead-in word ("top of" / "hall of" shape) reads as a quiet prefix; the
     channel name that follows is the tan subject of the page. */
  .word { display: inline-block; }
  .word.pre {
    font-size: 0.5em;
    font-weight: 600;
    color: var(--bb-muted);
    vertical-align: middle;
    margin-right: 0.35em;
    text-transform: lowercase;
  }

  .lede {
    font-family: var(--bb-font-body);
    font-size: clamp(15px, 1.6vw, 18px);
    line-height: 1.6;
    color: var(--bb-muted);
    margin: 0;
    max-width: 56ch;
  }

  .channel-link {
    font-family: var(--bb-font-mono);
    font-size: 12px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--bb-green-glow);
    text-decoration: none;
    border-bottom: 1px solid transparent;
    transition: border-color 140ms ease;
  }
  .channel-link:hover, .channel-link:focus-visible { border-bottom-color: currentColor; }

  .notice { max-width: 640px; width: 100%; margin: 0 auto; }

  /* --- Podium ------------------------------------------------------------ */

  .podium {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: var(--bb-space-4);
    align-items: stretch;
    max-width: 980px;
    width: 100%;
    margin: 0 auto;
  }

  .spot-wrap { min-width: 0; }
  /* First place stands taller in the middle; CSS order puts rank 1 between
     ranks 2 and 3, like a real podium. */
  .place-1 { order: 2; }
  .place-2 { order: 1; }
  .place-3 { order: 3; }
  @media (max-width: 720px) {
    .place-1, .place-2, .place-3 { order: 0; }
  }

  .podium :global(.card) {
    --card-pad: clamp(20px, 2.4vw, 30px);
    height: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    gap: var(--bb-space-3);
    min-width: 0;
    position: relative;
  }
  .place-1 :global(.card) {
    padding-top: calc(clamp(20px, 2.4vw, 30px) + var(--bb-space-4));
  }
  /* Hairline of light along the top edge, as the stats tiles wear. */
  .podium :global(.card)::before {
    content: '';
    position: absolute;
    inset: 0 0 auto;
    height: 1px;
    background: linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.14), transparent);
  }
  :global(:root[data-theme='light']) .podium :global(.card)::before {
    background: linear-gradient(90deg, transparent, rgba(20, 17, 12, 0.12), transparent);
  }

  .medal {
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: 13px;
    line-height: 1;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    border-radius: 50%;
    position: absolute;
    top: 10px;
    left: 50%;
    transform: translateX(-50%);
    border: 1px solid var(--bb-border);
    background: rgba(255, 255, 255, 0.06);
    color: var(--bb-muted);
  }
  /* Gold wears the brand tan; silver stays pale; bronze dims the tan. */
  .medal-1 {
    background: rgba(201, 168, 124, 0.16);
    border-color: var(--bb-tan);
    color: var(--bb-tan-light);
    box-shadow: 0 0 18px rgba(201, 168, 124, 0.25);
  }
  .medal-2 { background: rgba(255, 255, 255, 0.1); color: var(--bb-white); }
  .medal-3 { background: rgba(201, 168, 124, 0.07); color: rgba(201, 168, 124, 0.8); }

  .avatar {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 52px;
    height: 52px;
    border-radius: 50%;
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: 19px;
    letter-spacing: var(--bb-tracking-tight);
    text-transform: uppercase;
    color: var(--bb-green-glow);
    background: rgba(82, 183, 136, 0.1);
    border: 1px solid var(--bb-border);
  }
  .place-1 .avatar {
    width: 62px;
    height: 62px;
    font-size: 23px;
    color: var(--bb-tan-light);
    background: rgba(201, 168, 124, 0.12);
  }

  .name {
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: 15px;
    line-height: 1.35;
    color: var(--bb-white);
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .points { display: flex; align-items: baseline; gap: 7px; min-width: 0; justify-content: center; }
  .points .num {
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: clamp(26px, 3.4vw, 40px);
    line-height: 1;
    letter-spacing: var(--bb-tracking-tight);
    color: var(--bb-white);
    font-variant-numeric: tabular-nums;
  }
  .place-1 .points .num { font-size: clamp(32px, 4vw, 50px); color: var(--bb-tan-light); }
  .points .currency {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: var(--bb-tracking-eyebrow);
    text-transform: uppercase;
    color: var(--bb-muted);
  }

  .watched {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--bb-muted);
    font-variant-numeric: tabular-nums;
  }
  .watched :global(svg) { fill: none; stroke: currentColor; stroke-width: 1.6; stroke-linecap: round; stroke-linejoin: round; }

  /* --- Board ------------------------------------------------------------- */

  .board-wrap {
    min-width: 0;
    max-width: 980px;
    width: 100%;
    margin: 0 auto;
  }

  .board-wrap :global(.card) {
    --card-pad: clamp(20px, 2.4vw, 30px);
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-4);
    min-width: 0;
  }

  .board-head { display: flex; align-items: flex-start; gap: var(--bb-space-3); min-width: 0; }
  .board-titles { min-width: 0; }

  .board-head h2 {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: clamp(18px, 2vw, 22px);
    line-height: 1.2;
    letter-spacing: var(--bb-tracking-tight);
    color: var(--bb-white);
    margin: 0;
  }

  .board-head p {
    font-family: var(--bb-font-body);
    font-size: 13px;
    line-height: 1.5;
    color: var(--bb-muted);
    margin: 4px 0 0;
  }

  .ico {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    flex: 0 0 auto;
    border-radius: var(--bb-radius-sm);
    border: 1px solid var(--bb-border);
    background: rgba(82, 183, 136, 0.08);
    color: var(--bb-green-glow);
  }
  .ico.gem { background: rgba(201, 168, 124, 0.08); color: var(--bb-tan-light); }
  .ico :global(svg) { width: 16px; height: 16px; fill: none; stroke: currentColor; stroke-width: 1.6; stroke-linecap: round; stroke-linejoin: round; }

  .empty-card {
    max-width: 640px;
    margin: 0 auto;
    width: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    gap: var(--bb-space-3);
    padding-block: var(--bb-space-7);
  }
  .empty-card :global(h2) {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: clamp(20px, 2.4vw, 26px);
    color: var(--bb-white);
    margin: 0;
    letter-spacing: var(--bb-tracking-tight);
  }
  .empty-card :global(p) {
    font-family: var(--bb-font-body);
    font-size: 14px;
    line-height: 1.6;
    color: var(--bb-muted);
    margin: 0;
    max-width: 44ch;
  }

  .solo-note {
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

  .viewer { display: flex; align-items: center; gap: var(--bb-space-3); min-width: 0; }
  .viewer-name { overflow: hidden; text-overflow: ellipsis; }

  .avatar-sm {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    flex: 0 0 auto;
    border-radius: 50%;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 11px;
    text-transform: uppercase;
    color: var(--bb-green-glow);
    background: rgba(82, 183, 136, 0.1);
    border: 1px solid var(--bb-border);
  }

  .n { text-align: right; font-variant-numeric: tabular-nums; }
  th.n { text-align: right; padding-right: 0; }

  .muted {
    color: var(--bb-muted);
    font-family: var(--bb-font-mono);
    font-size: 12px;
  }
  .muted :global(svg) { vertical-align: -1px; margin-right: 5px; fill: none; stroke: currentColor; stroke-width: 1.6; stroke-linecap: round; stroke-linejoin: round; }

  .points-cell { font-weight: 600; }

  .ranked-note {
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
  .pip {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--bb-green-glow);
    box-shadow: 0 0 10px rgba(82, 183, 136, 0.7);
    animation: blink 2.4s ease-in-out infinite;
  }
  :global(:root[data-theme='light']) .pip { box-shadow: 0 0 8px rgba(45, 106, 79, 0.4); }

  @keyframes blink { 0%, 100% { opacity: 1; } 50% { opacity: 0.35; } }

  @media (max-width: 900px) {
    .podium { grid-template-columns: minmax(0, 1fr); max-width: 480px; }
  }

  @media (prefers-reduced-motion: reduce) {
    .pip { animation: none; }
  }
</style>
