<script lang="ts">
  import { page } from '$app/state';
  import { Icon, NavItem } from '@bagel/shared';
  let { data, children } = $props();

  let role = $state<'streamer' | 'mod'>(data.role ?? 'streamer');
  const path = $derived(page.url.pathname);
  const crumb = $derived(
    path.startsWith('/commands') ? 'Commands' : path.startsWith('/moderation') ? 'Moderation' : 'Overview'
  );

  function setRole(r: 'streamer' | 'mod') {
    role = r;
    document.body.dataset.role = r;
  }

  const initial = (data.displayName ?? 'M').charAt(0).toUpperCase();
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
      <NavItem href="/commands" icon="commands" label="Commands" active={crumb === 'Commands'} count={24} />
      <NavItem href="/moderation" icon="moderation" label="Moderation" active={crumb === 'Moderation'} />
    </nav>

    <div class="nav-group-label">Channel</div>
    <nav class="nav">
      <NavItem href="/" icon="activity" label="Activity" />
      {#if role === 'mod'}
        <NavItem href="/" icon="settings" label="Settings" locked />
      {:else}
        <NavItem href="/" icon="settings" label="Settings" />
      {/if}
    </nav>

    <div class="side-spacer"></div>

    <div class="side-foot">
      <div class="role-switch">
        <button class="role-opt {role === 'streamer' ? 'on' : ''}" onclick={() => setRole('streamer')}>Streamer</button>
        <button class="role-opt {role === 'mod' ? 'on' : ''}" onclick={() => setRole('mod')}>Moderator</button>
      </div>
      <div class="account">
        <div class="avatar">{initial}</div>
        <div class="who">
          <b>{data.displayName}</b>
          <span>{role === 'mod' ? 'Moderator' : 'Broadcaster'}</span>
        </div>
      </div>
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
        <input type="text" placeholder="Search commands, users…" />
      </label>
      <div class="status-pill"><span class="dot"></span> Connected</div>
      <button class="icon-btn" aria-label="Notifications"><Icon name="bell" size={17} /></button>
    </header>

    <div class="canvas">
      {@render children()}
    </div>
  </main>
</div>
