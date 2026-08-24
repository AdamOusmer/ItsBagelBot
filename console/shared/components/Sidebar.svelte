<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The desktop nav rail: the dock's grouped popover unfolded into a sticky
  // left column — same rows, same active language — plus room for one level of
  // subsection disclosures via NavLink.children. A pure renderer over the
  // AppShell's groups: it invents no destinations and derives no counts. It
  // hides itself below 1024px where the floating Dock takes over; see the
  // breakpoint comment at the bottom for why that line sits here.
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
</script>

{#snippet link(it: NavLink)}
  <a href={it.href} class="row" class:active={!!it.active} aria-current={it.active ? 'page' : undefined}>
    <Icon name={it.icon} size={15} />
    <span class="txt">{it.label}</span>
    {#if it.count}<span class="count" aria-hidden="true">{it.count}</span>{/if}
  </a>
{/snippet}

<aside class="sidebar" aria-label={t('nav.ariaMain')}>
  {#each groups as g (g.label)}
    <section class="group">
      {#if g.label}<h2 class="label">{g.label}</h2>{/if}
      <ul class="items">
        {#each g.items as it (it.href)}
          <li>
            {#if it.children?.length}
              <!-- A parent row is a folder, not a destination: a disclosure
                   button, not a link, so exactly one element per view carries
                   aria-current. Its own active state reads as a tint only. -->
              <button
                type="button"
                class="row disc"
                class:tint={!!it.active || hasActiveChild(it)}
                aria-expanded={isOpen(it)}
                onclick={() => toggle(it)}
              >
                <Icon name={it.icon} size={15} />
                <span class="txt">{it.label}</span>
                <svg class="chev" viewBox="0 0 24 24" aria-hidden="true"><path d="M9 6l6 6-6 6" /></svg>
              </button>
              {#if isOpen(it)}
                <ul class="sub">
                  {#each it.children as c (c.href)}
                    <li>{@render link(c)}</li>
                  {/each}
                </ul>
              {/if}
            {:else}
              {@render link(it)}
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
       scroll instead of stretching the layout row. */
    position: sticky;
    top: 0;
    align-self: start;
    height: 100vh;
    display: flex;
    flex-direction: column;
    gap: 20px;
    width: var(--sidebar-w, 248px);
    flex: none;
    padding: 16px 12px 24px;
    background: rgba(10, 10, 10, 0.45);
    border-right: 1px solid var(--rule, rgba(240, 236, 228, 0.1));
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  /* Below 1024px the Dock owns navigation and this rail disappears: exactly
     one nav surface is ever visible per width, so two systems can never show
     disagreeing active state. 1024 because it is the smallest common desktop
     canvas (1280px) where a 248px rail still fits beside the 1160px reading
     column with gutters to spare — narrower, the rail would push content
     off-screen rather than add navigation. */
  @media (max-width: 1023px) {
    .sidebar { display: none; }
  }

  .group { display: flex; flex-direction: column; }
  .label {
    margin: 0 0 6px;
    padding: 0 10px;
    font-family: var(--bb-font-mono);
    font-size: 9.5px;
    font-weight: 400;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--bb-tan);
  }
  .items { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 1px; }

  .row {
    position: relative;
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 8px 10px;
    border-radius: 8px 8px;
    border: none;
    background: none;
    color: var(--bb-muted);
    text-decoration: none;
    text-align: left;
    cursor: pointer;
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: 13px;
    transition: color var(--bb-dur-fast, 180ms) ease, background var(--bb-dur-fast, 180ms) ease;
  }
  .row :global(svg) { width: 15px; height: 15px; stroke: currentColor; fill: none; stroke-width: 1.6; stroke-linecap: round; stroke-linejoin: round; flex: none; }
  .row:hover { color: var(--bb-white); background: rgba(201, 168, 124, 0.08); }
  .row:focus-visible,
  .sidebar :global(:focus-visible) { outline: 2px solid var(--bb-green-glow, #52b788); outline-offset: 2px; }
  .txt { min-width: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .count {
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

  /* Active language mirrors the dock's popover rows: tan wash + pale ink, and
     the glowing dot marking THIS page (the tint alone marks an ancestor). */
  .row.active,
  .row.tint { color: var(--bb-tan-pale); background: rgba(201, 168, 124, 0.14); }
  .row.active::after {
    content: "";
    position: absolute;
    right: 8px;
    top: 50%;
    transform: translateY(-50%);
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--bb-green-glow);
    box-shadow: 0 0 6px var(--bb-green-glow);
  }

  /* .row prefix out-specifies the .row :global(svg) sizing above without
     resorting to !important (stroke etc. are identical anyway). */
  .row .chev {
    width: 14px;
    height: 14px;
    margin-left: auto;
    transition: transform var(--bb-dur-fast, 180ms) ease;
  }
  .disc[aria-expanded='true'] .chev { transform: rotate(90deg); }

  .sub {
    list-style: none;
    margin: 2px 0 4px 17px;
    padding: 0 0 0 8px;
    border-left: 1px solid var(--bb-border, rgba(201, 168, 124, 0.15));
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .sub .row { font-size: 12.5px; padding: 6px 8px; }

  @media (prefers-reduced-motion: reduce) {
    .row, .chev { transition: none; }
  }
</style>
