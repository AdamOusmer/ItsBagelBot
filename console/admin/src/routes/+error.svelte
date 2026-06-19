<script lang="ts">
  import { page } from '$app/stores';

  $: isTransient = $page.status === 503 || $page.status === 500;
  $: isNotFound = $page.status === 404;
  $: isAuth = $page.status === 401 || $page.status === 403;

  function retry() {
    window.location.reload();
  }
</script>

<svelte:head>
  <title>{$page.status} — ItsBagelBot Admin</title>
</svelte:head>

<div class="error-shell">
  <div class="error-card">
    <span class="error-code">{$page.status}</span>
    {#if isTransient}
      <h1>Back in a moment</h1>
      <p>The admin console is updating. This should resolve in a few seconds.</p>
      <button onclick={retry}>Retry</button>
    {:else if isNotFound}
      <h1>Page not found</h1>
      <p>The page you're looking for doesn't exist.</p>
      <a href="/">Go home</a>
    {:else if isAuth}
      <h1>Access denied</h1>
      <p>You don't have permission to view this page.</p>
      <a href="/login">Sign in</a>
    {:else}
      <h1>Something went wrong</h1>
      <p>{$page.error?.message ?? 'An unexpected error occurred.'}</p>
      <button onclick={retry}>Retry</button>
    {/if}
  </div>
</div>

<style>
  .error-shell {
    min-height: 100dvh;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--bg, #0f0f13);
    font-family: var(--font-sans, system-ui, sans-serif);
  }

  .error-card {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 1rem;
    padding: 3rem 2.5rem;
    background: var(--surface, #1a1a22);
    border: 1px solid var(--border, #2a2a38);
    border-radius: 1rem;
    text-align: center;
    max-width: 420px;
    width: 90%;
  }

  .error-code {
    font-size: 3.5rem;
    font-weight: 800;
    line-height: 1;
    color: var(--accent, #7c6aff);
    letter-spacing: -2px;
  }

  h1 {
    margin: 0;
    font-size: 1.25rem;
    font-weight: 600;
    color: var(--text, #e8e8f0);
  }

  p {
    margin: 0;
    font-size: 0.9rem;
    color: var(--text-muted, #888899);
    line-height: 1.6;
  }

  button, a {
    margin-top: 0.5rem;
    padding: 0.6rem 1.5rem;
    background: var(--accent, #7c6aff);
    color: #fff;
    border: none;
    border-radius: 0.5rem;
    font-size: 0.875rem;
    font-weight: 600;
    cursor: pointer;
    text-decoration: none;
    transition: opacity 0.15s;
  }

  button:hover, a:hover { opacity: 0.85; }
</style>
