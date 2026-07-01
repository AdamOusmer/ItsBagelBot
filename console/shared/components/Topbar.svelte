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

  // Local wall-clock readout — the strip's "master control" pulse.
  let now = $state('');
  $effect(() => {
    const fmt = () => (now = new Date().toLocaleTimeString(undefined, { hour12: false }));
    fmt();
    const t = setInterval(fmt, 1000);
    return () => clearInterval(t);
  });
</script>

<header class="topbar">
  <a class="station" href="/">
    <img src="/logo.png" alt="" />
    <span class="station-id">
      <b>{brandTitle}</b>
      {#if brandSub}<i>{brandSub}</i>{/if}
    </span>
  </a>

  <div class="crumb">
    <span class="sig">SIG</span>
    <span class="root">{root}</span><span class="sep">/</span><span class="here">{crumb}</span>
  </div>

  <div class="grow"></div>

  <span class="clock" aria-hidden="true">{now}</span>

  {#if accountName}
    <span class="operator" title="{accountName} · {accountRole}">
      <span class="avatar">{initial}</span>
      <span class="op-id">
        <b>{accountName}</b>
        <i>{accountRole}</i>
      </span>
    </span>
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
    padding: 9px 16px;
    background: rgba(10, 10, 10, 0.85);
    border-bottom: 1px solid var(--rule, rgba(240, 236, 228, 0.1));
  }
  @media (min-width: 761px) {
    .topbar { gap: 20px; padding: 9px var(--gutter); }
  }

  .station { display: flex; align-items: center; gap: 9px; text-decoration: none; flex: none; }
  .station img { width: 24px; height: 24px; border-radius: 2px; }
  .station-id { display: flex; flex-direction: column; line-height: 1; }
  .station-id b { font-family: var(--bb-font-display); font-weight: 800; font-size: 13px; letter-spacing: -0.01em; color: var(--bb-white); }
  .station-id i { font-style: normal; font-family: var(--bb-font-mono); font-size: 8px; letter-spacing: 0.24em; text-transform: uppercase; color: var(--bb-tan); margin-top: 3px; }

  .crumb { font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.14em; text-transform: uppercase; color: var(--bb-muted); display: flex; align-items: center; gap: 10px; min-width: 0; }
  .sig {
    color: var(--bb-green-glow); font-size: 9px; letter-spacing: 0.2em;
    border: 1px solid rgba(82, 183, 136, 0.35); padding: 2px 6px; border-radius: 2px; flex: none;
  }
  .crumb .sep { opacity: 0.45; }
  .crumb .here { color: var(--bb-tan); white-space: nowrap; }
  .crumb .root { white-space: nowrap; }

  .grow { flex: 1; }

  .clock {
    font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.12em;
    color: var(--bb-muted); font-variant-numeric: tabular-nums;
    display: none;
  }

  .operator { display: none; align-items: center; gap: 9px; }
  .avatar {
    width: 26px; height: 26px; border-radius: 2px; flex: none;
    background: linear-gradient(135deg, var(--bb-green-light), var(--bb-tan));
    display: flex; align-items: center; justify-content: center;
    font-family: var(--bb-font-display); font-weight: 800; font-size: 12px; color: #0a0a0a;
  }
  .op-id { display: flex; flex-direction: column; line-height: 1; }
  .op-id b { font-family: var(--bb-font-body); font-weight: 600; font-size: 12px; color: var(--bb-white); max-width: 140px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .op-id i { font-style: normal; font-family: var(--bb-font-mono); font-size: 8.5px; letter-spacing: 0.16em; text-transform: uppercase; color: var(--bb-tan); margin-top: 3px; }

  @media (min-width: 761px) {
    .clock { display: inline; }
    .operator { display: flex; }
  }
  /* On phones the station id doubles as the crumb root, so hide the root. */
  @media (max-width: 480px) {
    .crumb .root, .crumb .sep { display: none; }
  }

  .icon-btn { width: 32px; height: 32px; border-radius: 2px; display: flex; align-items: center; justify-content: center;
    background: none; border: 1px solid var(--rule, rgba(240, 236, 228, 0.1)); color: var(--bb-tan-light); cursor: pointer;
    transition: all var(--bb-dur-base) var(--bb-ease-out-expo); flex: none; }
  .icon-btn :global(svg) { width: 15px; height: 15px; stroke: currentColor; fill: none; stroke-width: 1.7; }
  .icon-btn:hover { border-color: var(--rule-tan, rgba(201, 168, 124, 0.45)); color: var(--bb-tan-pale); }
</style>
