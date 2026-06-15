<script lang="ts">
  import { page } from '$app/state';
  import { Icon, NavItem } from '@bagel/shared';
  let { data, children } = $props();

  const path = $derived(page.url.pathname);
  const crumb = $derived(
    path.startsWith('/commands') ? 'Commands' : path.startsWith('/modules') ? 'Modules' : 'Overview'
  );

  const initial = $derived((data.displayName ?? 'M').charAt(0).toUpperCase());
</script>

<div class="app">
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
      <NavItem href="/" icon="overview" label="Overview" active={crumb === 'Overview'} />
      <NavItem href="/commands" icon="commands" label="Commands" active={crumb === 'Commands'} />
      <NavItem href="/modules" icon="moderation" label="Modules" active={crumb === 'Modules'} />
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
      <label class="search">
        <Icon name="search" size={15} />
        <input type="text" placeholder="Search commands…" />
      </label>
      <div class="status-pill"><span class="dot"></span> Connected</div>
      <button class="icon-btn" aria-label="Notifications"><Icon name="bell" size={17} /></button>
    </header>

    <div class="canvas">
      {@render children()}
    </div>
  </main>
</div>
