<script lang="ts">
  // The dock: navigation floats in a rounded bar at the bottom, the same
  // pattern at every breakpoint. It scales without bloating: when the app
  // declares more than one nav group (admin), each multi-item group collapses
  // into ONE dock button that opens a small popover of its pages — the dock
  // stays at a handful of buttons no matter how many routes exist. Any item
  // routed at "/" is hoisted out of its group as a direct Home button.
  import Icon from './Icon.svelte';
  import type { IconName } from '../lib/icons';
  import type { NavLink, NavGroupDef } from '../lib/types';

  let {
    items,
    groups = [] as NavGroupDef[],
    logout = true
  }: { items: NavLink[]; groups?: NavGroupDef[]; logout?: boolean } = $props();

  const GROUP_ICONS: Record<string, IconName> = {};
  function groupIcon(g: NavGroupDef): IconName {
    return GROUP_ICONS[g.label ?? ''] ?? g.items[0]?.icon ?? 'overview';
  }

  const grouped = $derived(groups.length > 1);

  // Grouped mode: hoist "/" as Home, drop groups it leaves empty.
  const home = $derived(
    grouped ? (groups.flatMap((g) => g.items).find((i) => i.href === '/') ?? null) : null
  );
  const dockGroups = $derived(
    grouped
      ? groups
          .map((g) => ({ ...g, items: g.items.filter((i) => i.href !== '/') }))
          .filter((g) => g.items.length > 0)
      : []
  );

  let openGroup = $state<string | null>(null);
  const toggleGroup = (label: string) => (openGroup = openGroup === label ? null : label);
  const groupActive = (g: NavGroupDef) => g.items.some((i) => i.active);
  const groupCount = (g: NavGroupDef) =>
    g.items.reduce((n, i) => n + (Number(i.count) || 0), 0) || undefined;

  function closeOnNav() {
    openGroup = null;
  }
</script>

<svelte:window
  onkeydown={(e) => {
    if (e.key === 'Escape') openGroup = null;
  }}
/>

{#if openGroup}
  <!-- Click-away scrim; Escape (svelte:window above) is the keyboard path. -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="dock-scrim"
    role="presentation"
    onclick={() => (openGroup = null)}
    onkeydown={(e) => { if (e.key === 'Enter') openGroup = null; }}
  ></div>
{/if}

<nav class="dock" aria-label="Main navigation">
  <div class="dock-inner">
    {#if grouped}
      {#if home}
        <a href={home.href} class="dock-item {home.active ? 'active' : ''}" aria-current={home.active ? 'page' : undefined} onclick={closeOnNav}>
          <Icon name={home.icon} size={18} />
          <span class="lbl">{home.label}</span>
        </a>
      {/if}
      {#each dockGroups as g (g.label)}
        {#if g.items.length === 1}
          {@const it = g.items[0]}
          <a href={it.href} class="dock-item {it.active ? 'active' : ''}" aria-current={it.active ? 'page' : undefined} onclick={closeOnNav}>
            <Icon name={it.icon} size={18} />
            <span class="lbl">{it.label}</span>
            {#if it.count}<span class="dock-count" aria-hidden="true">{it.count}</span>{/if}
          </a>
        {:else}
          <div class="group-wrap">
            <button
              type="button"
              class="dock-item {groupActive(g) ? 'active' : ''} {openGroup === g.label ? 'open' : ''}"
              aria-expanded={openGroup === g.label}
              aria-haspopup="menu"
              onclick={() => toggleGroup(g.label ?? '')}
            >
              <Icon name={groupIcon(g)} size={18} />
              <span class="lbl">{g.label}</span>
              {#if groupCount(g)}<span class="dock-count" aria-hidden="true">{groupCount(g)}</span>{/if}
            </button>
            {#if openGroup === g.label}
              <div class="popover" role="menu">
                {#each g.items as it (it.href)}
                  <a
                    href={it.href}
                    role="menuitem"
                    class="pop-item {it.active ? 'active' : ''}"
                    aria-current={it.active ? 'page' : undefined}
                    onclick={closeOnNav}
                  >
                    <Icon name={it.icon} size={16} />
                    <span>{it.label}</span>
                    {#if it.count}<span class="pop-count">{it.count}</span>{/if}
                  </a>
                {/each}
              </div>
            {/if}
          </div>
        {/if}
      {/each}
    {:else}
      {#each items as it (it.href)}
        <a href={it.href} class="dock-item {it.active ? 'active' : ''}" aria-current={it.active ? 'page' : undefined}>
          <Icon name={it.icon} size={18} />
          <span class="lbl">{it.label}</span>
          {#if it.count}<span class="dock-count" aria-hidden="true">{it.count}</span>{/if}
        </a>
      {/each}
    {/if}
    {#if logout}
      <span class="dock-rule" aria-hidden="true"></span>
      <form method="POST" action="/auth/logout" class="dock-form">
        <button type="submit" class="dock-item" title="Log out">
          <Icon name="power" size={18} />
          <span class="lbl">Log out</span>
        </button>
      </form>
    {/if}
  </div>
</nav>

<style>
  .dock {
    position: fixed;
    left: 0; right: 0; bottom: 0;
    z-index: 60;
    display: flex;
    justify-content: center;
    padding: 0 12px calc(14px + env(safe-area-inset-bottom));
    pointer-events: none;
  }
  .dock-scrim { position: fixed; inset: 0; z-index: 59; }
  .dock-inner {
    pointer-events: auto;
    display: flex;
    align-items: stretch;
    gap: 4px;
    padding: 7px;
    background: rgba(17, 17, 16, 0.92);
    border: 1px solid var(--bb-border, rgba(201, 168, 124, 0.15));
    border-radius: var(--bb-radius-lg, 16px);
    box-shadow: 0 18px 50px rgba(0, 0, 0, 0.55);
    animation: dock-in 520ms var(--bb-ease-out-expo) 120ms both;
    max-width: calc(100vw - 24px);
  }
  @keyframes dock-in {
    from { opacity: 0; transform: translateY(14px); }
    to { opacity: 1; transform: translateY(0); }
  }

  .group-wrap { position: relative; display: flex; }

  .dock-item {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 4px;
    min-width: 64px;
    padding: 8px 14px 7px;
    border-radius: var(--bb-radius-md, 10px);
    color: var(--bb-muted);
    text-decoration: none;
    cursor: pointer;
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: 10.5px;
    letter-spacing: 0.02em;
    border: none;
    background: none;
    transition: color var(--bb-dur-fast, 180ms) ease, background var(--bb-dur-fast, 180ms) ease, transform 200ms var(--bb-ease-out-back, ease);
    white-space: nowrap;
  }
  .dock-item :global(svg) { width: 18px; height: 18px; stroke: currentColor; fill: none; stroke-width: 1.6; stroke-linecap: round; stroke-linejoin: round; }
  .dock-item:hover { color: var(--bb-white); transform: translateY(-2px); }
  .dock-item:active { transform: translateY(0); }

  .dock-item.active,
  .dock-item.open {
    color: var(--bb-tan-pale);
    background: rgba(201, 168, 124, 0.14);
  }
  .dock-item.active::before {
    content: "";
    position: absolute;
    top: 5px;
    right: 8px;
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--bb-green-glow);
    box-shadow: 0 0 6px var(--bb-green-glow);
  }

  .dock-count {
    position: absolute;
    top: 2px;
    left: 8px;
    min-width: 15px;
    height: 15px;
    padding: 0 4px;
    border-radius: 999px;
    background: var(--bb-tan, #c9a87c);
    color: #0a0a0a;
    font-family: var(--bb-font-body);
    font-size: 9.5px;
    font-weight: 700;
    line-height: 15px;
    text-align: center;
  }

  /* group popover: a small card floating above its dock button */
  .popover {
    position: absolute;
    bottom: calc(100% + 12px);
    left: 50%;
    transform: translateX(-50%);
    min-width: 190px;
    padding: 6px;
    background: var(--bb-card-bg, #111110);
    border: 1px solid var(--bb-border-strong, rgba(201, 168, 124, 0.35));
    border-radius: var(--bb-radius-lg, 16px);
    box-shadow: 0 18px 50px rgba(0, 0, 0, 0.55);
    display: flex;
    flex-direction: column;
    gap: 2px;
    animation: pop-in 260ms var(--bb-ease-out-back, ease-out) both;
    z-index: 61;
  }
  @keyframes pop-in {
    from { opacity: 0; transform: translateX(-50%) translateY(8px) scale(0.96); }
    to { opacity: 1; transform: translateX(-50%) translateY(0) scale(1); }
  }

  .pop-item {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    border-radius: var(--bb-radius-md, 10px);
    color: var(--bb-muted);
    text-decoration: none;
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: 13px;
    transition: color var(--bb-dur-fast, 180ms) ease, background var(--bb-dur-fast, 180ms) ease;
  }
  .pop-item :global(svg) { width: 16px; height: 16px; stroke: currentColor; fill: none; stroke-width: 1.6; stroke-linecap: round; stroke-linejoin: round; }
  .pop-item:hover { color: var(--bb-white); background: rgba(201, 168, 124, 0.1); }
  .pop-item.active { color: var(--bb-tan-pale); background: rgba(201, 168, 124, 0.14); }
  .pop-count {
    margin-left: auto;
    min-width: 18px;
    padding: 1px 6px;
    border-radius: 999px;
    background: var(--bb-tan, #c9a87c);
    color: #0a0a0a;
    font-size: 10.5px;
    font-weight: 700;
    text-align: center;
  }

  .dock-rule { width: 1px; background: var(--bb-border, rgba(201, 168, 124, 0.15)); margin: 8px 4px; }
  .dock-form { display: flex; }

  @media (max-width: 760px) {
    .dock { padding: 0 8px calc(8px + env(safe-area-inset-bottom)); }
    /* Full-bleed veil behind the pill: fades the scrolling page out and paints
       the canvas colour solid across the home-indicator strip, so nothing shows
       through the gaps around the floating dock. */
    .dock::before {
      content: "";
      position: absolute;
      inset: -22px 0 0;
      background: linear-gradient(
        to top,
        var(--bb-bg-0) 0%,
        var(--bb-bg-0) 46%,
        rgba(10, 10, 10, 0.72) 72%,
        transparent 100%
      );
      pointer-events: none;
      z-index: -1;
    }
    .dock-inner { width: 100%; justify-content: space-around; }
    .dock-item { min-width: 0; flex: 1; padding: 8px 6px 7px; }
    .group-wrap { flex: 1; }
    .group-wrap .dock-item { width: 100%; }
  }
  @media (max-width: 400px) {
    .dock-item .lbl { display: none; }
  }
  @media (prefers-reduced-motion: reduce) {
    .dock-inner, .popover { animation: none; }
    .dock-item:hover { transform: none; }
  }
</style>
