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
            : 'Overview'
  );

  const initial = $derived((data.displayName ?? 'A').charAt(0).toUpperCase());
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

    <div class="side-spacer"></div>

    <div class="side-foot">
      <div class="account">
        <div class="avatar">{initial}</div>
        <div class="who">
          <b>{data.displayName}</b>
          <span>Operator</span>
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
      <div class="status-pill"><span class="dot"></span> Connected</div>
      <button class="icon-btn" aria-label="Notifications"><Icon name="bell" size={17} /></button>
    </header>

    <div class="canvas">
      {@render children()}
    </div>
  </main>
</div>
