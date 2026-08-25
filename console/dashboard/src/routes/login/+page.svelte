<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import { page } from '$app/state';
  import { LightField, Icon, getI18n } from '@bagel/shared';
  import PublicNav from '$lib/components/public/PublicNav.svelte';
  import { webHref } from '$lib/components/public/links';

  const { t, locale } = getI18n();

  // Same row the signed-out layout ships: marketing destinations + Stats.
  const navLinks = $derived([
    { href: webHref('/pricing', locale), label: t('public.nav.pricing') },
    { href: webHref('/guides', locale), label: t('public.nav.guides') },
    { href: webHref('/contact', locale), label: t('public.nav.contact') },
    { href: 'https://stats.itsbagelbot.com/', label: t('public.nav.stats') }
  ]);

  const DISCORD = 'https://discord.gg/SZ2remwSDv';
  const SITE = 'https://itsbagelbot.com';

  const lines = $derived([
    { text: t('login.title1'), cls: 'tan' },
    { text: t('login.title2'), cls: '' },
    { text: t('login.title3'), cls: 'green' }
  ]);
  const facts = $derived([
    t('login.featUptime'),
    t('login.featEncrypted'),
    t('login.featEdge'),
    t('login.featSource')
  ]);
  const titleLabel = $derived(lines.map((l) => l.text).join(' '));

  const NOTICES: Record<string, string> = {
    signedout: t('login.noticeSignedout'),
    revoked: t('login.noticeRevoked'),
    banned: t('login.noticeBanned'),
    link: t('login.noticeLink'),
    retry: t('login.noticeRetry'),
    imp: t('login.noticeImpersonation')
  };
  const notice = $derived(NOTICES[page.url.searchParams.get('e') ?? ''] ?? null);

  // Post-login destination (validated server-side in /auth/login); rides the
  // OAuth round trip so a deep link like /billing?subscribe=1 survives sign-in.
  const next = $derived(page.url.searchParams.get('next'));
  const loginHref = $derived(next ? `/auth/login?next=${encodeURIComponent(next)}` : '/auth/login');

  function glyphs(text: string): string[] {
    return Array.from(text, (g) => (g === ' ' ? '\u00a0' : g));
  }

  // Same intro as web/src/components/home/Header.astro: each line's glyphs roll
  // up out of a per-line mask; the trailing period pops last with a glow.
  //
  // CSS hides the glyphs pre-intro (see the h1 .glyph media query), so every
  // branch that will NOT animate has to reveal them by hand. The Astro page
  // needs no such escape hatch: its intro is a blocking inline module, while
  // this one waits on onMount plus a dynamic import, and a hidden tab (the
  // Cursor Browser pane reports document.hidden even when fronted) freezes
  // WAAPI, so an unrevealed glyph would sit at opacity 0 forever.
  function revealGlyphs(h1: HTMLElement): void {
    h1.querySelectorAll<HTMLElement>('[data-hero-glyph]').forEach((g) => {
      g.style.opacity = '1';
      g.style.transform = 'none';
      g.style.filter = 'none';
    });
  }

  onMount(() => {
    const h1 = document.querySelector<HTMLElement>('[data-hero-title]');
    if (!h1 || h1.dataset.heroMotionReady === 'true') return;
    h1.dataset.heroMotionReady = 'true';
    // Reduced motion never hides them in the first place.
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    if (document.hidden) {
      revealGlyphs(h1);
      return;
    }

    // Chunk stalled or blocked: show the headline rather than an empty hero.
    let revealed = false;
    const watchdog = window.setTimeout(() => {
      revealed = true;
      revealGlyphs(h1);
    }, 1800);

    // motion touches the DOM; load it only on the client so SSR of /login
    // does not pull the animation runtime into the server module graph.
    void import('motion').then(({ animate, stagger, cubicBezier }) => {
      window.clearTimeout(watchdog);
      // A chunk that arrives after the watchdog fired must not animate: every
      // track starts at opacity 0, so the headline the watchdog just revealed
      // would blink out and roll in a second time.
      if (revealed) return;

      const LINE_DELAY_BASE = 0.35;
      const LINE_DELAY_STEP = 0.16;
      const GLYPH_STAGGER = 0.024;
      const easeOutQuint = cubicBezier(0.22, 1, 0.36, 1);
      const easeBackOut = cubicBezier(0.34, 1.56, 0.64, 1);

      h1.querySelectorAll<HTMLElement>('[data-hero-line]').forEach((line, lineIndex) => {
        const all = Array.from(line.querySelectorAll<HTMLElement>('[data-hero-glyph]'));
        if (!all.length) return;
        const lineDelay = LINE_DELAY_BASE + lineIndex * LINE_DELAY_STEP;
        const period = all[all.length - 1];
        const letters = all.slice(0, -1);

        animate(
          letters,
          {
            transform: ['translateY(118%) rotate(6deg)', 'translateY(0%) rotate(0deg)'],
            opacity: [0, 1],
            filter: ['blur(6px)', 'blur(0px)']
          },
          {
            delay: stagger(GLYPH_STAGGER, { startDelay: lineDelay }),
            duration: 0.8,
            ease: easeOutQuint
          }
        );

        const periodDelay = lineDelay + letters.length * GLYPH_STAGGER + 0.18;
        animate(
          period,
          {
            transform: ['translateY(0%) scale(0)', 'translateY(0%) scale(1)'],
            opacity: [0, 1]
          },
          { delay: periodDelay, duration: 0.55, ease: easeBackOut }
        );
        animate(
          period,
          {
            textShadow: [
              '0 0 0px rgba(245, 230, 207, 0)',
              '0 0 28px rgba(245, 230, 207, 0.85)',
              '0 0 0px rgba(245, 230, 207, 0)'
            ]
          },
          { delay: periodDelay, duration: 0.9, ease: 'easeOut' }
        );
      });
    }).catch(() => {
      window.clearTimeout(watchdog);
      revealGlyphs(h1);
    });
  });
</script>

<div class="starfield" aria-hidden="true"><LightField /></div>

<PublicNav links={navLinks} showLang />

<header>
  <div class="bg" aria-hidden="true">
    <div class="bg-ring">
      <svg viewBox="0 0 800 800" fill="none" xmlns="http://www.w3.org/2000/svg">
        <path
          d="M400 400 Q 550 200 650 400 Q 750 600 550 700 Q 350 800 200 650 Q 50 500 150 300 Q 250 100 450 150 Q 650 200 700 400 Q 750 600 600 720 Q 450 840 280 760 Q 110 680 80 500 Q 50 320 180 200 Q 310 80 480 120"
          stroke="url(#login-g1)"
          stroke-width="2.5"
          fill="none"
        />
        <path
          d="M400 400 Q 240 220 150 400 Q 60 580 240 680 Q 420 780 560 660 Q 700 540 680 360 Q 660 180 480 140 Q 300 100 180 240 Q 60 380 120 560 Q 180 740 360 780"
          stroke="url(#login-g2)"
          stroke-width="1.5"
          fill="none"
        />
        <defs>
          <linearGradient id="login-g1" x1="0" y1="0" x2="800" y2="800" gradientUnits="userSpaceOnUse">
            <stop stop-color="#c9a87c" offset="0" />
            <stop offset="1" stop-color="#2d6a4f" />
          </linearGradient>
          <linearGradient id="login-g2" x1="800" y1="0" x2="0" y2="800" gradientUnits="userSpaceOnUse">
            <stop stop-color="#40916c" offset="0" />
            <stop offset="1" stop-color="#c9a87c" />
          </linearGradient>
        </defs>
      </svg>
    </div>
    <div class="orb orb-1"></div>
    <div class="orb orb-2"></div>
  </div>
  <div class="header-material">
    {#if notice}
      <div class="notice reveal" style="--d:0s" role="alert">
        <Icon name="ban" size={14} />
        {notice}
      </div>
    {/if}

    <a class="eyebrow" href={DISCORD} target="_blank" rel="noopener noreferrer">
      <span class="eyebrow__badge">
        <span class="eyebrow__dot"></span>{t('login.badge')}
      </span>
      <span class="eyebrow__text">{t('login.topText')}</span>
    </a>

    <div class="split">
      <h1 aria-label={titleLabel} data-hero-title>
        {#each lines as line (line.text)}
          <span class="line {line.cls}" aria-hidden="true" data-hero-line>
            {#each glyphs(line.text) as glyph, i (i)}
              <span class="glyph" data-hero-glyph>{glyph}</span>
            {/each}
          </span>
        {/each}
      </h1>

      <div class="aside">
        <p class="lede">{t('login.lede')}</p>

        <div class="facts">
          {#each facts as fact (fact)}
            <span>{fact}</span>
          {/each}
        </div>

        <a class="cta" href={loginHref} data-sveltekit-reload>
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <path d="M4 3h16v12l-4 4h-4l-3 3v-3H4z" />
            <line x1="9" y1="8" x2="9" y2="12" />
            <line x1="14" y1="8" x2="14" y2="12" />
          </svg>
          {t('login.cta')}
        </a>

        <p class="consent">{@html t('login.consent')}</p>

        <a class="migrate" href={SITE}>
          {t('login.back')}<span class="migrate__arrow" aria-hidden="true">→</span>
        </a>
      </div>
    </div>
  </div>
</header>

<style>
  .starfield {
    position: fixed;
    inset: 0;
    z-index: 0;
    pointer-events: none;
  }

  header {
    position: relative;
    z-index: 1;
    min-height: 100vh;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    text-align: center;
    padding: max(96px, 12vh) 50px 48px;
    overflow: hidden;
  }

  .header-material {
    display: flex;
    flex-direction: column;
    align-items: center;
    width: min(100%, 1200px);
  }

  .split {
    display: contents;
  }

  .bg {
    position: absolute;
    inset: 0;
    pointer-events: none;
    overflow: hidden;
  }

  /* Login paints its own orbs; the shell's ambient pair would muddy them. The
     orbs live in app.html, so hiding them takes a :global rule — but route CSS
     stays in the document after a client-side navigation, and an unqualified
     :global(.bg-orb) kept them hidden on every page visited afterwards (reach
     /login from the error page's sign-in link, then go Back). Gating on the
     hero's presence unscopes the rule the moment this page unmounts, and
     unlike a body class toggled from onMount it also holds during SSR, so the
     shell orbs never flash in before hydration. */
  :global(body:has([data-hero-title]) .bg-orb) {
    display: none;
  }

  .bg-ring {
    position: absolute;
    top: 50%;
    right: -180px;
    width: 760px;
    height: 760px;
    opacity: 0.1;
    transform: translateY(-50%);
  }

  .bg-ring svg {
    width: 100%;
    height: 100%;
    animation: slowspin 40s linear infinite;
  }

  .orb {
    position: absolute;
    border-radius: 50%;
    filter: blur(80px);
    opacity: 0.16;
  }

  .orb::before {
    content: '';
    position: absolute;
    inset: 0;
    border-radius: inherit;
    background: inherit;
    animation: pulse 5s ease-in-out infinite;
  }

  .orb-1 {
    width: 500px;
    height: 500px;
    background: var(--bb-green);
    top: -120px;
    right: -60px;
  }

  .orb-2 {
    width: 400px;
    height: 400px;
    background: var(--bb-tan);
    bottom: -100px;
    left: -80px;
  }

  .orb-2::before {
    animation-delay: 400ms;
  }

  @keyframes slowspin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }

  @keyframes pulse {
    0%, 100% { opacity: 0.55; transform: scale(1); }
    50% { opacity: 1; transform: scale(1.1); }
  }

  .notice {
    display: inline-flex;
    align-items: center;
    gap: 9px;
    padding: 11px 18px;
    margin-bottom: 20px;
    border-radius: 8px;
    background: rgba(176, 90, 70, 0.1);
    border: 1px solid rgba(176, 90, 70, 0.4);
    color: #cf8a78;
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    max-width: 60ch;
    text-align: left;
  }
  .notice :global(svg) {
    stroke: currentColor;
    fill: none;
    stroke-width: 1.8;
    flex: none;
  }

  .eyebrow {
    display: inline-flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 24px;
    text-decoration: none;
    opacity: 0;
    animation: fadeUp 800ms 200ms var(--bb-ease-out-expo) forwards;
  }

  .eyebrow__badge {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    flex-shrink: 0;
    padding: 4px 11px;
    border-radius: 20px;
    background: rgba(45, 106, 79, 0.15);
    border: 1px solid rgba(64, 145, 108, 0.3);
    font-family: var(--bb-font-mono);
    font-size: 0.7rem;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--bb-green-light);
    transition: border-color 0.2s;
  }

  .eyebrow__dot {
    width: 7px;
    height: 7px;
    flex-shrink: 0;
    border-radius: 50%;
    background: var(--bb-green-glow);
    animation: pulse-dot 1.8s ease-in-out infinite;
  }

  .eyebrow__text {
    font-family: var(--bb-font-mono);
    font-size: 0.9rem;
    letter-spacing: 0.2em;
    text-transform: uppercase;
    color: var(--bb-green-glow);
    transition: color 0.2s;
  }

  .eyebrow:hover .eyebrow__badge {
    border-color: rgba(64, 145, 108, 0.7);
  }
  .eyebrow:hover .eyebrow__text {
    color: var(--bb-white);
  }

  /* margin-top:0 stands in for web's `* { margin: 0 }` reset (style.css), which
     console/shared/styles/app.css does not ship. Without it the UA's
     `h1 { margin-block-start: 0.67em }` survives — 58.85px at the 87.84px
     desktop size — and grew the grid row past the title, so `align-items: end`
     bottom-aligned the title 59px below its eyebrow. Same reset gap as .lede
     and .consent below. */
  h1 {
    font-family: var(--bb-font-display);
    font-size: clamp(3rem, min(9vw, 15vh), 8rem);
    font-weight: 800;
    line-height: 0.9;
    letter-spacing: 0;
    margin-top: 0;
    margin-bottom: 24px;
    color: var(--bb-white);
  }

  h1 .line {
    display: block;
    white-space: nowrap;
    overflow: hidden;
    padding-block: 0.12em;
    margin-block: -0.12em;
  }

  h1 .line.tan { color: var(--bb-tan); }
  h1 .line.green { color: var(--bb-green-glow); }

  h1 .glyph {
    display: inline-block;
    transform-origin: 20% 80%;
  }

  /* Keep this selector statically matchable: an attribute the mount adds at
     runtime (h1[data-hero-animate]) compiles away — Svelte prunes selectors it
     cannot match in the markup and warns css_unused_selector, which left the
     glyphs visible and killed the roll-in. onMount reveals them instead. */
  @media (scripting: enabled) and (prefers-reduced-motion: no-preference) {
    h1 .glyph {
      opacity: 0;
      transform: translateY(118%) rotate(6deg);
    }
  }

  /* margin-top:0 is load-bearing, not tidiness. web/src/styles/style.css
     resets `* { margin: 0 }`, so the hero's `p` rule only ever sets a bottom
     margin; console/shared/styles/app.css has no such reset, so the UA's
     `p { margin: 1em 0 }` survived here and added a measured 15.68px above
     the lede that the hero does not have. Same story for .consent below. */
  .lede {
    max-width: 550px;
    font-size: 1.05rem;
    line-height: 1.75;
    color: #b0a898;
    margin-top: 0;
    margin-bottom: 24px;
    opacity: 0;
    animation: fadeUp 0.9s 0.5s var(--bb-ease-out-expo) forwards;
  }

  .facts {
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    align-items: center;
    margin-top: 26px;
    font-family: var(--bb-font-mono);
    font-size: 0.68rem;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: rgba(201, 168, 124, 0.66);
    opacity: 0;
    animation: fadeUp 0.9s 0.65s var(--bb-ease-out-expo) forwards;
  }

  .facts span:not(:last-child)::after {
    content: '·';
    margin: 0 16px;
    color: rgba(201, 168, 124, 0.4);
  }

  .cta {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    margin-top: 18px;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 0.95rem;
    letter-spacing: -0.01em;
    color: #0a0a0a;
    background: var(--bb-tan);
    padding: 16px 36px;
    border-radius: 4px;
    text-decoration: none;
    opacity: 0;
    animation: fadeUp 0.9s 0.8s var(--bb-ease-out-expo) forwards;
    transition: background 0.2s, transform 0.2s, box-shadow 0.2s;
  }
  .cta svg {
    width: 17px;
    height: 17px;
    stroke: currentColor;
    fill: none;
    stroke-width: 1.8;
    stroke-linejoin: round;
  }
  .cta:hover {
    background: var(--bb-tan-light);
    transform: translateY(-2px);
    box-shadow: 0 8px 32px rgba(201, 168, 124, 0.25);
  }

  .consent {
    margin-top: 16px;
    margin-bottom: 0;
    margin-inline: auto;
    font-family: var(--bb-font-body);
    font-size: 0.78rem;
    line-height: 1.5;
    color: var(--bb-muted);
    max-width: 42ch;
    text-align: center;
    opacity: 0;
    animation: fadeUp 0.9s 0.92s var(--bb-ease-out-expo) forwards;
  }
  .consent :global(a) {
    color: var(--bb-tan-light);
    text-decoration: none;
  }
  .consent :global(a:hover) {
    color: var(--bb-white);
  }

  .migrate {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    margin-top: 18px;
    font-size: 0.85rem;
    color: var(--bb-green-light);
    text-decoration: none;
    opacity: 0;
    animation: fadeUp 0.9s 1.05s var(--bb-ease-out-expo) forwards;
    transition: color 0.2s;
  }
  .migrate__arrow {
    transition: transform 0.25s var(--bb-ease-out-expo);
  }
  .migrate:hover { color: var(--bb-white); }
  .migrate:hover .migrate__arrow { transform: translateX(4px); }

  .reveal {
    opacity: 0;
    animation: fadeUp 0.8s var(--bb-ease-out-expo) both;
    animation-delay: var(--d, 0s);
  }

  @keyframes pulse-dot {
    0%, 100% { opacity: 1; transform: scale(1); }
    50% { opacity: 0.3; transform: scale(0.75); }
  }

  @keyframes fadeUp {
    from { opacity: 0; transform: translateY(24px); }
    to { opacity: 1; transform: translateY(0); }
  }

  @media (max-height: 820px) {
    h1 { margin-bottom: 16px; }
    .facts { margin-top: 16px; }
    .cta { margin-top: 12px; }
    .migrate { margin-top: 12px; }
  }

  @media (max-width: 900px) {
    header { padding: clamp(72px, 10vh, 92px) 24px 28px; }
    .header-material { width: min(100%, 680px); }

    .bg-ring {
      top: 53%;
      left: 50%;
      right: auto;
      width: clamp(440px, 128vw, 620px);
      height: clamp(440px, 128vw, 620px);
      opacity: 0.08;
      transform: translate(-50%, -50%);
    }

    h1 {
      font-size: clamp(1.9rem, min(9vw, 8.4vh), 4.2rem);
      line-height: 0.96;
      margin-bottom: 18px;
    }

    .eyebrow {
      flex-direction: column;
      gap: 8px;
      margin-bottom: 14px;
    }
    .eyebrow__text {
      text-align: center;
      font-size: 0.78rem;
      letter-spacing: 0.16em;
    }
    .eyebrow__badge {
      padding: 3px 9px;
      font-size: 0.62rem;
    }

    .lede {
      max-width: min(520px, 100%);
      font-size: 0.98rem;
      line-height: 1.58;
      margin-bottom: 18px;
    }

    .facts {
      margin-top: 18px;
      font-size: 0.6rem;
      letter-spacing: 0.11em;
    }
    .facts span:not(:last-child)::after { margin: 0 11px; }

    .cta {
      margin-top: 14px;
      padding: 14px 28px;
      font-size: 0.9rem;
    }
    .migrate { margin-top: 14px; font-size: 0.8rem; }
  }

  @media (max-width: 480px) {
    header { padding: clamp(68px, 10vh, 84px) 18px 20px; }

    .bg-ring {
      top: 50%;
      width: clamp(380px, 118vw, 500px);
      height: clamp(380px, 118vw, 500px);
    }

    .eyebrow { gap: 8px; margin-bottom: 12px; }
    .eyebrow__text { font-size: 0.68rem; letter-spacing: 0.14em; }
    .eyebrow__dot { width: 6px; height: 6px; }

    h1 {
      font-size: clamp(1.75rem, min(8.4vw, 7.6vh), 2.25rem);
      margin-bottom: 16px;
    }
    .lede { font-size: 0.92rem; line-height: 1.52; margin-bottom: 16px; }
    .facts { margin-top: 14px; font-size: 0.56rem; }
    .facts span:not(:last-child)::after { margin: 0 8px; }
    .cta { padding: 14px 24px; font-size: 0.85rem; }
    .migrate { margin-top: 12px; font-size: 0.76rem; }
  }

  /* ── Editorial left — desktop only. Keep this block last.
     Copied from web/src/components/home/Header.astro: `auto 1fr` + 56px gap,
     no divider. A max-content/hairline grid was tried here and it is what
     painted the vertical rule the hero never has. */
  @media (min-width: 901px) {
    .header-material {
      align-items: flex-start;
      text-align: left;
      width: 100%;
    }

    .split {
      display: grid;
      grid-template-columns: auto 1fr;
      gap: 56px;
      align-items: end;
      width: 100%;
    }

    .split h1 {
      margin-bottom: 0;
      font-size: clamp(3rem, min(6.1vw, 13vh), 5.5rem);
    }

    .aside {
      display: flex;
      flex-direction: column;
      align-items: flex-start;
      padding-bottom: 10px;
    }

    .lede {
      max-width: 380px;
      margin-bottom: 24px;
    }

    .eyebrow {
      align-self: flex-start;
      margin-bottom: 36px;
    }

    .facts {
      justify-content: flex-start;
      margin-top: 0;
      text-align: left;
    }

    .consent {
      text-align: left;
      margin-inline: 0;
    }

    .cta { margin-top: 16px; }
    .migrate { margin-top: 16px; }
  }

  @media (prefers-reduced-motion: reduce) {
    .reveal, .eyebrow, .lede, .facts, .cta, .consent, .migrate {
      animation: none;
      opacity: 1;
    }
    .bg-ring svg, .orb::before, .eyebrow__dot { animation: none; }
  }
</style>
