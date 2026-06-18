<script lang="ts">
  import { page } from '$app/state';
  import { Icon, NavItem } from '@bagel/shared';
  let { data, children } = $props();

  const path = $derived(page.url.pathname);
  const crumb = $derived(
    path.startsWith('/commands')
      ? 'Commands'
      : path.startsWith('/settings') || path.startsWith('/access')
        ? 'Settings'
        : 'Overview'
  );

  const initial = $derived((data.displayName ?? 'M').charAt(0).toUpperCase());

  // Delegate view: nav and routes are limited to the granted sections, and the
  // owner-only Overview/Access entries are hidden.
  const isDelegate = $derived(!!data.delegateOf);
  const sections = $derived((data.sections ?? []) as string[]);
  const canCommands = $derived(!isDelegate || sections.includes('commands'));
</script>

{#if isDelegate}
  <div class="imp-banner" role="status">
    <span>Shared access to <b>{data.delegateLogin}</b>'s dashboard ({sections.join(', ')})</span>
    <a href="/delegate/exit" class="imp-exit">Exit</a>
  </div>
{/if}

{#if data.impersonatorLogin}
  <div class="imp-banner" role="status">
    <span>Viewing as <b>{data.login}</b> (admin {data.impersonatorLogin})</span>
    <form method="POST" action="/auth/logout">
      <button type="submit" class="imp-exit">Exit</button>
    </form>
  </div>
{/if}

<div class="app" class:impersonating={data.impersonatorLogin || isDelegate}>
  <aside class="sidebar">
    <div class="brand">
      <img src="/logo.png" alt="ItsBagelBot" />
      <div>
        <div class="name">ItsBagelBot</div>
        <div class="sub">Console</div>
      </div>
    </div>

    <div class="nav-group-label">Manage</div>
    <nav class="nav">
      {#if !isDelegate}
        <NavItem href="/" icon="overview" label="Overview" active={crumb === 'Overview'} />
      {/if}
      {#if canCommands}
        <NavItem href="/commands" icon="commands" label="Commands" active={crumb === 'Commands'} />
      {/if}
      {#if !isDelegate}
        <NavItem href="/settings" icon="settings" label="Settings" active={crumb === 'Settings'} />
      {/if}
    </nav>

    <div class="side-spacer"></div>

    <div class="side-foot">
      <div class="account">
        <div class="avatar">{initial}</div>
        <div class="who">
          <b>{data.displayName}</b>
          <span>Broadcaster</span>
        </div>
      </div>
      <form method="POST" action="/auth/logout">
        <button class="btn ghost" type="submit" style="width:100%;justify-content:center;margin-top:10px">
          <Icon name="power" size={14} /> Log out
        </button>
      </form>
    </div>
  </aside>

  <main class="main">
    <header class="topbar">
      <div class="crumb">
        <span>ItsBagelBot</span><span class="sep">/</span><span class="here">{crumb}</span>
      </div>
      <div class="grow"></div>
      <button class="icon-btn" aria-label="Notifications"><Icon name="bell" size={17} /></button>
    </header>

    <div class="canvas">
      {@render children()}
    </div>
  </main>

  <!-- mobile bottom nav (sidebar is hidden ≤760px) -->
  <nav class="mobile-nav">
    {#if !isDelegate}
      <a href="/" class={crumb === 'Overview' ? 'active' : ''}><Icon name="overview" size={20} />Overview</a>
    {/if}
    {#if canCommands}
      <a href="/commands" class={crumb === 'Commands' ? 'active' : ''}><Icon name="commands" size={20} />Commands</a>
    {/if}
    {#if !isDelegate}
      <a href="/settings" class={crumb === 'Settings' ? 'active' : ''}><Icon name="settings" size={20} />Settings</a>
    {/if}
    <form method="POST" action="/auth/logout"><button type="submit"><Icon name="power" size={20} />Log out</button></form>
  </nav>
</div>

<style>
  .imp-banner {
    position: fixed; top: 0; left: 0; right: 0; z-index: 300;
    display: flex; align-items: center; justify-content: center; gap: 1rem;
    padding: 8px 14px;
    font-family: var(--bb-font-body); font-size: 13px;
    color: var(--bb-bg-1, #111);
    background: var(--bb-tan, #c9a87c);
    border-bottom: 1px solid rgba(0, 0, 0, 0.2);
  }
  .imp-banner b { font-weight: 700; }
  .imp-banner form { display: inline; }
  .imp-exit {
    cursor: pointer; font: inherit; font-weight: 700;
    display: inline-block; text-decoration: none; line-height: 1.4;
    padding: 3px 12px; border-radius: var(--bb-radius-sm, 8px);
    color: var(--bb-bg-1, #111);
    background: transparent; border: 1px solid rgba(0, 0, 0, 0.35);
  }
  .imp-exit:hover { background: rgba(0, 0, 0, 0.12); }
  /* The banner is a fixed ~44px strip. Offset the grid below it with padding and
     keep the shell exactly 100vh via border-box, so the content area shrinks to
     100vh - 44px instead of the page growing past the viewport. A margin-top or a
     content-box padding-top here adds 44px on top of min-height:100vh and scrolls
     the whole page (nav included). The sidebar (otherwise height:100vh, sticky
     top:0) is matched to the reduced area so it does not overhang either. */
  .app.impersonating {
    box-sizing: border-box;
    padding-top: 44px;
    min-height: 100vh;
  }
  .app.impersonating :global(.sidebar) {
    height: calc(100vh - 44px);
    top: 44px;
  }
</style>
