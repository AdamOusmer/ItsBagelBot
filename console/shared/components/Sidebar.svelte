<script lang="ts">
  import Brand from './Brand.svelte';
  import NavGroup from './NavGroup.svelte';
  import AccountFoot from './AccountFoot.svelte';
  import type { NavGroupDef } from '../lib/types';
  let { brandTitle = 'ItsBagelBot', brandSub, groups, accountName, accountRole }:
    { brandTitle?: string; brandSub: string; groups: NavGroupDef[]; accountName: string; accountRole: string } = $props();

  // Continuous item numbering across groups (01, 02, ... like a track sheet).
  const starts = $derived.by(() => {
    let n = 1;
    return groups.map((g) => {
      const s = n;
      n += g.items.length;
      return s;
    });
  });
</script>

<aside class="sidebar">
  <Brand title={brandTitle} sub={brandSub} />
  {#each groups as g, gi}
    <NavGroup label={g.label} items={g.items} startIndex={starts[gi]} />
  {/each}
  <div class="side-spacer"></div>
  <AccountFoot name={accountName} role={accountRole} />
</aside>

<style>
  /* Flat ink rail: a single right rule separates it from the canvas.
     No glass, no shadow — the indexed entries carry the structure. */
  .sidebar {
    position: sticky; top: 0; align-self: start; height: 100vh;
    display: none; flex-direction: column;
    padding: 20px 14px;
    background: rgba(10, 10, 10, 0.55);
    border-right: 1px solid var(--rule, rgba(240, 236, 228, 0.1));
  }
  @media (min-width: 761px) { .sidebar { display: flex; } }
  .side-spacer { flex: 1; }
</style>
