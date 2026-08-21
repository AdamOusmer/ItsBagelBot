<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { onMount, untrack } from 'svelte';
  import { AuroraBg, LightField, AlertBanner, Card, Icon, getI18n, type IconName } from '@bagel/shared';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();

  const { t, locale } = getI18n();

  // Fallback poll of /stats/data, armed only while the SSE stream is failed.
  const POLL_MS = 5000;
  // The boards ride the same 2s stream as the counters (a `boards` event beside
  // each snapshot frame), so this poll is only the stand-in for a failed
  // stream, at the same cadence as the counters' own fallback.
  // 0 -> value rise on first paint (ease-out-quart, as the shared countUp action).
  const INTRO_MS = 900;
  // Time constant of the chase that re-bases the odometer onto a fresh snapshot:
  // ~95% of the correction lands within 3τ ≈ 600ms, which reads as a drift
  // rather than a jump.
  const TAU_MS = 200;
  // Rates are a read-out, not an odometer — settle them a touch more slowly.
  const RATE_TAU_MS = 300;
  // A throttled tab (or a slept laptop) must not integrate minutes of rate into
  // a single frame.
  const MAX_DT_MS = 250;
  // How far past the last snapshot we are willing to extrapolate. A stalled
  // stream should coast to a stop, not invent a number nobody can vouch for.
  const MAX_PROJECT_S = 10;
  // Slowest the totals may move while a downward correction is being absorbed,
  // as a share of the live rate. The counters are meant to read as running, so
  // they crawl through a correction rather than parking on one value.
  const MIN_CRAWL = 0.25;

  // The SSR snapshot is a seed, not a binding: from hydration on, this page's
  // numbers come from its own stream, never from a re-run of the page load. Read
  // it untracked so that intent is explicit (and so the seed-vs-prop reactivity
  // warning does not fire on a deliberate one-time copy).
  const seed = untrack(() => data.stats);
  type Snapshot = typeof seed;

  // Last snapshot from the server: seeded by SSR, replaced by each SSE frame.
  // `live` only ever holds healthy numbers; `degraded` tracks the banner on its
  // own so an outage frame cannot rebase the odometer (see applySnapshot).
  let live = $state(seed);
  let degraded = $state(seed.degraded);

  // The two per-channel boards. Same seed-not-binding rule as the odometer, but
  // no extrapolation: these are printed exactly as the server last sent them.
  let boards = $state(untrack(() => data.boards));

  // What the tiles actually print. Seeded from the SSR numbers so the no-JS /
  // pre-hydration render already carries the real values; onMount then runs the
  // 0 -> value rise and hands over to the ticking loop below.
  let display = $state({
    messages: seed.messages_total,
    events: seed.events_total,
    msgRate: seed.msg_rate ?? 0,
    eventRate: seed.event_rate ?? 0
  });

  type Frame = typeof display;

  // Trajectory bookkeeping — deliberately not $state: it is read and written by
  // the animation frame, never rendered.
  let snapAt = 0; // performance.now() when the current snapshot landed
  let introAt = 0; // start of the 0 -> value rise
  let lastFrame = 0;
  let raf = 0;
  let reduced = false;
  let streamDown = false;

  /**
   * Where the totals should stand *right now*: the last snapshot's numbers plus
   * its rate times the time since it landed. This is what makes the counters
   * live — between snapshots the display keeps climbing at the fleet's own
   * rate instead of sitting still for 2s and then stepping.
   */
  function targetFrame(now: number): Frame {
    const secs = Math.min(Math.max(0, now - snapAt) / 1000, MAX_PROJECT_S);
    const msgRate = live.msg_rate ?? 0;
    const eventRate = live.event_rate ?? 0;
    return {
      messages: live.messages_total + msgRate * secs,
      events: live.events_total + eventRate * secs,
      msgRate,
      eventRate
    };
  }

  function lerp(from: number, to: number, eased: number): number {
    return from + (to - from) * eased;
  }

  /**
   * Fraction of the remaining gap to close on this frame.
   *
   * After the intro it is an exponential chase (time constant TAU_MS): the
   * target moves continuously, so a fixed-duration tween would have to be
   * restarted on every snapshot; a chase absorbs a moving target for free.
   * During the intro it is the same ease-out-quart the shared countUp uses,
   * rewritten as a per-frame closing fraction so the moving target does not
   * break the curve.
   */
  function closingFraction(now: number, dt: number): number {
    const p = (now - introAt) / INTRO_MS;
    if (p >= 1) return 1 - Math.exp(-dt / TAU_MS);
    const before = Math.max(0, (now - dt - introAt) / INTRO_MS);
    return 1 - Math.pow((1 - p) / (1 - before), 4);
  }

  /**
   * One frame of one total: chase the (moving) target, but never below a crawl.
   *
   * The floor does both jobs the odometer needs. It is monotonic, so a downward
   * correction is never visible as digits running backwards; and it keeps a
   * fraction of the live rate flowing, so the digits stay in motion even while
   * an over-extrapolation is being corrected away instead of parking for a few
   * hundred ms. A rate of 0 floors to a standstill, which is the honest reading.
   */
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

  /** Land on the current truth immediately (reduced motion, tab re-shown). */
  function snap(now: number): void {
    lastFrame = now;
    display = targetFrame(now);
  }

  function applySnapshot(next: Snapshot): void {
    degraded = next.degraded;
    // A degraded snapshot carries zeros, not truth: raise the banner and keep
    // the last good numbers coasting rather than rebasing the odometer onto
    // nothing (which the reset branch below would read as a counter reset).
    if (next.degraded) return;
    const prev = live;
    live = next;
    snapAt = performance.now();
    if (reduced) {
      snap(snapAt); // no extrapolation for reduced motion: each snapshot lands as-is
      return;
    }
    // A total that genuinely went down is a counter reset, not over-extrapolation:
    // drop the monotonic floor with it instead of freezing the tile forever.
    if (next.messages_total < prev.messages_total) display.messages = next.messages_total;
    if (next.events_total < prev.events_total) display.events = next.events_total;
  }

  async function refresh(): Promise<void> {
    // A backgrounded tab keeps neither an honest rAF nor a reason to poll; the
    // visibility listener catches it up the moment it comes back.
    if (document.hidden) return;
    try {
      const res = await fetch('/stats/data', { headers: { accept: 'application/json' } });
      if (!res.ok) return;
      applySnapshot(await res.json());
    } catch {
      // Keep the last good snapshot on a network blip rather than blanking.
    }
  }

  /** Fallback refresh of the leaderboards; a hidden tab keeps its last ranking. */
  async function refreshBoards(): Promise<void> {
    if (document.hidden) return;
    try {
      const res = await fetch('/stats/boards', { headers: { accept: 'application/json' } });
      if (!res.ok) return;
      boards = await res.json();
    } catch {
      // Keep the last ranking on a network blip rather than emptying the board.
    }
  }

  /**
   * Live snapshots over SSE. EventSource reconnects on its own, so a failure
   * only needs to arm the old 5s poll of /stats/data as a stand-in and stand it
   * down again once the stream is back.
   */
  function openStream(): EventSource {
    const es = new EventSource('/stats/stream');
    es.onopen = () => (streamDown = false);
    es.onerror = () => (streamDown = true);
    es.onmessage = (ev) => {
      try {
        applySnapshot(JSON.parse(ev.data) as Snapshot);
      } catch {
        // Malformed frame: ignore it, the next one is 2s out.
      }
    };
    // The boards arrive on their own event so the counter frame keeps its
    // shape. They are printed as sent — no extrapolation, no chase: a rank is
    // a fact about a moment, not a trajectory.
    es.addEventListener('boards', (ev) => {
      try {
        boards = JSON.parse((ev as MessageEvent<string>).data) as typeof boards;
      } catch {
        // Malformed frame: keep the last ranking.
      }
    });
    return es;
  }

  onMount(() => {
    reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
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

    const es = openStream();
    const timer = setInterval(() => {
      if (!streamDown) return;
      void refresh();
      void refreshBoards();
    }, POLL_MS);

    const onVisible = () => {
      if (document.hidden) return;
      // rAF is suspended while hidden: land on the truth, then resume ticking.
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
      es.close();
      clearInterval(timer);
      document.removeEventListener('visibilitychange', onVisible);
      cancelAnimationFrame(raf);
    };
  });

  const totalFmt = new Intl.NumberFormat(locale, { maximumFractionDigits: 0 });
  const rateFmt = new Intl.NumberFormat(locale, { maximumFractionDigits: 1 });

  const PENDING = '—';

  // Split at render time, not in the catalogs: the stagger is presentation, and
  // the headline stays one translatable sentence.
  const headline = $derived(t('stats.headline').split(' '));

  // Two tiles, not four: each lifetime total carries its own live rate as a
  // subline, so the page opens on the two numbers it is actually about.
  const tiles = $derived([
    {
      icon: 'send' as IconName,
      tan: false,
      label: t('stats.messagesLabel'),
      value: totalFmt.format(Math.round(display.messages)),
      rate: live.msg_rate === null ? null : rateFmt.format(display.msgRate),
      rateLabel: t('stats.messageRateLabel')
    },
    {
      icon: 'pulse' as IconName,
      tan: true,
      label: t('stats.eventsLabel'),
      value: totalFmt.format(Math.round(display.events)),
      rate: live.event_rate === null ? null : rateFmt.format(display.eventRate),
      rateLabel: t('stats.eventRateLabel')
    }
  ]);

  const traffic = $derived(boards.channels);
  const feed = $derived(boards.feed);

  // Both boards label a channel with the name the fleet stored for it, which is
  // a display name: usually the login with capitals, which the public channel
  // page lowercases on the way in — but not always (a localized display name
  // has no login shape at all). Link by id when the label cannot be one, rather
  // than linking somewhere that 404s.
  const LOGIN_SHAPE = /^[A-Za-z0-9_]{1,25}$/;
  const channelHref = (row: { id: string; name: string }) =>
    `/user/${LOGIN_SHAPE.test(row.name) ? row.name : row.id}`;
</script>

<svelte:head>
  <title>{t('stats.title')}</title>
  <meta name="description" content={t('stats.metaDescription')} />
  <!-- The page answers on two hosts (stats.itsbagelbot.com root and
       dashboard.itsbagelbot.com/stats); the subdomain is the one search
       engines and shares should converge on. -->
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
    <!-- Word-by-word entrance, as on /login. The shared `.reveal` steps in 80ms
         per unit of --i, so fractional steps give the words a tighter cadence
         than the sections around them. -->
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
      <AlertBanner variant="warn" icon="clock">{t('stats.degraded')}</AlertBanner>
    </div>
  {/if}

  <section class="tiles" aria-label={t('stats.headline')}>
    <!-- The surface is the shared Card the management pages and the public
         channel page use, with `hover` on: cursor-tracked tan spotlight, border
         brighten, small lift. The staggered rise stays on a wrapper, because
         `.reveal` ends on a filled `transform` that would otherwise outrank the
         card's own hover lift. -->
    {#each tiles as tile, i (tile.label)}
      <div class="tile-wrap reveal" style="--i:{5 + i * 0.5}">
        <Card hover class="tile">
          <div class="tile-head">
            <span class="ico" class:tan={tile.tan} aria-hidden="true"><Icon name={tile.icon} size={16} /></span>
            <span class="label">{tile.label}</span>
          </div>
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
          </div>
        </Card>
      </div>
    {/each}
  </section>

  <section class="boards" aria-label={t('stats.boardsEyebrow')}>
    <div class="board-wrap reveal" style="--i:6">
      <Card class="board">
        <header class="board-head">
          <span class="ico" aria-hidden="true"><Icon name="activity" size={16} /></span>
          <div class="board-titles">
            <h2>{t('stats.trafficBoardTitle')}</h2>
            <p>{t('stats.trafficBoardNote')}</p>
          </div>
        </header>
        {#if traffic.length === 0}
          <p class="empty">{t('stats.boardEmpty')}</p>
        {:else}
          <!-- Own scroll container: a ten-digit total on a narrow phone must
               scroll the table, never the page. -->
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
                    <td class="chan">
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
      <Card class="board">
        <header class="board-head">
          <span class="ico tan" aria-hidden="true"><Icon name="heart" size={16} /></span>
          <div class="board-titles">
            <h2>{t('stats.feedBoardTitle')}</h2>
            <p>{t('stats.feedBoardNote')}</p>
          </div>
        </header>
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
                <span class="chan">
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
    <span class="pip" aria-hidden="true"></span>
    <span>{t('stats.liveNote')}</span>
  </footer>
</main>

<style>
  /* Mote field sits above the aurora (z-index 0) but below content (z-index 1).
     Own stacking context so LightField's z-index:-1 canvas stays contained. */
  .starfield {
    position: fixed;
    inset: 0;
    z-index: 0;
    pointer-events: none;
  }

  .stats-page {
    position: relative;
    z-index: 1;
    /* Clears the fixed 76px public nav the same way the channel page's hero
       does, then keeps its own breathing room above the eyebrow. */
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

  /* The shared Card, scaled up: the numerals are the subject here, so the tile
     just gets more padding and a column layout. Everything else — surface,
     hairline border, radius, hover spotlight/lift — is the Card's own. */
  .tile-wrap { min-width: 0; }

  .tiles :global(.card) {
    --card-pad: clamp(24px, 3.4vw, 40px);
    height: 100%;
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-5);
    min-width: 0;
  }
  /* Hairline of light along the top edge, as on the marketing surfaces. Free to
     use: Card's own ::before only paints under the (unused) `sheen` variant. */
  .tiles :global(.card)::before {
    content: '';
    position: absolute;
    inset: 0 0 auto;
    height: 1px;
    background: linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.14), transparent);
  }

  .tile-head { display: flex; align-items: center; gap: var(--bb-space-3); min-width: 0; }

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
  .ico.tan { background: rgba(201, 168, 124, 0.08); color: var(--bb-tan-light); }
  .ico :global(svg) { width: 16px; height: 16px; fill: none; stroke: currentColor; stroke-width: 1.6; stroke-linecap: round; stroke-linejoin: round; }

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
    /* Sized so a ten-digit total (13 characters with separators) still fits on
       one line in the narrowest two-column tile, just above the 720px collapse. */
    font-size: clamp(30px, 5vw, 60px);
    line-height: 1;
    letter-spacing: var(--bb-tracking-tight);
    color: var(--bb-white);
    /* Tabular figures: a digit that changes 60 times a second must not reflow
       the ones beside it. */
    font-variant-numeric: tabular-nums;
    overflow-wrap: anywhere;
  }

  .unit {
    font-family: var(--bb-font-mono);
    font-size: clamp(13px, 1.4vw, 17px);
    color: var(--bb-muted);
  }

  /* Rate subline: the tile's second number, deliberately a read-out rather than
     an odometer — same tabular figures, a third of the size. */
  .rate {
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: calc(-1 * var(--bb-space-3));
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

  .boards :global(.card) {
    --card-pad: clamp(20px, 2.4vw, 30px);
    height: 100%;
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

  /* Rank column: a fixed mono gutter so the names line up whatever the digits. */
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
  .chan a {
    color: var(--bb-white);
    text-decoration: none;
    border-bottom: 1px solid transparent;
    transition: color 140ms ease, border-color 140ms ease;
  }
  .chan a:hover, .chan a:focus-visible { color: var(--bb-green-glow); border-bottom-color: currentColor; }
  .unnamed { color: var(--bb-muted); font-style: italic; }

  /* Feed board: one big tan total, then the podium as a list — the ranking is
     one number per row, so a table would be three quarters chrome. */
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
  .pip {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--bb-green-glow);
    box-shadow: 0 0 10px rgba(82, 183, 136, 0.7);
    animation: blink 2.4s ease-in-out infinite;
  }

  @keyframes blink { 0%, 100% { opacity: 1; } 50% { opacity: 0.35; } }

  @media (max-width: 900px) {
    .boards { grid-template-columns: minmax(0, 1fr); }
  }

  @media (max-width: 720px) {
    .tiles { grid-template-columns: minmax(0, 1fr); }
  }

  @media (prefers-reduced-motion: reduce) {
    .pip { animation: none; }
  }
</style>
