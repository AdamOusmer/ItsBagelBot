<script lang="ts">
  import { page } from '$app/state';
  import { Icon, NavItem } from '@bagel/shared';
  let { data, children } = $props();

  const path = $derived(page.url.pathname);
  const crumb = $derived(
    path.startsWith('/shards')
      ? 'Shards'
      : path.startsWith('/lanes')
        ? 'Lanes'
        : path.startsWith('/users')
          ? 'Users'
          : path.startsWith('/events')
            ? 'Events'
            : path.startsWith('/staff')
              ? 'Staff'
              : path.startsWith('/audit')
                ? 'Audit'
                : 'Overview'
  );

  const initial = $derived((data.displayName ?? 'A').charAt(0).toUpperCase());

  // Staff roster + audit log are manager-only (admin/owner). Moderators never
  // see the nav entries; the routes also redirect them server-side.
  const isManager = $derived(data.role === 'admin' || data.role === 'owner');
  const roleLabel = $derived(
    data.role === 'owner' ? 'Owner' : data.role === 'admin' ? 'Admin' : 'Moderator'
  );
</script>

<div class="app">
  <aside class="sidebar">
    <div class="brand">
      <img src="/logo.png" alt="ItsBagelBot" />
      <div>
        <div class="name">ItsBagelBot</div>
        <div class="sub">Admin</div>
      </div>
    </div>

    <div class="nav-group-label">Operate</div>
    <nav class="nav">
      <NavItem href="/" icon="overview" label="Overview" active={crumb === 'Overview'} />
      <NavItem href="/shards" icon="pulse" label="Shards" active={crumb === 'Shards'} />
      <NavItem href="/lanes" icon="activity" label="Lanes" active={crumb === 'Lanes'} />
    </nav>

    <div class="nav-group-label">Accounts</div>
    <nav class="nav">
      <NavItem href="/users" icon="users" label="Users" active={crumb === 'Users'} />
      <NavItem href="/events" icon="bell" label="Events" active={crumb === 'Events'} />
    </nav>

    {#if isManager}
      <div class="nav-group-label">Access</div>
      <nav class="nav">
        <NavItem href="/staff" icon="moderation" label="Staff" active={crumb === 'Staff'} />
        <NavItem href="/audit" icon="check" label="Audit" active={crumb === 'Audit'} />
      </nav>
    {/if}

    <div class="side-spacer"></div>

    <div class="side-foot">
      <div class="account">
        <div class="avatar">{initial}</div>
        <div class="who">
          <b>{data.displayName}</b>
          <span>{roleLabel}</span>
        </div>
      </div>
    </div>
  </aside>

  <main class="main">
    <header class="topbar">
      <div class="crumb">
        <span>Admin</span><span class="sep">/</span><span class="here">{crumb}</span>
      </div>
      <div class="grow"></div>
      <label class="search">
        <Icon name="search" size={15} />
        <input type="text" placeholder="Search users, shards…" />
      </label>
      <button class="icon-btn" aria-label="Notifications"><Icon name="bell" size={17} /></button>
    </header>

    <div class="canvas">
      {@render children()}
    </div>
  </main>

  <!-- mobile bottom nav: shown <=760px, replaces hidden sidebar -->
  <nav class="mobile-nav" aria-label="Main navigation">
    <a href="/" class={crumb === 'Overview' ? 'active' : ''}><Icon name="overview" size={20} /><span>Overview</span></a>
    <a href="/shards" class={crumb === 'Shards' ? 'active' : ''}><Icon name="pulse" size={20} /><span>Shards</span></a>
    <a href="/lanes" class={crumb === 'Lanes' ? 'active' : ''}><Icon name="activity" size={20} /><span>Lanes</span></a>
    <a href="/users" class={crumb === 'Users' ? 'active' : ''}><Icon name="users" size={20} /><span>Users</span></a>
    {#if isManager}
      <a href="/staff" class={crumb === 'Staff' ? 'active' : ''}><Icon name="moderation" size={20} /><span>Staff</span></a>
      <a href="/audit" class={crumb === 'Audit' ? 'active' : ''}><Icon name="check" size={20} /><span>Audit</span></a>
    {:else}
      <a href="/events" class={crumb === 'Events' ? 'active' : ''}><Icon name="bell" size={20} /><span>Events</span></a>
    {/if}
  </nav>
</div>

<style>
  /* mobile-nav: hidden above 760px; shown below (mirrors shared app.css pattern) */
  .mobile-nav {
    display: none;
  }

  @media (max-width: 760px) {
    /* fix app grid: single-column with room for bottom nav */
    :global(.app) {
      grid-template-columns: 1fr;
    }

    /* bottom nav */
    .mobile-nav {
      display: flex;
      position: fixed;
      left: 0; right: 0; bottom: 0;
      z-index: 50;
      padding: 6px max(8px, env(safe-area-inset-left)) calc(6px + env(safe-area-inset-bottom));
      gap: 0;
      background: rgba(10, 10, 10, 0.82);
      border-top: 1px solid var(--glass-border);
      backdrop-filter: blur(var(--glass-blur));
      -webkit-backdrop-filter: blur(var(--glass-blur));
    }

    .mobile-nav a {
      flex: 1;
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 3px;
      padding: 6px 2px;
      min-height: 44px;
      color: var(--bb-muted);
      text-decoration: none;
      font-family: var(--bb-font-mono);
      font-size: 8px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
      transition: color 0.15s ease;
    }

    /* hide text labels on narrowest viewports; keep icon tap target */
    @media (max-width: 400px) {
      .mobile-nav a span {
        display: none;
      }
    }

    :global(.mobile-nav a svg) {
      width: 20px;
      height: 20px;
      stroke: currentColor;
      fill: none;
      stroke-width: 1.7;
      stroke-linecap: round;
      stroke-linejoin: round;
    }

    .mobile-nav a.active {
      color: var(--bb-green-glow);
    }

    /* canvas bottom padding: prevent content from hiding behind nav bar */
    :global(.canvas) {
      padding-bottom: calc(72px + var(--gutter));
    }
  }
</style>
