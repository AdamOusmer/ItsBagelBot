<script lang="ts">
  import { page } from '$app/state';
  import Icon from './Icon.svelte';

  let { appName, loginHref = '/login' }: { appName: string; loginHref?: string } = $props();

  const isTransient = $derived(page.status === 503 || page.status === 500);
  const isNotFound = $derived(page.status === 404);
  const isAuth = $derived(page.status === 401 || page.status === 403);

  function retry() {
    window.location.reload();
  }
</script>

<svelte:head>
  <title>{page.status} — ItsBagelBot {appName}</title>
</svelte:head>

<main class="error-shell">
  <div class="panel">
    <div class="status-icon">
      {#if isTransient}
        <Icon name="pulse" size={24} />
      {:else if isNotFound}
        <Icon name="search" size={24} />
      {:else if isAuth}
        <Icon name="moderation" size={24} />
      {:else}
        <Icon name="activity" size={24} />
      {/if}
    </div>

    <div class="error-code">{page.status}</div>

    {#if isTransient}
      <div class="name">Back in a moment</div>
      <p class="lede">The {appName.toLowerCase()} console is updating. This should resolve in a few seconds.</p>
      <button class="err-btn" onclick={retry}>Retry</button>
    {:else if isNotFound}
      <div class="name">Page not found</div>
      <p class="lede">The page you're looking for doesn't exist.</p>
      <a href="/" class="err-btn">Go home</a>
    {:else if isAuth}
      <div class="name">Access denied</div>
      <p class="lede">You don't have permission to view this page.</p>
      <a href={loginHref} class="err-btn">Sign in</a>
    {:else}
      <div class="name">Something went wrong</div>
      <p class="lede">{page.error?.message ?? 'An unexpected error occurred.'}</p>
      <button class="err-btn" onclick={retry}>Retry</button>
    {/if}
  </div>
</main>

<style>
  .error-shell {
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px;
    /* fallback background if not rendered inside the main app layout */
    background: var(--bb-bg-1, #0f0f13);
  }
  .panel {
    width: 100%;
    max-width: 420px;
    padding: 44px 40px 40px;
    text-align: center;
    background: var(--glass-fill, rgba(255, 255, 255, 0.055));
    border: 1px solid var(--glass-border, rgba(255, 255, 255, 0.14));
    border-radius: 8px 8px;
    backdrop-filter: blur(var(--glass-blur, 26px)) saturate(var(--glass-sat, 180%));
    -webkit-backdrop-filter: blur(var(--glass-blur, 26px)) saturate(var(--glass-sat, 180%));
    box-shadow: var(--glass-rim), var(--glass-shadow);
  }
  .status-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 48px;
    height: 48px;
    border-radius: 8px;
    background: rgba(255, 255, 255, 0.05);
    color: var(--bb-tan, #c9a87c);
    border: 1px solid var(--glass-border, rgba(255, 255, 255, 0.14));
    margin-bottom: 16px;
  }
  .status-icon :global(svg) {
    stroke: currentColor;
    fill: none;
    stroke-width: 1.7;
  }
  .error-code {
    font-family: var(--bb-font-display, system-ui);
    font-size: 3.5rem;
    font-weight: 800;
    line-height: 1;
    color: var(--bb-tan, #c9a87c);
    letter-spacing: -2px;
    margin-bottom: 12px;
  }
  .name {
    font-family: var(--bb-font-display, system-ui);
    font-weight: 700;
    font-size: 22px;
    letter-spacing: -0.01em;
    color: var(--bb-white, #fff);
  }
  .lede {
    font-family: var(--bb-font-body, system-ui);
    font-size: 14px;
    line-height: 1.55;
    color: var(--bb-muted, #888899);
    margin: 12px 0 24px;
  }
  .err-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 0.4rem;
    width: 100%;
    font-family: var(--bb-font-mono, ui-monospace, monospace);
    font-size: 11px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    padding: 12px 20px;
    border-radius: var(--bb-radius-pill, 999px);
    cursor: pointer;
    white-space: nowrap;
    text-decoration: none;
    color: var(--bb-white, #fff);
    background: var(--ui-accent, var(--bb-green, #2d6a4f));
    border: 1px solid var(--ui-accent-light, var(--bb-green-light, #40916c));
    transition: all var(--bb-dur-base, 240ms) var(--bb-ease-out-back, ease);
  }
  .err-btn:hover {
    background: var(--ui-accent-light, var(--bb-green-light, #40916c));
    box-shadow: 0 0 24px rgba(82, 183, 136, 0.32);
  }
</style>
