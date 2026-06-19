<script lang="ts">
  import { page } from '$app/state';
  import { Icon } from '@bagel/shared';

  const isTransient = $derived(page.status === 503 || page.status === 500);
  const isNotFound = $derived(page.status === 404);
  const isAuth = $derived(page.status === 401 || page.status === 403);

  function retry() {
    window.location.reload();
  }
</script>

<svelte:head>
  <title>{page.status} — ItsBagelBot Admin</title>
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
      <p class="lede">The admin console is updating. This should resolve in a few seconds.</p>
      <button class="btn primary block" onclick={retry}>Retry</button>
    {:else if isNotFound}
      <div class="name">Page not found</div>
      <p class="lede">The page you're looking for doesn't exist.</p>
      <a href="/" class="btn primary block">Go home</a>
    {:else if isAuth}
      <div class="name">Access denied</div>
      <p class="lede">You don't have permission to view this page.</p>
      <a href="/login" class="btn primary block">Sign in</a>
    {:else}
      <div class="name">Something went wrong</div>
      <p class="lede">{page.error?.message ?? 'An unexpected error occurred.'}</p>
      <button class="btn primary block" onclick={retry}>Retry</button>
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
    background: var(--glass-fill);
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-lg, 16px);
    backdrop-filter: blur(var(--glass-blur)) saturate(var(--glass-sat, 180%));
    -webkit-backdrop-filter: blur(var(--glass-blur)) saturate(var(--glass-sat, 180%));
    box-shadow: var(--glass-rim), var(--glass-shadow);
  }
  .status-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 48px;
    height: 48px;
    border-radius: 12px;
    background: rgba(255, 255, 255, 0.05);
    color: var(--bb-tan, #c9a87c);
    border: 1px solid var(--glass-border);
    margin-bottom: 16px;
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
  .btn.block {
    width: 100%;
    justify-content: center;
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
  }
</style>
