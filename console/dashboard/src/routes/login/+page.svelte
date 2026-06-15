<script lang="ts">
  import { onMount } from 'svelte';
  import { AuroraBg } from '@bagel/shared';

  let stage: HTMLElement;
  const headline = ['Your', 'stream.', 'Your', 'tools.', 'Your', 'rules.'];

  onMount(async () => {
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      stage.querySelectorAll<HTMLElement>('[data-reveal]').forEach((el) => (el.style.opacity = '1'));
      return;
    }
    const { animate, stagger } = await import('motion');

    animate(
      stage.querySelectorAll('.brand-pop'),
      { opacity: [0, 1], scale: [0.6, 1], filter: ['blur(14px)', 'blur(0px)'] },
      { duration: 0.9, ease: [0.16, 1, 0.3, 1] }
    );
    animate(
      stage.querySelectorAll('.word'),
      { opacity: [0, 1], y: [44, 0], rotate: [4, 0] },
      { duration: 0.85, delay: stagger(0.07, { startDelay: 0.25 }), ease: [0.16, 1, 0.3, 1] }
    );
    animate(
      stage.querySelectorAll('.fade-up'),
      { opacity: [0, 1], y: [24, 0] },
      { duration: 0.7, delay: stagger(0.12, { startDelay: 0.85 }), ease: [0.16, 1, 0.3, 1] }
    );
    animate(
      stage.querySelectorAll('.chip-in'),
      { opacity: [0, 1], y: [16, 0], scale: [0.9, 1] },
      { duration: 0.6, delay: stagger(0.08, { startDelay: 1.15 }), ease: [0.34, 1.56, 0.64, 1] }
    );
  });
</script>

<AuroraBg />

<main class="onboard" bind:this={stage}>
  <div class="logo brand-pop" data-reveal>
    <img src="/logo.png" alt="ItsBagelBot" />
    <span class="halo"></span>
  </div>

  <div class="eyebrow fade-up" data-reveal>ItsBagelBot Console</div>

  <h1 class="hero">
    {#each headline as w, i}
      <span class="word" data-reveal class:accent={i >= 4}>{w}&nbsp;</span>
    {/each}
  </h1>

  <p class="lede fade-up" data-reveal>
    Private, distributed, encrypted. Manage commands, moderation, and your bot from one glassy console.
  </p>

  <a class="cta fade-up" data-reveal href="/auth/login">
    <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 3h16v12l-4 4h-4l-3 3v-3H4z" /><line x1="9" y1="8" x2="9" y2="12" /><line x1="14" y1="8" x2="14" y2="12" /></svg>
    Continue with Twitch
  </a>

  <div class="features">
    <span class="feat chip-in" data-reveal>Zero downtime</span>
    <span class="feat chip-in" data-reveal>End-to-end encrypted</span>
    <span class="feat chip-in" data-reveal>Edge-routed, 3 regions</span>
  </div>
</main>

<style>
  .onboard {
    position: relative;
    z-index: 1;
    min-height: 100vh;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    text-align: center;
    gap: 18px;
    padding: 24px;
  }
  [data-reveal] { opacity: 0; }

  .logo {
    position: relative;
    width: 84px;
    height: 84px;
    display: grid;
    place-items: center;
    margin-bottom: 6px;
  }
  .logo img {
    width: 72px;
    height: 72px;
    border-radius: 18px;
    animation: float 6s ease-in-out infinite;
  }
  .logo .halo {
    position: absolute;
    inset: -22px;
    border-radius: 50%;
    background: radial-gradient(circle, rgba(82, 183, 136, 0.35), transparent 68%);
    filter: blur(8px);
    animation: pulse 3.2s ease-in-out infinite;
  }

  .eyebrow {
    font-family: var(--bb-font-mono);
    font-size: 12px;
    letter-spacing: 0.22em;
    text-transform: uppercase;
    color: var(--bb-green-glow);
  }
  .hero {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: clamp(40px, 8vw, 92px);
    line-height: 0.98;
    letter-spacing: -0.03em;
    color: var(--bb-white);
    margin: 4px 0;
    max-width: 12ch;
  }
  .hero .word { display: inline-block; }
  .hero .accent { color: var(--bb-tan-light); font-style: italic; font-weight: 600; }

  .lede {
    font-family: var(--bb-font-body);
    font-size: clamp(15px, 1.6vw, 18px);
    color: var(--bb-muted);
    max-width: 52ch;
    line-height: 1.6;
    margin: 4px 0 6px;
  }

  .cta {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    font-family: var(--bb-font-mono);
    font-size: 12px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--bb-white);
    background: var(--bb-green);
    border: 1px solid var(--bb-green-light);
    padding: 15px 26px;
    border-radius: var(--bb-radius-pill);
    text-decoration: none;
    box-shadow: 0 0 0 0 rgba(82, 183, 136, 0.5);
    transition: box-shadow 0.4s var(--bb-ease-out-expo), background 0.3s, transform 0.3s;
    animation: ctaglow 3s ease-in-out infinite;
  }
  .cta svg { width: 16px; height: 16px; stroke: currentColor; fill: none; stroke-width: 1.7; stroke-linejoin: round; }
  .cta:hover { background: var(--bb-green-light); transform: translateY(-2px); }

  .features { display: flex; gap: 10px; flex-wrap: wrap; justify-content: center; margin-top: 10px; }
  .feat {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.06em;
    color: var(--bb-tan);
    padding: 8px 14px;
    border-radius: var(--bb-radius-pill);
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--bb-border);
  }

  @keyframes float { 0%, 100% { transform: translateY(0) rotate(-1deg); } 50% { transform: translateY(-10px) rotate(1deg); } }
  @keyframes pulse { 0%, 100% { opacity: 0.55; transform: scale(1); } 50% { opacity: 0.9; transform: scale(1.12); } }
  @keyframes ctaglow {
    0%, 100% { box-shadow: 0 0 0 0 rgba(82, 183, 136, 0); }
    50% { box-shadow: 0 0 36px 2px rgba(82, 183, 136, 0.4); }
  }

  @media (prefers-reduced-motion: reduce) {
    .logo img, .logo .halo, .cta { animation: none; }
  }
</style>
