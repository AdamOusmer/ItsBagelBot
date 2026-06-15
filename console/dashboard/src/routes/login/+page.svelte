<script lang="ts">
  import { AuroraBg } from '@bagel/shared';
  const headline = ['Your', 'stream.', 'Your', 'tools.', 'Your', 'rules.'];
</script>

<AuroraBg />

<main class="onboard">
  <div class="logo pop" style="--d:0s">
    <img src="/logo.png" alt="ItsBagelBot" />
    <span class="halo"></span>
  </div>

  <div class="eyebrow reveal" style="--d:.18s">ItsBagelBot Console</div>

  <h1 class="hero">
    {#each headline as w, i}
      <span class="word reveal" style="--d:{0.28 + i * 0.07}s" class:accent={i >= 4}>{w}&nbsp;</span>
    {/each}
  </h1>

  <p class="lede reveal" style="--d:.85s">
    Private, distributed, encrypted. Manage commands, modules, and your bot from one glassy console.
  </p>

  <a class="cta reveal" style="--d:1s" href="/auth/login">
    <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 3h16v12l-4 4h-4l-3 3v-3H4z" /><line x1="9" y1="8" x2="9" y2="12" /><line x1="14" y1="8" x2="14" y2="12" /></svg>
    Continue with Twitch
  </a>

  <div class="features">
    <span class="feat reveal" style="--d:1.15s">Zero downtime</span>
    <span class="feat reveal" style="--d:1.23s">End-to-end encrypted</span>
    <span class="feat reveal" style="--d:1.31s">Edge-routed, 3 regions</span>
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

  /* Entrance is CSS-only: content animates in on load with no JS dependency, so
     a reload never leaves the page invisible. `both` holds the from-state during
     the stagger delay, then settles visible. */
  .reveal {
    opacity: 0;
    animation: reveal 0.8s cubic-bezier(0.16, 1, 0.3, 1) both;
    animation-delay: var(--d, 0s);
  }
  .pop {
    opacity: 0;
    animation: pop 0.9s cubic-bezier(0.16, 1, 0.3, 1) both;
    animation-delay: var(--d, 0s);
  }
  @keyframes reveal { from { opacity: 0; transform: translateY(22px); } to { opacity: 1; transform: none; } }
  @keyframes pop { from { opacity: 0; transform: scale(0.6); filter: blur(12px); } to { opacity: 1; transform: none; filter: none; } }

  .logo { position: relative; width: 84px; height: 84px; display: grid; place-items: center; margin-bottom: 6px; }
  .logo img { width: 72px; height: 72px; border-radius: 18px; animation: float 6s ease-in-out infinite; }
  .logo .halo { position: absolute; inset: -22px; border-radius: 50%; background: radial-gradient(circle, rgba(82, 183, 136, 0.35), transparent 68%); filter: blur(8px); animation: pulse 3.2s ease-in-out infinite; }

  .eyebrow { font-family: var(--bb-font-mono); font-size: 12px; letter-spacing: 0.22em; text-transform: uppercase; color: var(--bb-green-glow); }
  .hero { font-family: var(--bb-font-display); font-weight: 700; font-size: clamp(40px, 8vw, 92px); line-height: 0.98; letter-spacing: -0.03em; color: var(--bb-white); margin: 4px 0; max-width: 12ch; }
  .hero .word { display: inline-block; }
  .hero .accent { color: var(--bb-tan-light); font-style: normal; font-weight: 700; }

  .lede { font-family: var(--bb-font-body); font-size: clamp(15px, 1.6vw, 18px); color: var(--bb-muted); max-width: 52ch; line-height: 1.6; margin: 4px 0 6px; }

  .cta { display: inline-flex; align-items: center; gap: 10px; font-family: var(--bb-font-mono); font-size: 12px; letter-spacing: 0.12em; text-transform: uppercase; color: var(--bb-white); background: var(--bb-green); border: 1px solid var(--bb-green-light); padding: 15px 26px; border-radius: var(--bb-radius-pill); text-decoration: none; transition: background 0.3s, transform 0.3s; }
  .cta svg { width: 16px; height: 16px; stroke: currentColor; fill: none; stroke-width: 1.7; stroke-linejoin: round; }
  .cta:hover { background: var(--bb-green-light); transform: translateY(-2px); box-shadow: 0 0 36px rgba(82, 183, 136, 0.4); }

  .features { display: flex; gap: 10px; flex-wrap: wrap; justify-content: center; margin-top: 10px; }
  .feat { font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.06em; color: var(--bb-tan); padding: 8px 14px; border-radius: var(--bb-radius-pill); background: rgba(255, 255, 255, 0.03); border: 1px solid var(--bb-border); }

  @keyframes float { 0%, 100% { transform: translateY(0) rotate(-1deg); } 50% { transform: translateY(-10px) rotate(1deg); } }
  @keyframes pulse { 0%, 100% { opacity: 0.55; transform: scale(1); } 50% { opacity: 0.9; transform: scale(1.12); } }

  @media (prefers-reduced-motion: reduce) {
    .reveal, .pop { animation: none; opacity: 1; }
    .logo img, .logo .halo { animation: none; }
  }
</style>
