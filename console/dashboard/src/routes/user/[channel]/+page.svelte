<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import { Card } from '@bagel/shared';
  import PublicNav from '$lib/components/public/PublicNav.svelte';
  import PublicFooter from '$lib/components/public/PublicFooter.svelte';

  let { data } = $props();

  const commandLabel = $derived(data.commands.length === 1 ? 'command' : 'commands');
  const moduleLabel = $derived(data.modules.length === 1 ? 'module' : 'modules');
  const creatorCode = $derived(String(data.creatorCode ?? '').trim());

  let fieldEl = $state<HTMLCanvasElement | null>(null);
  let titleEl = $state<HTMLHeadingElement | null>(null);

  // Warm light-field + decode title, ported from the marketing site's
  // PageHero (web/src/script/lightfield.js + decode.js). Self-contained per
  // mount: rAF is gated by an IntersectionObserver and both effects honor
  // reduced-motion, so the header degrades to a static glow + plain text.
  onMount(() => {
    const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    const cleanups: Array<() => void> = [];

    // ── star motes drifting up through the hero ──
    const canvas = fieldEl;
    if (canvas && !reduce) {
      const ctx = canvas.getContext('2d');
      if (ctx) {
        let w = 0, h = 0, raf = 0;
        let dpr = Math.min(window.devicePixelRatio || 1, 2);
        const warmth = 0.7;
        type Mote = { x: number; y: number; r: number; vy: number; vx: number; a: number; warm: number };
        let motes: Mote[] = [];

        const build = () => {
          w = canvas.clientWidth;
          h = canvas.clientHeight;
          if (!w || !h) return;
          canvas.width = Math.round(w * dpr);
          canvas.height = Math.round(h * dpr);
          const count = w < 700 ? 40 : 70;
          motes = Array.from({ length: count }, () => ({
            x: Math.random() * w,
            y: Math.random() * h,
            r: 0.6 + Math.random() * 2,
            vy: -(0.05 + Math.random() * 0.2),
            vx: (Math.random() - 0.5) * 0.1,
            a: 0.12 + Math.random() * 0.45,
            warm: Math.random()
          }));
        };

        const draw = () => {
          if (!w || !h || !motes.length) build();
          if (!w || !h) return;
          ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
          ctx.clearRect(0, 0, w, h);
          ctx.globalCompositeOperation = 'lighter';
          for (const m of motes) {
            m.y += m.vy;
            m.x += m.vx;
            if (m.y < -10) { m.y = h + 10; m.x = Math.random() * w; }
            if (m.x < -10) m.x = w + 10; else if (m.x > w + 10) m.x = -10;
            const col = m.warm < warmth ? '201, 168, 124' : '82, 183, 136';
            ctx.beginPath();
            ctx.arc(m.x, m.y, m.r, 0, Math.PI * 2);
            ctx.fillStyle = `rgba(${col}, ${m.a.toFixed(3)})`;
            ctx.fill();
          }
          ctx.globalCompositeOperation = 'source-over';
        };

        const stop = () => { if (raf) { cancelAnimationFrame(raf); raf = 0; } };
        const loop = () => { draw(); raf = requestAnimationFrame(loop); };

        const io = new IntersectionObserver(([e]) => {
          if (e.isIntersecting && !raf) raf = requestAnimationFrame(loop);
          else if (!e.isIntersecting) stop();
        }, { rootMargin: '150px' });
        io.observe(canvas);

        const onResize = () => { dpr = Math.min(window.devicePixelRatio || 1, 2); build(); };
        window.addEventListener('resize', onResize, { passive: true });
        build();

        cleanups.push(() => { stop(); io.disconnect(); window.removeEventListener('resize', onResize); });
      }
    }

    // ── decode-on-view channel name ──
    const title = titleEl;
    if (title) {
      const text = title.textContent ?? '';
      if (reduce) {
        title.textContent = text;
      } else {
        const SCRAMBLE = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789#$%&*+-/<>';
        const chars = Array.from(text);
        const duration = Math.min(1000, 380 + chars.length * 26);
        let raf = 0;
        const run = () => {
          const start = performance.now();
          const tick = (now: number) => {
            const progress = Math.min(1, (now - start) / duration);
            const revealCount = Math.floor(chars.length * progress);
            const t = Math.floor(progress * 22);
            title.textContent = chars
              .map((char, i) => {
                if (char === ' ' || /\W/.test(char)) return char;
                if (i < revealCount) return char;
                return SCRAMBLE[(i * 19 + t * 7) % SCRAMBLE.length];
              })
              .join('');
            if (progress < 1) raf = requestAnimationFrame(tick);
            else title.textContent = text;
          };
          raf = requestAnimationFrame(tick);
        };
        const io = new IntersectionObserver((entries) => {
          for (const entry of entries) {
            if (!entry.isIntersecting) continue;
            io.unobserve(entry.target);
            run();
          }
        }, { threshold: 0.45 });
        io.observe(title);
        cleanups.push(() => { if (raf) cancelAnimationFrame(raf); io.disconnect(); });
      }
    }

    return () => { for (const c of cleanups) c(); };
  });
</script>

<svelte:head>
  <title>{data.channelName} commands - ItsBagelBot</title>
  <meta
    name="description"
    content={`Active chat commands and modules for ${data.channelName} on ItsBagelBot.`}
  />
</svelte:head>

<!-- Shared public chrome (see lib/components/public): the marketing nav + footer. -->
<PublicNav />

<main class="page">
  <!-- Hero in the marketing PageHero language: warm light-field + hearth glow
       + a decode (scramble → resolve) channel name. -->
  <header class="phero">
    <canvas class="phero__field" bind:this={fieldEl} aria-hidden="true"></canvas>
    <div class="phero__glow" aria-hidden="true"></div>

    <div class="phero__inner">
      <span class="phero__eyebrow">Channel commands</span>
      <h1 class="phero__title" bind:this={titleEl}>{data.channelName}</h1>
      <p class="phero__desc">
        {data.commands.length} active custom {commandLabel}. {data.modules.length} active {moduleLabel}.
      </p>
    </div>
  </header>

  {#if creatorCode}
    <section class="creator-strip" aria-label="Creator code">
      <span class="creator-strip__signal" aria-hidden="true"></span>
      <span class="creator-strip__label">Creator code</span>
      <strong>{creatorCode}</strong>
    </section>
  {/if}

  {#if data.degraded}
    <section class="notice" role="status">
      Command data is temporarily unavailable.
    </section>
  {/if}

  <!-- Custom commands ─────────────────────────────────────────── -->
  <section class="block">
    <div class="block-head">
      <span class="block-eyebrow">Commands</span>
      <h2>Custom commands</h2>
    </div>

    {#if data.commands.length}
      <div class="grid">
        {#each data.commands as command}
          <Card atmosphere hover class="tile">
            <div class="tile-top">
              <h3 class="trigger">{command.trigger}</h3>
              {#if command.uses}
                <span class="tag">{command.uses} uses</span>
              {/if}
            </div>
            <p class="tile-desc">{command.response}</p>
            <ul class="feats">
              {#if command.aliases.length}
                {@render feat(command.aliases.join(', '))}
              {/if}
              {@render feat(command.perm)}
              {#if command.cooldown > 0}
                {@render feat(`${command.cooldown}s cooldown`)}
              {/if}
              {#if command.liveOnly}
                {@render feat('Live only')}
              {/if}
            </ul>
          </Card>
        {/each}
      </div>
    {:else}
      <Card atmosphere class="empty">No active custom commands.</Card>
    {/if}
  </section>

  <!-- Active modules ──────────────────────────────────────────── -->
  <section class="block">
    <div class="block-head">
      <span class="block-eyebrow">Modules</span>
      <h2>Active modules</h2>
    </div>

    {#if data.modules.length}
      <div class="grid">
        {#each data.modules as mod}
          <Card atmosphere hover class="tile">
            <div class="tile-top">
              <div class="tile-title">
                <span class="cat">{mod.category}</span>
                <h3>{mod.label}</h3>
              </div>
              <span class="active-dot" aria-label="Active"></span>
            </div>
            <p class="tile-desc">{mod.tagline}</p>

            {#if mod.commands.length}
              <ul class="feats detail">
                {#each mod.commands as command}
                  {@render detail(command.label, command.meta)}
                {/each}
              </ul>
            {:else if mod.events.length}
              <ul class="feats detail">
                {#each mod.events as event}
                  {@render detail(event.label, event.meta)}
                {/each}
              </ul>
            {:else}
              <span class="status">Active</span>
            {/if}
          </Card>
        {/each}
      </div>
    {:else}
      <Card atmosphere class="empty">No active modules.</Card>
    {/if}
  </section>
</main>

<PublicFooter />

{#snippet check()}
  <svg class="check" width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
    <path d="M3.5 8.5L6.5 11.5L12.5 4.5" stroke="currentColor" stroke-width="1.5"
          stroke-linecap="round" stroke-linejoin="round" />
  </svg>
{/snippet}

{#snippet feat(label: string)}
  <li class="feat">{@render check()}<span>{label}</span></li>
{/snippet}

{#snippet detail(label: string, meta: string)}
  <li class="feat detail-row">
    {@render check()}
    <strong>{label}</strong>
    <span>{meta}</span>
  </li>
{/snippet}


<style>
  h1, h2, h3, p { margin: 0; }

  /* ── page shell ── */
  .page {
    padding: 0 24px 96px;
    color: var(--bb-white);
  }

  /* ── PageHero: light-field + glow + decode title ── */
  .phero {
    position: relative;
    isolation: isolate;
    overflow: hidden;
    max-width: var(--bb-content-max, 1200px);
    margin: 0 auto;
    min-height: clamp(56vh, 68vh, 78vh);
    display: flex;
    align-items: center;
    justify-content: center;
    text-align: center;
    padding: calc(76px + 64px) 24px 72px;
  }
  .phero__field { position: absolute; inset: 0; width: 100%; height: 100%; display: block; z-index: -2; pointer-events: none; }
  .phero__glow {
    position: absolute;
    left: 50%;
    top: 46%;
    width: 110vmin;
    height: 90vmin;
    translate: -50% -50%;
    z-index: -1;
    pointer-events: none;
    background: radial-gradient(55% 60% at 50% 50%, rgba(201,168,124,0.16) 0%, rgba(82,183,136,0.06) 38%, transparent 70%);
    filter: blur(12px);
  }
  .phero__inner { position: relative; z-index: 1; max-width: 720px; display: flex; flex-direction: column; align-items: center; }
  .phero__eyebrow {
    font-family: var(--bb-font-mono);
    font-size: clamp(0.66rem, 1.4vw, 0.76rem);
    letter-spacing: 0.22em;
    text-transform: uppercase;
    color: var(--bb-green-glow);
    margin-bottom: 22px;
    text-shadow: 0 0 16px rgba(82,183,136,0.5);
    opacity: 0;
    animation: pheroIn 700ms 120ms var(--bb-ease-out-expo) forwards;
  }
  .phero__title {
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: clamp(2.4rem, 8vw, 5rem);
    line-height: 1;
    letter-spacing: -0.03em;
    color: var(--bb-white);
    max-width: 16ch;
    text-shadow: 0 0 24px rgba(240,236,228,0.18), 0 3px 18px rgba(0,0,0,0.9), 0 0 40px rgba(201,168,124,0.22);
    opacity: 0;
    animation: pheroIn 760ms 60ms var(--bb-ease-out-expo) forwards;
  }
  .phero__desc {
    font-family: var(--bb-font-body);
    font-size: clamp(1rem, 1.6vw, 1.15rem);
    line-height: 1.7;
    color: var(--bb-muted);
    max-width: 52ch;
    margin-top: 26px;
    opacity: 0;
    animation: pheroIn 800ms 320ms var(--bb-ease-out-expo) forwards;
  }
  @keyframes pheroIn {
    from { opacity: 0; transform: translateY(16px); }
    to { opacity: 1; transform: translateY(0); }
  }
  @media (prefers-reduced-motion: reduce) {
    .phero__eyebrow, .phero__title, .phero__desc { animation: none; opacity: 1; }
    .phero__field { display: none; }
  }

  /* ── content blocks ── */
  .block, .notice, .creator-strip { max-width: var(--bb-content-max, 1200px); margin: 0 auto; }
  .creator-strip {
    position: sticky;
    top: calc(76px + env(safe-area-inset-top, 0px) + 14px);
    z-index: 35;
    width: max-content;
    max-width: min(100%, 560px);
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 14px;
    border: 1px solid rgba(82,183,136,0.34);
    border-radius: 100px;
    background:
      radial-gradient(420px circle at 20% 50%, rgba(201,168,124,0.16), transparent 58%),
      linear-gradient(180deg, rgba(240,236,228,0.07), rgba(240,236,228,0.025)),
      rgba(17,17,16,0.9);
    color: var(--bb-white);
    box-shadow:
      0 18px 42px rgba(0,0,0,0.34),
      0 0 28px rgba(82,183,136,0.08);
    backdrop-filter: blur(12px);
  }
  .creator-strip__signal {
    width: 8px;
    height: 8px;
    flex: none;
    border-radius: 999px;
    background: var(--bb-green-glow);
    box-shadow: 0 0 16px rgba(82,183,136,0.82);
  }
  .creator-strip__label {
    font-family: var(--bb-font-mono);
    font-size: 0.68rem;
    letter-spacing: 0.16em;
    text-transform: uppercase;
    color: var(--bb-green-glow);
    white-space: nowrap;
  }
  .creator-strip strong {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--bb-font-mono);
    font-size: 0.82rem;
    font-weight: 500;
    letter-spacing: 0.08em;
    color: var(--bb-tan-pale);
    text-shadow: 0 0 18px rgba(201,168,124,0.22);
  }
  .notice {
    margin-bottom: 24px;
    padding: 14px 18px;
    border: 1px solid rgba(201,168,124,0.28);
    border-radius: var(--bb-radius-sm, 8px);
    background: rgba(201,168,124,0.08);
    color: var(--bb-tan-light);
    font-family: var(--bb-font-body);
  }
  .block { padding: 40px 0 8px; }
  .block-head { margin-bottom: 22px; }
  .block-eyebrow {
    font-family: var(--bb-font-mono);
    font-size: 0.72rem;
    letter-spacing: 0.18em;
    text-transform: uppercase;
    color: var(--bb-green-glow);
  }
  .block-head h2 {
    margin-top: 8px;
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: clamp(2rem, 4vw, 3.2rem);
    line-height: 1;
    letter-spacing: -0.02em;
  }

  .grid { display: grid; grid-template-columns: 1fr; gap: 24px; }
  @media (min-width: 860px) { .grid { grid-template-columns: repeat(2, 1fr); } }
  .grid :global(.card), :global(.empty) { display: flex; flex-direction: column; }

  .tile-top { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; }
  .tile-title { display: flex; flex-direction: column; gap: 8px; }
  .trigger, .tile-title h3 {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 1.3rem;
    line-height: 1.1;
    letter-spacing: 0.01em;
    color: var(--bb-white);
  }
  .cat, .tag, .status {
    font-family: var(--bb-font-mono);
    font-size: 0.68rem;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--bb-tan-light);
    white-space: nowrap;
  }
  .tile-desc {
    margin-top: 16px;
    font-family: var(--bb-font-body);
    font-size: 0.9rem;
    line-height: 1.65;
    color: var(--bb-muted);
  }
  .active-dot {
    width: 9px; height: 9px;
    margin-top: 5px;
    border-radius: 999px;
    background: var(--bb-green-glow);
    box-shadow: 0 0 14px rgba(82,183,136,0.8);
    flex: none;
  }

  .feats { list-style: none; margin: 22px 0 0; padding: 0; display: flex; flex-direction: column; gap: 11px; }
  .feats.detail { gap: 0; }
  .feat {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    font-family: var(--bb-font-body);
    font-size: 0.87rem;
    line-height: 1.45;
    color: rgba(240,236,228,0.72);
  }
  .feat :global(.check) { flex-shrink: 0; color: var(--bb-green-glow); margin-top: 1px; }
  .detail-row {
    display: grid;
    grid-template-columns: 18px minmax(84px, 0.4fr) minmax(0, 1fr);
    gap: 12px;
    align-items: baseline;
    padding: 12px 0;
    border-top: 1px solid var(--bb-border);
  }
  .detail-row:first-child { border-top: none; }
  .detail-row strong { font-family: var(--bb-font-mono); font-size: 0.85rem; font-weight: 500; color: var(--bb-tan-light); }
  .detail-row > span { color: var(--bb-muted); font-size: 0.88rem; line-height: 1.45; }
  .status {
    align-self: flex-start;
    margin-top: 20px;
    border: 1px solid var(--bb-border);
    border-radius: 999px;
    background: rgba(240,236,228,0.03);
    padding: 7px 12px;
  }
  :global(.empty) { padding: 28px; color: var(--bb-muted); font-family: var(--bb-font-body); }

  @media (max-width: 760px) {
    .page { padding-inline: 18px; }
    .phero { min-height: 60vh; padding-top: calc(76px + 40px); padding-bottom: 54px; }
    .creator-strip {
      width: 100%;
      justify-content: center;
      gap: 9px;
      padding-inline: 12px;
    }
    .creator-strip__label { font-size: 0.62rem; letter-spacing: 0.12em; }
    .creator-strip strong { font-size: 0.75rem; }
    .detail-row { grid-template-columns: 18px 1fr; gap: 4px 12px; }
    .detail-row > span { grid-column: 2; }
  }
</style>
