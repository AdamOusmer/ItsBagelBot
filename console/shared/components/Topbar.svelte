<script lang="ts">
  // Call-sign strip: with the sidebar gone this is the only chrome above the
  // content — station mark, route readout, wall clock, and the signed-in
  // operator. One thin ruled line, everything else is page.
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  let {
    root,
    crumb,
    actions,
    brandTitle = 'ItsBagelBot',
    brandSub = '',
    accountName = '',
    accountRole = ''
  }: {
    root: string;
    crumb: string;
    actions?: Snippet;
    brandTitle?: string;
    brandSub?: string;
    accountName?: string;
    accountRole?: string;
  } = $props();

  const initial = $derived((accountName || '?').charAt(0).toUpperCase());

  // Account menu: the avatar chip opens a small dropdown holding Log out —
  // sign-out lives here (not in the dock) so navigation stays uncrowded.
  let menuOpen = $state(false);

  // Local wall-clock readout — the strip's "master control" pulse.
  let now = $state('');
  $effect(() => {
    const fmt = () => (now = new Date().toLocaleTimeString(undefined, { hour12: false }));
    fmt();
    const t = setInterval(fmt, 1000);
    return () => clearInterval(t);
  });
</script>

<svelte:window onkeydown={(e) => { if (e.key === 'Escape') menuOpen = false; }} />

<header class="topbar">
  <a class="station" href="/">
    <img src="/logo.png" alt="" />
    <span class="station-id">
      <b>{brandTitle}</b>
      {#if brandSub}<i>{brandSub}</i>{/if}
    </span>
  </a>

  <div class="crumb">
    <span class="root">{root}</span><span class="sep">/</span><span class="here">{crumb}</span>
  </div>

  <div class="grow"></div>

  <span class="clock" aria-hidden="true">{now}</span>

  {#if accountName}
    <div class="operator-wrap">
      <button
        class="operator"
        class:open={menuOpen}
        title="{accountName} · {accountRole}"
        aria-expanded={menuOpen}
        aria-haspopup="menu"
        onclick={() => (menuOpen = !menuOpen)}
      >
        <span class="avatar">{initial}</span>
        <span class="op-id">
          <b>{accountName}</b>
          <i>{accountRole}</i>
        </span>
      </button>
      {#if menuOpen}
        <!-- Click-away scrim; Escape via the window handler below. -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class="op-scrim"
          role="presentation"
          onclick={() => (menuOpen = false)}
          onkeydown={(e) => { if (e.key === 'Enter') menuOpen = false; }}
        ></div>
        <div class="op-menu" role="menu">
          <div class="op-menu-head">
            <b>{accountName}</b>
            <i>{accountRole}</i>
          </div>
          <form method="POST" action="/auth/logout">
            <button type="submit" class="op-menu-item" role="menuitem">
              <Icon name="power" size={15} />
              Log out
            </button>
          </form>
        </div>
      {/if}
    </div>
  {/if}

  {#if actions}
    {@render actions()}
  {:else}
    <button class="icon-btn" aria-label="Notifications"><Icon name="bell" size={16} /></button>
  {/if}
</header>

<style>
  .topbar {
    position: sticky; top: 0; z-index: 40;
    display: flex; align-items: center; gap: 14px;
    /* Top padding carries the notch inset so the strip's ink paints the safe
       area; the row itself stays at its normal height below it. */
    padding: calc(9px + env(safe-area-inset-top, 0px)) max(16px, env(safe-area-inset-right, 0px)) 9px max(16px, env(safe-area-inset-left, 0px));
    background: rgba(10, 10, 10, 0.85);
    border-bottom: 1px solid var(--rule, rgba(240, 236, 228, 0.1));
  }
  @media (min-width: 761px) {
    .topbar { gap: 20px; padding: calc(9px + env(safe-area-inset-top, 0px)) var(--gutter) 9px; }
  }

  .station { display: flex; align-items: center; gap: 9px; text-decoration: none; flex: none; }
  .station img { width: 26px; height: 26px; border-radius: var(--bb-radius-sm, 6px); }
  .station-id { display: flex; flex-direction: column; line-height: 1; }
  .station-id b { font-family: var(--bb-font-display); font-weight: 800; font-size: 13.5px; letter-spacing: -0.01em; color: var(--bb-white); }
  .station-id i { font-style: normal; font-family: var(--bb-font-display); font-weight: 700; font-size: 9.5px; letter-spacing: 0.04em; color: var(--bb-tan); margin-top: 3px; }

  .crumb { font-family: var(--bb-font-body); font-weight: 500; font-size: 13px; color: var(--bb-muted); display: flex; align-items: center; gap: 8px; min-width: 0; }
  .crumb .sep { opacity: 0.45; }
  .crumb .here { color: var(--bb-tan-light); white-space: nowrap; font-weight: 600; }
  .crumb .root { white-space: nowrap; }

  .grow { flex: 1; }

  .clock {
    font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.12em;
    color: var(--bb-muted); font-variant-numeric: tabular-nums;
    display: none;
  }

  .operator-wrap { position: relative; display: flex; }
  .operator {
    display: flex; align-items: center; gap: 9px;
    background: none; border: none; padding: 3px; border-radius: var(--bb-radius-pill, 100px);
    cursor: pointer;
    transition: background var(--bb-dur-fast, 180ms) ease;
  }
  .operator:hover, .operator.open { background: rgba(201, 168, 124, 0.1); }
  .avatar {
    width: 30px; height: 30px; border-radius: 50%; flex: none;
    background: linear-gradient(135deg, var(--bb-green-light), var(--bb-tan));
    display: flex; align-items: center; justify-content: center;
    font-family: var(--bb-font-display); font-weight: 800; font-size: 12px; color: #0a0a0a;
  }
  .op-id { display: none; flex-direction: column; line-height: 1; text-align: left; }
  .op-id b { font-family: var(--bb-font-body); font-weight: 600; font-size: 12px; color: var(--bb-white); max-width: 140px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .op-id i { font-style: normal; font-family: var(--bb-font-display); font-weight: 700; font-size: 10px; letter-spacing: 0.02em; color: var(--bb-tan); margin-top: 3px; }

  .op-scrim { position: fixed; inset: 0; z-index: 89; }
  .op-menu {
    position: absolute;
    top: calc(100% + 10px);
    right: 0;
    z-index: 90;
    min-width: 190px;
    padding: 8px;
    background: var(--bb-card-bg, #111110);
    border: 1px solid var(--bb-border-strong, rgba(201, 168, 124, 0.35));
    border-radius: var(--bb-radius-lg, 16px);
    box-shadow: 0 18px 50px rgba(0, 0, 0, 0.55);
    transform-origin: top right;
    animation: menu-in 240ms var(--bb-ease-out-back, ease-out) both;
  }
  @keyframes menu-in {
    from { opacity: 0; transform: translateY(-6px) scale(0.97); }
    to { opacity: 1; transform: translateY(0) scale(1); }
  }
  .op-menu-head { display: flex; flex-direction: column; gap: 3px; padding: 6px 10px 10px; border-bottom: 1px solid var(--bb-border); margin-bottom: 6px; }
  .op-menu-head b { font-family: var(--bb-font-body); font-weight: 600; font-size: 13px; color: var(--bb-white); }
  .op-menu-head i { font-style: normal; font-family: var(--bb-font-display); font-weight: 700; font-size: 10px; color: var(--bb-tan); }
  .op-menu form { display: flex; }
  .op-menu-item {
    display: flex; align-items: center; gap: 10px; width: 100%;
    padding: 10px 10px; border-radius: var(--bb-radius-md, 10px);
    background: none; border: none; cursor: pointer;
    font-family: var(--bb-font-body); font-weight: 600; font-size: 13px; color: var(--bb-muted);
    transition: color var(--bb-dur-fast, 180ms) ease, background var(--bb-dur-fast, 180ms) ease;
  }
  .op-menu-item :global(svg) { stroke: currentColor; fill: none; }
  .op-menu-item:hover { color: var(--bb-white); background: rgba(201, 168, 124, 0.1); }

  @media (min-width: 761px) {
    .clock { display: inline; }
    .op-id { display: flex; }
  }
  @media (prefers-reduced-motion: reduce) {
    .op-menu { animation: none; }
  }
  /* On phones the station id doubles as the crumb root, so hide the root. */
  @media (max-width: 480px) {
    .crumb .root, .crumb .sep { display: none; }
  }

  .icon-btn { width: 34px; height: 34px; border-radius: var(--bb-radius-md, 10px); display: flex; align-items: center; justify-content: center;
    background: none; border: 1px solid var(--rule, rgba(240, 236, 228, 0.1)); color: var(--bb-tan-light); cursor: pointer;
    transition: all var(--bb-dur-base) var(--bb-ease-out-expo); flex: none; }
  .icon-btn :global(svg) { width: 15px; height: 15px; stroke: currentColor; fill: none; stroke-width: 1.7; }
  .icon-btn:hover { border-color: var(--rule-tan, rgba(201, 168, 124, 0.45)); color: var(--bb-tan-pale); }
</style>
