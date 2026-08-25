<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The desktop nav rail, written in the console's Signal Ledger language:
  // one continuous track sheet of numbered destinations on full-bleed
  // hairlines — not a stack of rounded pills (an earlier cut used the dock's
  // wash-and-radius rows and read as a generic admin template next to the
  // ruled canvas). The active language is hardware, not fill: a 2px LED bar
  // at the rail's edge, the entry's index warming to tan, and the square
  // "signal present" dot the status-pill and node-list already use.
  //
  // A parent entry with children is a SPLIT row — the row itself is the link
  // to the section hub, the trailing toggle only expands. A disclosure-only
  // parent was tried first; it stranded the one destination everyone wants
  // (/commands) behind an extra click and forced a child duplicating the
  // parent's label so the page stayed reachable. No child duplicates a parent
  // href, so exactly one element per view carries aria-current="page".
  //
  // A pure renderer over the AppShell's groups: it invents no destinations
  // and derives no counts. It hides below 1024px where the floating Dock
  // takes over; see the breakpoint comment at the bottom for why that line
  // sits here.
  import { slide } from 'svelte/transition';
  import { expoOut } from 'svelte/easing';
  import Icon from './Icon.svelte';
  import type { NavGroupDef, NavLink } from '../lib/types';
  import { getI18n } from '../lib/i18n/context';

  const { t } = getI18n();

  let { groups }: { groups: NavGroupDef[] } = $props();

  // Expand state: explicit user toggles keyed by href win over the default;
  // absent keys fall through to "open iff self-or-child is active", so a page
  // load lands with the current section already expanded and a later reload
  // never re-collapses what the user left open (the record lives and dies with
  // this mount, the same scope as the dock's popover state).
  let toggled = $state<Record<string, boolean>>({});

  function hasActiveChild(it: NavLink): boolean {
    return !!it.children?.some((c) => c.active);
  }
  function isOpen(it: NavLink): boolean {
    return toggled[it.href] ?? (!!it.active || hasActiveChild(it));
  }
  function toggle(it: NavLink): void {
    toggled[it.href] = !isOpen(it);
  }

  // Continuous numbering across groups: the sheet counts destinations, not
  // groups (01..05 dashboard, 01..11 admin), so a row's number is stable when
  // a group above it grows. Subsection lines carry the parent's number plus
  // their own (03.1) — a subgroup address, the way a mixer labels bus sends.
  const starts = $derived.by(() => {
    let n = 1;
    return groups.map((g) => {
      const s = n;
      n += g.items.length;
      return s;
    });
  });
  const pad = (n: number) => String(n).padStart(2, '0');

  // href-derived so server and client agree; a render counter would hydrate-
  // mismatch the moment grant filtering drops an entry server-side.
  const subId = (href: string) => 'sub-' + href.replace(/[^a-z0-9]+/gi, '-');

  // Read once at init like Bolota does; behaviour-only, so SSR and hydration
  // render identical markup either way.
  const reduceMotion =
    typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;
</script>

{#snippet leaf(it: NavLink, n: string)}
  <a href={it.href} class="row" class:active={!!it.active} aria-current={it.active ? 'page' : undefined}>
    <span class="idx" aria-hidden="true">{n}</span>
    <Icon name={it.icon} size={14} />
    <span class="txt">{it.label}</span>
    {#if it.count}<span class="count" aria-hidden="true">{it.count}</span>{/if}
  </a>
{/snippet}

<aside class="sidebar" aria-label={t('nav.ariaMain')}>
  {#each groups as g, gi (g.label)}
    <section class="group">
      {#if g.label}<h2 class="eyebrow">{g.label}</h2>{/if}
      <ul class="items">
        {#each g.items as it, ii (it.href)}
          {@const n = starts[gi] + ii}
          <li style="--i:{n - 1}">
            {#if it.children?.length}
              {@const childActive = hasActiveChild(it)}
              {@const fullActive = !!it.active && !childActive}
              <div class="prow" class:open={isOpen(it)} class:tint={childActive}>
                <!-- The section hub stays a real link; only the toggle opens
                     the sheet. tint marks "you are somewhere inside" — the
                     child carries aria-current and the full marks. -->
                <a
                  href={it.href}
                  class="row"
                  class:active={fullActive}
                  aria-current={fullActive ? 'page' : undefined}
                >
                  <span class="idx" aria-hidden="true">{pad(n)}</span>
                  <Icon name={it.icon} size={14} />
                  <span class="txt">{it.label}</span>
                </a>
                <button
                  type="button"
                  class="toggle"
                  aria-expanded={isOpen(it)}
                  aria-controls={subId(it.href)}
                  aria-label={t('nav.subsectionsAria', { label: it.label })}
                  onclick={() => toggle(it)}
                >
                  <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M9 6l6 6-6 6" /></svg>
                </button>
              </div>
              {#if isOpen(it)}
                <ul
                  class="sub"
                  id={subId(it.href)}
                  transition:slide={{ duration: reduceMotion ? 0 : 260, easing: expoOut }}
                >
                  <!-- No icons on subsection lines: the indent, the decimal
                       index and the guide rule already say "child of 03";
                       an icon per line is noise at this width. -->
                  {#each it.children as c, ci (c.href)}
                    <li>
                      <a
                        href={c.href}
                        class="row subrow"
                        class:active={!!c.active}
                        aria-current={c.active ? 'page' : undefined}
                      >
                        <span class="idx" aria-hidden="true">{pad(n)}.{ci + 1}</span>
                        <span class="txt">{c.label}</span>
                      </a>
                    </li>
                  {/each}
                </ul>
              {/if}
            {:else}
              {@render leaf(it, pad(n))}
            {/if}
          </li>
        {/each}
      </ul>
    </section>
  {/each}
</aside>

<style>
  .sidebar {
    /* align-self:start + sticky keeps the rail pinned while the row it lives
       in grows with the page; overflow-y gives long group lists their own
       scroll instead of stretching the layout row. Pinned BELOW the topbar:
       height 100vh from the grid row put the first entries under the sticky
       strip (topbar is 55px, so rows scrolled to y=32 hid behind it) and
       pushed the last entries below the fold. */
    position: sticky;
    top: var(--topbar-h, 55px);
    align-self: start;
    height: calc(100vh - var(--topbar-h, 55px));
    display: flex;
    flex-direction: column;
    width: var(--sidebar-w, 216px);
    flex: none;
    padding: 4px 0 24px;
    background: rgba(10, 10, 10, 0.45);
    border-right: 1px solid var(--rule, rgba(201, 168, 124, 0.14));
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  /* Below 1024px the Dock owns navigation and this rail disappears: exactly
     one nav surface is ever visible per width, so two systems can never show
     disagreeing active state. 1024 because it is the smallest common desktop
     canvas (1280px) where a 216px rail still fits beside the 1160px reading
     column with gutters to spare — narrower, the rail would push content
     off-screen rather than add navigation. */
  @media (max-width: 1023px) {
    .sidebar { display: none; }
  }

  .group { display: flex; flex-direction: column; }

  /* Group eyebrow: the ledger's section stamp, ruled off to the rail's edge. */
  .eyebrow {
    display: flex;
    align-items: center;
    gap: 10px;
    margin: 0;
    padding: 18px 14px 8px;
    font-family: var(--bb-font-mono);
    font-size: 9px;
    font-weight: 500;
    letter-spacing: 0.22em;
    text-transform: uppercase;
    color: var(--bb-tan);
  }
  .eyebrow::after { content: ""; flex: 1; height: 1px; background: var(--rule, rgba(201, 168, 124, 0.14)); }

  /* Full-bleed hairlines: the rules run edge to edge like a channel strip;
     inset boxes are what turned the last version into a form. */
  .items { list-style: none; margin: 0; padding: 0; }
  .items > li {
    border-bottom: 1px solid rgba(201, 168, 124, 0.07);
    /* Channel-strip check-in: rows rise in order when the rail mounts, the
       same staged entrance the stat grid and feed use. */
    animation: rail-in 520ms var(--bb-ease-out-expo) both;
    animation-delay: calc(min(var(--i, 0), 14) * 45ms);
  }
  @keyframes rail-in {
    from { opacity: 0; transform: translateY(8px); }
    to { opacity: 1; transform: translateY(0); }
  }

  .row {
    position: relative;
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 11px 14px;
    border: none;
    background: none;
    color: var(--bb-muted);
    text-decoration: none;
    text-align: left;
    cursor: pointer;
    font-family: var(--bb-font-mono);
    font-weight: 500;
    font-size: 11px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    transition:
      color var(--bb-dur-fast, 180ms) ease,
      background var(--bb-dur-fast, 180ms) ease;
  }
  .row :global(svg) { width: 14px; height: 14px; stroke: currentColor; fill: none; stroke-width: 1.6; stroke-linecap: round; stroke-linejoin: round; flex: none; }
  .row:hover { color: var(--bb-white); background: rgba(201, 168, 124, 0.05); }
  .row:focus-visible,
  .toggle:focus-visible { outline: 2px solid var(--bb-focus, #e0c49a); outline-offset: -2px; z-index: 1; }
  .txt { min-width: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

  /* The track number: quiet until the row is live or hovered, then warm. */
  .idx {
    font-size: 9.5px;
    font-weight: 400;
    font-variant-numeric: tabular-nums;
    color: var(--bb-muted);
    opacity: 0.55;
    min-width: 15px;
    transition: color var(--bb-dur-fast, 180ms) ease, opacity var(--bb-dur-fast, 180ms) ease;
  }
  .row:hover .idx { opacity: 1; }

  /* The LED bar: this page, edge-lit. Hover gets a 30% preview of it, so the
     active mark is taught by the hover state instead of appearing from
     nowhere. */
  .row::before {
    content: "";
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
    width: 2px;
    background: var(--bb-green-glow);
    box-shadow: 0 0 8px rgba(82, 183, 136, 0.55);
    opacity: 0;
    transition: opacity var(--bb-dur-fast, 180ms) ease;
  }
  .row:hover::before { opacity: 0.3; }

  .row.active { color: var(--bb-white); }
  .row.active .idx { color: var(--bb-tan); opacity: 1; }
  .row.active::before { opacity: 1; }
  /* The square "signal present" dot — the mark status-pill and node-list
     already use, squared off like the legacy rail's channel light. Only the
     current page gets it; ancestors read as warmth, not signal. */
  .row.active::after {
    content: "";
    position: absolute;
    right: 13px;
    top: 50%;
    transform: translateY(-50%);
    width: 5px;
    height: 5px;
    background: var(--bb-green-glow);
    box-shadow: 0 0 8px var(--bb-green-glow);
  }
  .row.active .count { margin-right: 14px; }

  /* Ancestor of the current page: warm, but no bar and no dot. */
  .prow.tint .row { color: var(--bb-tan-light); }
  .prow.tint .row .idx { color: var(--bb-tan); opacity: 1; }

  .count {
    margin-left: auto;
    font-size: 10px;
    font-variant-numeric: tabular-nums;
    color: var(--bb-tan);
  }

  /* Split parent row: the anchor fills, the toggle is its own hit zone. */
  .prow { display: flex; align-items: stretch; }
  .prow .row { flex: 1; min-width: 0; }
  /* A parent's dot parks left of the toggle so the two right-edge marks
     never stack. */
  .prow .row.active::after { right: 6px; }

  .toggle {
    flex: none;
    width: 32px;
    display: flex;
    align-items: center;
    justify-content: center;
    border: none;
    background: none;
    color: var(--bb-muted);
    cursor: pointer;
    transition: color var(--bb-dur-fast, 180ms) ease;
  }
  .toggle:hover { color: var(--bb-tan-pale); }
  .toggle svg {
    width: 12px;
    height: 12px;
    stroke: currentColor;
    fill: none;
    stroke-width: 1.8;
    stroke-linecap: round;
    stroke-linejoin: round;
    transition: transform var(--bb-dur-base, 320ms) var(--bb-ease-out-expo);
  }
  .toggle[aria-expanded='true'] svg { transform: rotate(90deg); }
  .prow.tint .toggle { color: var(--bb-tan); }

  /* Subsection sheet: one guide rule off the parent's rail, decimal indices.
     The active jack is a short LED segment riding the guide, not a dot —
     inside the sheet the guide is the thing that lights. */
  .sub {
    list-style: none;
    margin: 0 0 2px 15px;
    padding: 0 0 0 11px;
    border-left: 1px solid rgba(201, 168, 124, 0.16);
    display: flex;
    flex-direction: column;
  }
  .subrow {
    font-size: 10px;
    letter-spacing: 0.08em;
    padding: 8px 14px 8px 12px;
  }
  .subrow .idx { font-size: 9px; min-width: 23px; opacity: 0.5; }
  .subrow.active { color: var(--bb-tan-pale); }
  .subrow.active .idx { color: var(--bb-tan); opacity: 1; }
  .subrow::before { display: none; }
  .subrow.active::after {
    content: "";
    position: absolute;
    left: -12px;
    right: auto;
    top: 50%;
    transform: translateY(-50%);
    width: 2px;
    height: 14px;
    background: var(--bb-green-glow);
    box-shadow: 0 0 8px var(--bb-green-glow);
  }

  @media (prefers-reduced-motion: reduce) {
    .items > li { animation: none; }
    .row, .row::before, .idx, .toggle, .toggle svg { transition: none; }
  }
</style>
