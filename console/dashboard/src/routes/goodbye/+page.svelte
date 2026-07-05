<script lang="ts">
  import { onMount } from 'svelte';
  import { AuroraBg, getI18n } from '@bagel/shared';

  const { t } = getI18n();

  const HOME = 'https://itsbagelbot.com';
  const DELAY_MS = 5000;

  let leaving = $state(false);

  onMount(() => {
    localStorage.removeItem('bb-onboarded');
    let exit: ReturnType<typeof setTimeout>;
    const t = setTimeout(() => {
      leaving = true;
      exit = setTimeout(() => (window.location.href = HOME), 700);
    }, DELAY_MS);
    return () => {
      clearTimeout(t);
      clearTimeout(exit);
    };
  });
</script>

<svelte:head>
  <title>{t('goodbye.title')}</title>
  <noscript><meta http-equiv="refresh" content="5;url=https://itsbagelbot.com" /></noscript>
</svelte:head>

<AuroraBg />

<main class="onboard" class:leaving>
  <div class="logo pop" style="--d:0s">
    <img src="/logo.png" alt="ItsBagelBot" />
    <span class="halo"></span>
  </div>

  <div class="eyebrow reveal" style="--d:.18s">{t('goodbye.eyebrow')}</div>

  <h1 class="hero reveal" style="--d:.3s">{t('goodbye.heroPre')}<span class="accent">{t('goodbye.heroAccent')}</span></h1>

  <p class="lede reveal" style="--d:.6s">
    {t('goodbye.lede')}
  </p>

  <a class="cta reveal" style="--d:.85s" href={HOME}>{t('goodbye.cta')}</a>

  <span class="bar reveal" style="--d:1s" aria-hidden="true"><i></i></span>
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
    gap: 16px;
    padding: 24px;
    transition: opacity 0.7s ease, filter 0.7s ease;
  }
  .onboard.leaving { opacity: 0; filter: blur(6px); }

  .reveal { opacity: 0; animation: reveal 0.8s cubic-bezier(0.16, 1, 0.3, 1) both; animation-delay: var(--d, 0s); }
  .pop { opacity: 0; animation: pop 0.9s cubic-bezier(0.16, 1, 0.3, 1) both; animation-delay: var(--d, 0s); }
  @keyframes reveal { from { opacity: 0; transform: translateY(22px); } to { opacity: 1; transform: none; } }
  @keyframes pop { from { opacity: 0; transform: scale(0.6); filter: blur(12px); } to { opacity: 1; transform: none; filter: none; } }

  .logo { position: relative; width: 84px; height: 84px; display: grid; place-items: center; margin-bottom: 6px; }
  .logo img { width: 72px; height: 72px; border-radius: 8px; animation: float 6s ease-in-out infinite; }
  .logo .halo { position: absolute; inset: -22px; border-radius: 50%; background: radial-gradient(circle, rgba(82, 183, 136, 0.35), transparent 68%); filter: blur(8px); animation: pulse 3.2s ease-in-out infinite; }

  .eyebrow { font-family: var(--bb-font-mono); font-size: 12px; letter-spacing: 0.22em; text-transform: uppercase; color: var(--bb-green-glow); }
  .hero { font-family: var(--bb-font-display); font-weight: 700; font-size: clamp(34px, 6vw, 68px); line-height: 1; letter-spacing: -0.03em; color: var(--bb-white); margin: 4px 0; max-width: 16ch; }
  .hero .accent { color: var(--bb-tan-light); }

  .lede { font-family: var(--bb-font-body); font-size: clamp(15px, 1.6vw, 18px); color: var(--bb-muted); max-width: 52ch; line-height: 1.6; margin: 4px 0 6px; }

  .cta { display: inline-flex; align-items: center; gap: 10px; font-family: var(--bb-font-mono); font-size: 12px; letter-spacing: 0.12em; text-transform: uppercase; color: var(--bb-white); background: var(--bb-green); border: 1px solid var(--bb-green-light); padding: 15px 26px; border-radius: var(--bb-radius-pill); text-decoration: none; transition: background 0.3s, transform 0.3s; }
  .cta:hover { background: var(--bb-green-light); transform: translateY(-2px); box-shadow: 0 0 36px rgba(82, 183, 136, 0.4); }

  .bar { margin-top: 8px; width: 180px; height: 3px; border-radius: 999px; background: rgba(255, 255, 255, 0.06); border: 1px solid var(--bb-border); overflow: hidden; }
  .bar i { display: block; height: 100%; width: 100%; background: var(--bb-green); transform-origin: left; animation: drain 5s linear forwards; }

  @keyframes float { 0%, 100% { transform: translateY(0) rotate(-1deg); } 50% { transform: translateY(-10px) rotate(1deg); } }
  @keyframes pulse { 0%, 100% { opacity: 0.55; transform: scale(1); } 50% { opacity: 0.9; transform: scale(1.12); } }
  @keyframes drain { from { transform: scaleX(1); } to { transform: scaleX(0); } }

  @media (prefers-reduced-motion: reduce) {
    .reveal, .pop, .logo img, .logo .halo, .bar i { animation: none; opacity: 1; }
    .onboard.leaving { opacity: 1; filter: none; }
  }
</style>
