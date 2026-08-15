<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { AuroraBg, AlertBanner, Icon, getI18n, type IconName } from '@bagel/shared';
  import LangSwitch from '$lib/components/LangSwitch.svelte';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();

  const { t, locale } = getI18n();

  const POLL_MS = 5000;
  const ANIM_MS = 900;

  // The SSR snapshot is a seed, not a binding: from hydration on, this page's
  // numbers come from its own poll, never from a re-run of the page load. Read
  // it untracked so that intent is explicit (and so the seed-vs-prop reactivity
  // warning does not fire on a deliberate one-time copy).
  const seed = untrack(() => data.stats);

  // Last snapshot from the server: seeded by SSR, replaced by each poll.
  let live = $state(seed);

  // What the tiles actually print. Seeded from the SSR numbers so the no-JS /
  // pre-hydration render already carries the real values; onMount then runs the
  // same 0 -> value rise the shared `countUp` action does, and every later poll
  // tweens from the current display instead of snapping.
  let display = $state({
    messages: seed.messages_total,
    events: seed.events_total,
    msgRate: seed.msg_rate ?? 0,
    eventRate: seed.event_rate ?? 0
  });

  type Frame = typeof display;

  let raf = 0;

  function targetFrame(): Frame {
    return {
      messages: live.messages_total,
      events: live.events_total,
      msgRate: live.msg_rate ?? 0,
      eventRate: live.event_rate ?? 0
    };
  }

  function lerp(from: number, to: number, eased: number): number {
    return from + (to - from) * eased;
  }

  function animateTo(target: Frame): void {
    cancelAnimationFrame(raf);
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      display = target;
      return;
    }
    const from = { ...display };
    const t0 = performance.now();
    const tick = (now: number) => {
      const p = Math.min(1, (now - t0) / ANIM_MS);
      const eased = 1 - Math.pow(1 - p, 4); // ease-out-quart, as in countUp
      display = {
        messages: lerp(from.messages, target.messages, eased),
        events: lerp(from.events, target.events, eased),
        msgRate: lerp(from.msgRate, target.msgRate, eased),
        eventRate: lerp(from.eventRate, target.eventRate, eased)
      };
      if (p < 1) raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
  }

  async function refresh(): Promise<void> {
    // A backgrounded tab keeps neither an honest rAF nor a reason to poll; the
    // visibility listener catches it up the moment it comes back.
    if (document.hidden) return;
    try {
      const res = await fetch('/stats/data', { headers: { accept: 'application/json' } });
      if (!res.ok) return;
      live = await res.json();
    } catch {
      // Keep the last good snapshot on a network blip rather than blanking.
    }
  }

  onMount(() => {
    display = { messages: 0, events: 0, msgRate: 0, eventRate: 0 };
    animateTo(targetFrame());

    const timer = setInterval(refresh, POLL_MS);
    const onVisible = () => {
      if (!document.hidden) refresh();
    };
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      clearInterval(timer);
      document.removeEventListener('visibilitychange', onVisible);
      cancelAnimationFrame(raf);
    };
  });

  // Re-tween whenever a poll lands a new snapshot. The animation itself reads
  // and writes `display`, so it runs untracked — tracking it would make the
  // effect its own dependency and re-arm on every frame.
  $effect(() => {
    const target = targetFrame();
    untrack(() => animateTo(target));
  });

  const totalFmt = new Intl.NumberFormat(locale, { maximumFractionDigits: 0 });
  const rateFmt = new Intl.NumberFormat(locale, { maximumFractionDigits: 1 });

  const PENDING = '—';

  const tiles = $derived([
    {
      icon: 'send' as IconName,
      tan: false,
      label: t('stats.messagesLabel'),
      value: totalFmt.format(Math.round(display.messages)),
      unit: ''
    },
    {
      icon: 'pulse' as IconName,
      tan: true,
      label: t('stats.eventsLabel'),
      value: totalFmt.format(Math.round(display.events)),
      unit: ''
    },
    {
      icon: 'activity' as IconName,
      tan: false,
      label: t('stats.messageRateLabel'),
      value: live.msg_rate === null ? PENDING : rateFmt.format(display.msgRate),
      unit: live.msg_rate === null ? '' : t('stats.perSecond')
    },
    {
      icon: 'lanes' as IconName,
      tan: true,
      label: t('stats.eventRateLabel'),
      value: live.event_rate === null ? PENDING : rateFmt.format(display.eventRate),
      unit: live.event_rate === null ? '' : t('stats.perSecond')
    }
  ]);
</script>

<svelte:head>
  <title>{t('stats.title')}</title>
  <meta name="description" content={t('stats.metaDescription')} />
  <meta property="og:title" content={t('stats.title')} />
  <meta property="og:description" content={t('stats.metaDescription')} />
  <meta name="twitter:title" content={t('stats.title')} />
  <meta name="twitter:description" content={t('stats.metaDescription')} />
</svelte:head>

<AuroraBg />

<div class="lang-corner"><LangSwitch /></div>

<main class="stats-page">
  <header class="hero">
    <div class="eyebrow reveal" style="--i:0">{t('stats.eyebrow')}</div>
    <h1 class="headline reveal" style="--i:1">{t('stats.headline')}</h1>
    <p class="lede reveal" style="--i:2">{t('stats.tagline')}</p>
  </header>

  {#if live.degraded}
    <div class="notice reveal" style="--i:3">
      <AlertBanner variant="warn" icon="clock">{t('stats.degraded')}</AlertBanner>
    </div>
  {/if}

  <section class="tiles" aria-label={t('stats.headline')}>
    {#each tiles as tile, i (tile.label)}
      <article class="tile reveal" style="--i:{4 + i}">
        <div class="tile-head">
          <span class="ico" class:tan={tile.tan} aria-hidden="true"><Icon name={tile.icon} size={16} /></span>
          <span class="label">{tile.label}</span>
        </div>
        <div class="value">
          <span class="num">{tile.value}</span>{#if tile.unit}<small class="unit">{tile.unit}</small>{/if}
        </div>
      </article>
    {/each}
  </section>

  <footer class="foot reveal" style="--i:8">
    <span class="pip" aria-hidden="true"></span>
    <span>{t('stats.liveNote')}</span>
  </footer>
</main>

<style>
  .lang-corner { position: absolute; top: 18px; right: 18px; z-index: 3; }

  .stats-page {
    position: relative;
    z-index: 1;
    min-height: 100vh;
    max-width: var(--bb-content-max);
    margin: 0 auto;
    padding: clamp(72px, 12vh, 140px) var(--bb-space-5) var(--bb-space-8);
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-6);
  }

  .hero { text-align: center; display: flex; flex-direction: column; align-items: center; gap: var(--bb-space-3); }

  .eyebrow {
    font-family: var(--bb-font-mono);
    font-size: 12px;
    letter-spacing: var(--bb-tracking-eyebrow);
    text-transform: uppercase;
    color: var(--bb-green-glow);
  }

  .headline {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: clamp(32px, 5.4vw, 62px);
    line-height: 1.02;
    letter-spacing: var(--bb-tracking-tight);
    color: var(--bb-white);
    margin: 0;
    max-width: 18ch;
  }

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

  .tile {
    background: var(--bb-card-bg);
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-lg);
    box-shadow: var(--bb-shadow-soft);
    padding: clamp(20px, 3vw, 34px);
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-5);
    min-width: 0;
    transition: border-color var(--bb-dur-base) var(--bb-ease-out-expo);
  }
  .tile:hover { border-color: var(--bb-border-strong); }

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
    font-weight: 700;
    font-size: clamp(38px, 6.4vw, 76px);
    line-height: 1;
    letter-spacing: var(--bb-tracking-tight);
    color: var(--bb-white);
    font-variant-numeric: tabular-nums;
    overflow-wrap: anywhere;
  }

  .unit {
    font-family: var(--bb-font-mono);
    font-size: clamp(13px, 1.4vw, 17px);
    color: var(--bb-muted);
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

  @media (max-width: 720px) {
    .tiles { grid-template-columns: minmax(0, 1fr); }
  }

  @media (prefers-reduced-motion: reduce) {
    .pip { animation: none; }
  }
</style>
