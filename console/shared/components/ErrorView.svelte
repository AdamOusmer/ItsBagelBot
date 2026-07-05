<script lang="ts">
  import { page } from '$app/state';
  import LightField from './LightField.svelte';

  let { appName, loginHref = '/login' }: { appName: string; loginHref?: string } = $props();

  const view = $derived.by(() => {
    if (page.status === 404) return {
      eyebrow: 'Lost in the crumbs',
      title: 'This page wandered off.',
      description: "We looked under every sesame seed, but the page you're after isn't here.",
      action: 'home' as const
    };
    if (page.status === 401 || page.status === 403) return {
      eyebrow: 'Behind the counter',
      title: 'This one is staff only.',
      description: `Sign in with an account that has access to the ${appName.toLowerCase()}.`,
      action: 'login' as const
    };
    if (page.status === 500 || page.status === 503) return {
      eyebrow: 'A little overbaked',
      title: 'Something went sideways.',
      description: 'A tray tipped over behind the scenes. Give us a moment, then try the page again.',
      action: 'retry' as const
    };
    return {
      eyebrow: 'An unexpected detour',
      title: 'We hit a rough patch.',
      description: page.error?.message ?? 'Something unexpected happened while loading this page.',
      action: 'retry' as const
    };
  });

  const homeLabel = $derived(appName === 'Dashboard' ? 'Back to dashboard' : `Back to ${appName.toLowerCase()}`);

  function retry() {
    window.location.reload();
  }

  function goBack() {
    if (window.history.length > 1) window.history.back();
    else window.location.assign('/');
  }
</script>

<svelte:head>
  <title>{page.status} — ItsBagelBot {appName}</title>
</svelte:head>

<main class="error-scene" aria-labelledby="error-title">
  <LightField />
  <div class="glow" aria-hidden="true"></div>

  <div class="orbit-wrap" aria-hidden="true">
    <span class="orbit one"></span>
    <span class="orbit two"></span>
  </div>

  <div class="content">
    <p class="eyebrow"><span>{page.status}</span> · {view.eyebrow}</p>
    <p class="code" aria-hidden="true">{page.status}</p>
    <h1 id="error-title">{view.title}</h1>
    <p class="description">{view.description}</p>

    <div class="actions">
      {#if view.action === 'retry'}
        <button class="error-action primary" type="button" onclick={retry}>Try again</button>
      {:else if view.action === 'login'}
        <a class="error-action primary" href={loginHref}>Sign in</a>
      {:else}
        <a class="error-action primary" href="/">{homeLabel}</a>
      {/if}
      <button class="error-action quiet" type="button" onclick={goBack}>Go back</button>
    </div>

    <p class="aside">The oven is still warm. Everything else is right where you left it.</p>
  </div>
</main>

<style>
  .error-scene {
    position: relative;
    isolation: isolate;
    overflow: hidden;
    display: grid;
    place-items: center;
    min-height: 100svh;
    padding: 72px 24px;
    text-align: center;
    background: var(--bb-black, #0a0a0a);
  }

  .glow {
    position: absolute;
    z-index: -1;
    left: 50%;
    top: 48%;
    width: min(920px, 110vw);
    aspect-ratio: 1.35;
    translate: -50% -50%;
    border-radius: 50%;
    background:
      radial-gradient(circle at 48% 44%, rgba(201, 168, 124, 0.16), transparent 34%),
      radial-gradient(circle at 54% 58%, rgba(82, 183, 136, 0.09), transparent 58%);
    filter: blur(18px);
    animation: error-glow 7s ease-in-out infinite;
    pointer-events: none;
  }

  .orbit-wrap {
    position: absolute;
    z-index: -1;
    left: 50%;
    top: 47%;
    width: min(650px, 82vw);
    aspect-ratio: 1;
    translate: -50% -50%;
    pointer-events: none;
    opacity: 0;
    animation: orbit-in 1.2s 100ms var(--bb-ease-out-expo) forwards;
  }

  .orbit {
    position: absolute;
    inset: 8%;
    border: 1px solid rgba(201, 168, 124, 0.13);
    border-radius: 50%;
    transform: rotate(-18deg) scaleY(0.46);
  }
  .orbit::after {
    content: '';
    position: absolute;
    top: 48%;
    left: -4px;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--bb-tan-light);
    box-shadow: 0 0 16px rgba(224, 196, 154, 0.8);
  }
  .orbit.one { animation: orbit-one 18s linear infinite; }
  .orbit.two {
    inset: 18%;
    border-color: rgba(82, 183, 136, 0.13);
    transform: rotate(38deg) scaleY(0.58);
    animation: orbit-two 23s linear infinite reverse;
  }
  .orbit.two::after {
    left: auto;
    right: -3px;
    background: var(--bb-green-glow);
    box-shadow: 0 0 16px rgba(82, 183, 136, 0.75);
  }

  .content {
    position: relative;
    z-index: 2;
    display: flex;
    flex-direction: column;
    align-items: center;
    width: min(100%, 760px);
  }
  .eyebrow {
    margin: 0 0 18px;
    color: var(--bb-green-glow);
    font-family: var(--bb-font-mono);
    font-size: clamp(0.68rem, 1.6vw, 0.78rem);
    line-height: 1.4;
    letter-spacing: 0.2em;
    text-transform: uppercase;
    text-shadow: 0 0 16px rgba(82, 183, 136, 0.45);
    opacity: 0;
    animation: error-rise 720ms 100ms var(--bb-ease-out-expo) forwards;
  }
  .eyebrow span { color: var(--bb-tan-light); }
  .code {
    margin: 0 0 -0.18em;
    color: transparent;
    font-family: var(--bb-font-display);
    font-size: clamp(6.8rem, 24vw, 13.5rem);
    font-weight: 800;
    line-height: 0.72;
    letter-spacing: -0.08em;
    -webkit-text-stroke: 1px rgba(201, 168, 124, 0.28);
    text-shadow: 0 0 60px rgba(201, 168, 124, 0.08);
    opacity: 0;
    animation: code-in 1s 20ms var(--bb-ease-out-expo) forwards, code-breathe 6s 1.1s ease-in-out infinite;
  }
  h1 {
    max-width: 13ch;
    margin: 0;
    color: var(--bb-white);
    font-family: var(--bb-font-display);
    font-size: clamp(2.2rem, 6.5vw, 4.5rem);
    font-weight: 800;
    line-height: 0.98;
    letter-spacing: -0.035em;
    text-shadow: 0 3px 18px rgba(0, 0, 0, 0.9), 0 0 38px rgba(201, 168, 124, 0.16);
    opacity: 0;
    animation: error-rise 780ms 260ms var(--bb-ease-out-expo) forwards;
  }
  .description {
    max-width: 49ch;
    margin: 24px 0 0;
    color: #aaa197;
    font-family: var(--bb-font-body);
    font-size: clamp(0.98rem, 1.8vw, 1.12rem);
    line-height: 1.7;
    opacity: 0;
    animation: error-rise 780ms 390ms var(--bb-ease-out-expo) forwards;
  }
  .actions {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 12px;
    margin-top: 34px;
    opacity: 0;
    animation: error-rise 780ms 500ms var(--bb-ease-out-expo) forwards;
  }
  .error-action {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-height: 48px;
    padding: 0 22px;
    border: 1px solid transparent;
    border-radius: var(--bb-radius-sm, 6px);
    font-family: var(--bb-font-display);
    font-size: 0.88rem;
    font-weight: 700;
    text-decoration: none;
    cursor: pointer;
    transition: transform 320ms var(--bb-ease-out-expo), background 220ms ease, border-color 220ms ease, box-shadow 320ms ease;
  }
  .error-action.primary { background: var(--bb-tan); color: var(--bb-black); }
  .error-action.primary:hover,
  .error-action.primary:focus-visible {
    background: var(--bb-tan-light);
    transform: translateY(-2px);
    box-shadow: 0 10px 32px rgba(201, 168, 124, 0.22);
  }
  .error-action.quiet {
    background: rgba(10, 10, 10, 0.32);
    border-color: rgba(201, 168, 124, 0.3);
    color: var(--bb-tan-light);
  }
  .error-action.quiet:hover,
  .error-action.quiet:focus-visible {
    border-color: rgba(201, 168, 124, 0.58);
    background: rgba(201, 168, 124, 0.08);
    transform: translateY(-2px);
  }
  .aside {
    margin: 32px 0 0;
    color: var(--bb-muted);
    font-family: var(--bb-font-mono);
    font-size: 0.68rem;
    line-height: 1.6;
    letter-spacing: 0.08em;
    opacity: 0;
    animation: error-rise 780ms 610ms var(--bb-ease-out-expo) forwards;
  }

  @keyframes error-rise { from { opacity: 0; transform: translateY(18px); } to { opacity: 1; transform: translateY(0); } }
  @keyframes code-in { from { opacity: 0; transform: scale(0.92); filter: blur(8px); } to { opacity: 1; transform: scale(1); filter: blur(0); } }
  @keyframes code-breathe { 50% { text-shadow: 0 0 80px rgba(201, 168, 124, 0.16); } }
  @keyframes error-glow { 50% { opacity: 0.72; transform: scale(1.06); } }
  @keyframes orbit-in { to { opacity: 1; } }
  @keyframes orbit-one { to { transform: rotate(342deg) scaleY(0.46); } }
  @keyframes orbit-two { to { transform: rotate(398deg) scaleY(0.58); } }
  @media (max-width: 520px) {
    .error-scene { padding-inline: 20px; }
    .orbit-wrap { width: 110vw; }
    .actions { width: 100%; flex-direction: column; }
    .error-action { width: min(100%, 300px); }
    .aside { max-width: 33ch; }
  }

  @media (prefers-reduced-motion: reduce) {
    .glow, .orbit-wrap, .orbit, .eyebrow, .code, h1, .description, .actions, .aside { animation: none; }
    .orbit-wrap, .eyebrow, .code, h1, .description, .actions, .aside { opacity: 1; }
    .error-action:hover { transform: none; }
  }
</style>
