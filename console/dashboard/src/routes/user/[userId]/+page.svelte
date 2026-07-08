<script lang="ts">
  import { onMount } from 'svelte';
  import { Card } from '@bagel/shared';

  let { data } = $props();

  const commandLabel = $derived(data.commands.length === 1 ? 'command' : 'commands');
  const moduleLabel = $derived(data.modules.length === 1 ? 'module' : 'modules');

  // Links point at the live marketing site so the public page shares one nav +
  // footer with web/ (same buttons, routed to the original web).
  const WEB = 'https://itsbagelbot.com';
  const DASH = 'https://dashboard.itsbagelbot.com';

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

<!-- Shared marketing nav (ported from web/src/components/layout/Nav.astro):
     logo + section links + CTA, all routed to the live site. -->
<nav class="site-nav" aria-label="Primary">
  <div class="site-nav__inner">
    <a class="logo" href={WEB} aria-label="ItsBagelBot home">
      <img src="/logo.png" alt="" width="35" height="35" />
      <span>ItsBagelBot</span>
    </a>

    <ul class="links" aria-label="Primary">
      <li>{@render navlink(`${WEB}/pricing`, 'Pricing')}</li>
      <li>{@render navlink(`${WEB}/contact`, 'Contact')}</li>
    </ul>

    <div class="nav-cta">
      <a class="cta-btn" href={DASH} target="_blank" rel="noopener noreferrer">Add to Twitch</a>
    </div>
  </div>
</nav>

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
          <Card hover class="tile">
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
      <Card class="empty">No active custom commands.</Card>
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
          <Card hover class="tile">
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
      <Card class="empty">No active modules.</Card>
    {/if}
  </section>
</main>

<!-- Shared marketing footer (ported from web/src/components/layout/Footer.astro),
     links routed to the live site. -->
<footer class="site-footer">
  <div class="signoff">
    <p class="signoff__line">Baked late. Served fresh.</p>
    <p class="signoff__sub">See you in chat.</p>
  </div>

  <div class="foot-top">
    <div class="foot-brand">
      <span class="foot-brand__logo"><img src="/logo.png" alt="ItsBagelBot Logo" width="55" height="55" /></span>
      <div>
        <span class="foot-name">ItsBagelBot</span>
        <span class="foot-tagline">Your stream. Your rules.</span>
      </div>
    </div>

    <div class="foot-cols">
      <div class="foot-col">
        <p>Product</p>
        <a href={`${WEB}/pricing`}>Pricing</a>
        <a href="https://docs.itsbagelbot.com" target="_blank" rel="noopener noreferrer">Developer</a>
        <a href={DASH} target="_blank" rel="noopener noreferrer">Dashboard</a>
      </div>
      <div class="foot-col">
        <p>Company</p>
        <a href={`${WEB}/contact`}>Contact</a>
        <a href="https://github.com/AdamOusmer/ItsBagelBot" target="_blank" rel="noopener noreferrer">GitHub</a>
      </div>
      <div class="foot-col">
        <p>Community</p>
        <a href="https://discord.gg/SZ2remwSDv" target="_blank" rel="noopener noreferrer">Discord</a>
        <a href="https://twitch.tv/itsbagelbot" target="_blank" rel="noopener noreferrer">Twitch</a>
      </div>
    </div>
  </div>

  <div class="foot-bottom">
    <span class="foot-copy">&copy; 2026 ItsBagelBot</span>
    <span class="foot-note">No data sold · No trackers · No surprises</span>
    <div class="foot-legal">
      <a href={`${WEB}/privacy`}>Privacy Policy</a>
      <a href={`${WEB}/terms`}>Terms of Service</a>
    </div>
  </div>
</footer>

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

<!-- NavLink motion glyph, ported from web/src/components/layout/NavLink.astro -->
{#snippet navlink(href: string, label: string)}
  <a class="nav-link-motion" {href} aria-label={label}>
    <span class="nav-link-motion__mask" aria-hidden="true">
      <span class="nav-link-motion__line nav-link-motion__line--rest">
        {#each Array.from(label) as glyph, i}
          <span class="nav-link-motion__glyph" style={`--glyph-index: ${i};`}>{glyph}</span>
        {/each}
      </span>
      <span class="nav-link-motion__line nav-link-motion__line--active">
        {#each Array.from(label) as glyph, i}
          <span class="nav-link-motion__glyph" style={`--glyph-index: ${i};`}>{glyph}</span>
        {/each}
      </span>
    </span>
  </a>
{/snippet}

<style>
  h1, h2, h3, p { margin: 0; }

  /* ── shared marketing nav ── */
  .site-nav {
    font-family: var(--bb-font-display);
    position: fixed;
    top: 0;
    left: 0;
    width: 100%;
    height: calc(76px + env(safe-area-inset-top, 0px));
    padding-top: env(safe-area-inset-top, 0px);
    padding-inline: max(24px, 4vw);
    border-bottom: 1px solid var(--bb-border);
    z-index: 50;
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    background: rgba(10, 10, 10, 0.7);
  }
  .site-nav__inner {
    display: grid;
    grid-template-columns: minmax(170px, 1fr) minmax(0, auto) minmax(170px, 1fr);
    align-items: center;
    gap: 24px;
    width: min(100%, 1180px);
    height: 100%;
    margin: 0 auto;
  }
  .logo {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
    width: max-content;
    color: var(--bb-white);
    text-decoration: none;
  }
  .logo img { width: 35px; height: 35px; border-radius: 8px; }
  .logo span {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 1.2rem;
    color: var(--bb-white);
    white-space: nowrap;
  }
  ul.links {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 30px;
    min-width: 0;
    padding: 0 1.5rem;
    list-style: none;
    margin: 0;
  }
  ul.links li { display: flex; align-items: center; }
  .nav-cta { display: flex; justify-content: flex-end; align-items: center; gap: 14px; }

  /* CTA — ported from SecondaryButton.astro */
  .cta-btn {
    font-family: var(--bb-font-mono);
    font-size: 0.78rem;
    padding: 10px 22px;
    background: transparent;
    border: 1px solid var(--bb-tan);
    color: var(--bb-tan-light);
    border-radius: 4px;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    text-decoration: none;
    white-space: nowrap;
    display: inline-block;
    transition: background 0.2s, color 0.2s;
  }
  .cta-btn:hover { background: var(--bb-tan); color: var(--bb-black); }

  /* NavLink motion — ported from NavLink.astro */
  .nav-link-motion {
    --nav-link-shift: 1.22em;
    position: relative;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-height: 32px;
    padding: 0.42rem 0.08rem;
    color: var(--bb-muted);
    font-family: var(--bb-font-mono);
    font-size: 0.78rem;
    font-weight: 500;
    line-height: 1;
    text-decoration: none;
    text-transform: uppercase;
    white-space: nowrap;
    outline: none;
    isolation: isolate;
  }
  .nav-link-motion::before {
    content: "";
    position: absolute;
    left: 0; right: 0;
    bottom: 0.28rem;
    height: 1px;
    pointer-events: none;
    background: linear-gradient(90deg, transparent, rgba(224,196,154,0.15), rgba(224,196,154,0.78), rgba(82,183,136,0.54), transparent);
    opacity: 0;
    transform: scaleX(0.18);
    transform-origin: center;
  }
  .nav-link-motion__mask {
    position: relative;
    display: block;
    height: 1.12em;
    overflow: hidden;
    text-shadow: 0 3px 12px rgba(0,0,0,0.9), 0 0 18px rgba(224,196,154,0.18);
  }
  .nav-link-motion__line { display: inline-flex; align-items: baseline; color: inherit; }
  .nav-link-motion__line--active {
    position: absolute;
    inset: 0 auto auto 0;
    color: var(--bb-white);
    text-shadow: 0 3px 12px rgba(0,0,0,0.9), 0 0 18px rgba(224,196,154,0.34), 0 0 26px rgba(82,183,136,0.18);
  }
  .nav-link-motion__glyph { display: inline-block; transform: translateY(0); }
  .nav-link-motion__line--active .nav-link-motion__glyph {
    opacity: 0.4;
    transform: translateY(var(--nav-link-shift)) rotate(3deg);
  }
  .nav-link-motion:focus-visible { outline: 1px solid rgba(224,196,154,0.58); outline-offset: 6px; }

  @media (min-width: 1024px) and (hover: hover) and (pointer: fine) {
    .nav-link-motion { transition: color 180ms ease; }
    .nav-link-motion::before {
      transition: opacity 260ms ease, transform 620ms var(--bb-ease-out-expo);
    }
    .nav-link-motion__glyph {
      transition: opacity 280ms ease, transform 420ms var(--bb-ease-out-expo);
      transition-delay: calc(var(--glyph-index) * 16ms);
      will-change: transform, opacity;
    }
    .nav-link-motion:is(:hover, :focus-visible) { color: var(--bb-white); }
    .nav-link-motion:is(:hover, :focus-visible)::before { opacity: 1; transform: scaleX(1); }
    .nav-link-motion:is(:hover, :focus-visible) .nav-link-motion__line--rest .nav-link-motion__glyph {
      opacity: 0.32;
      transform: translateY(calc(var(--nav-link-shift) * -1)) rotate(-3deg);
    }
    .nav-link-motion:is(:hover, :focus-visible) .nav-link-motion__line--active .nav-link-motion__glyph {
      opacity: 1;
      transform: translateY(0) rotate(0deg);
      transition-delay: calc(72ms + (var(--glyph-index) * 18ms));
    }
  }

  @media (max-width: 1120px) {
    .site-nav__inner { grid-template-columns: minmax(155px, 1fr) minmax(0, auto) minmax(150px, 1fr); gap: 18px; }
    ul.links { gap: 20px; padding-inline: 1rem; }
    .logo span { font-size: 1.08rem; }
  }
  @media (max-width: 900px) {
    .site-nav__inner { grid-template-columns: 1fr auto; gap: 16px; }
    ul.links { display: none; }
  }

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
  .block, .notice { max-width: var(--bb-content-max, 1200px); margin: 0 auto; }
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

  /* ── shared marketing footer ── */
  .site-footer {
    position: relative;
    border-top: 1px solid var(--bb-border);
    padding: 72px 4% 40px;
    overflow: hidden;
    color: var(--bb-white);
  }
  .site-footer::before {
    content: "";
    position: absolute;
    left: 50%;
    top: 0;
    width: 90vmin;
    height: 40vmin;
    translate: -50% -50%;
    pointer-events: none;
    background: radial-gradient(50% 60% at 50% 50%, rgba(201,168,124,0.09) 0%, transparent 70%);
    filter: blur(12px);
  }
  .signoff { position: relative; text-align: center; margin-bottom: 72px; }
  .signoff__line {
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: clamp(1.7rem, 4.6vw, 2.9rem);
    letter-spacing: -0.02em;
    line-height: 1.08;
    color: var(--bb-white);
    margin: 0 0 10px;
    text-shadow: 0 0 30px rgba(201,168,124,0.16);
  }
  .signoff__sub {
    font-family: var(--bb-font-hand, "Caveat", cursive);
    font-size: clamp(1.2rem, 2.6vw, 1.5rem);
    font-weight: 500;
    color: var(--bb-tan-light);
    margin: 0;
    rotate: -1deg;
  }
  .foot-top {
    position: relative;
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 60px;
    margin-bottom: 60px;
  }
  .foot-brand { display: flex; align-items: center; gap: 15px; }
  .foot-brand__logo {
    display: inline-block;
    line-height: 0;
    transition: rotate 500ms cubic-bezier(0.34, 1.56, 0.64, 1);
  }
  .foot-brand__logo img { width: 55px; height: 55px; border-radius: 10px; }
  @media (hover: hover) and (pointer: fine) {
    .foot-brand:hover .foot-brand__logo { rotate: 14deg; }
  }
  .foot-brand div { display: flex; flex-direction: column; }
  .foot-name {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 1.1rem;
    color: var(--bb-white);
    margin-bottom: 10px;
  }
  .foot-tagline { font-family: var(--bb-font-mono); font-size: 0.9rem; color: var(--bb-muted); letter-spacing: 0.05em; }
  .foot-cols { display: flex; gap: 60px; }
  .foot-col { display: flex; flex-direction: column; gap: 15px; }
  .foot-col p {
    font-family: var(--bb-font-mono);
    font-size: 0.9rem;
    letter-spacing: 0.10em;
    text-transform: uppercase;
    color: var(--bb-white);
    margin: 0 0 4px 0;
  }
  .foot-col a {
    position: relative;
    width: fit-content;
    font-family: var(--bb-font-body);
    font-size: 0.85rem;
    color: var(--bb-muted);
    text-decoration: none;
    transition: color 0.2s var(--bb-ease-out-expo), transform 0.3s var(--bb-ease-out-expo);
  }
  .foot-col a::before {
    content: "";
    position: absolute;
    left: -12px;
    top: 50%;
    width: 4px;
    height: 4px;
    border-radius: 50%;
    translate: 0 -50%;
    background: var(--bb-tan);
    opacity: 0;
    scale: 0;
    transition: opacity 0.25s ease, scale 0.3s cubic-bezier(0.34, 1.56, 0.64, 1);
  }
  .foot-col a:hover { color: var(--bb-tan-light); transform: translateX(6px); }
  .foot-col a:hover::before { opacity: 1; scale: 1; }
  @media (prefers-reduced-motion: reduce) {
    .foot-col a:hover { transform: none; }
    .foot-col a::before { display: none; }
    .foot-brand__logo { transition: none; }
  }
  .foot-bottom {
    display: grid;
    grid-template-columns: 1fr auto 1fr;
    align-items: center;
    padding-top: 30px;
    border-top: 1px solid rgba(255,255,255,0.05);
  }
  .foot-copy, .foot-note {
    font-family: var(--bb-font-mono);
    font-size: 0.7rem;
    color: var(--bb-muted);
    letter-spacing: 0.06em;
  }
  .foot-note { letter-spacing: 0.08em; }
  .foot-legal { display: flex; justify-content: flex-end; gap: 20px; }
  .foot-legal a {
    font-family: var(--bb-font-mono);
    font-size: 0.7rem;
    color: var(--bb-muted);
    text-decoration: none;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    transition: color 0.2s;
  }
  .foot-legal a:hover { color: var(--bb-tan-light); }

  @media (max-width: 900px) {
    .site-footer { padding: 56px 25px 30px; }
    .signoff { margin-bottom: 56px; }
    .foot-top { flex-direction: column; gap: 40px; }
    .foot-cols { flex-wrap: wrap; gap: 30px; }
    .foot-bottom { grid-template-columns: 1fr; gap: 10px; text-align: center; }
    .foot-legal { justify-content: center; }
  }

  @media (max-width: 760px) {
    .page { padding-inline: 18px; }
    .phero { min-height: 60vh; padding-top: calc(76px + 40px); padding-bottom: 54px; }
    .detail-row { grid-template-columns: 18px 1fr; gap: 4px 12px; }
    .detail-row > span { grid-column: 2; }
  }
</style>
