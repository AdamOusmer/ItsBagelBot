<script lang="ts">
  import Brand from './Brand.svelte';
  import NavGroup from './NavGroup.svelte';
  import AccountFoot from './AccountFoot.svelte';
  import type { NavGroupDef } from '../lib/types';
  let { brandTitle = 'ItsBagelBot', brandSub, groups, accountName, accountRole }:
    { brandTitle?: string; brandSub: string; groups: NavGroupDef[]; accountName: string; accountRole: string } = $props();
</script>

<aside class="sidebar">
  <Brand title={brandTitle} sub={brandSub} />
  {#each groups as g}
    <NavGroup label={g.label} items={g.items} />
  {/each}
  <div class="side-spacer"></div>
  <AccountFoot name={accountName} role={accountRole} />
</aside>

<style>
  .sidebar {
    position: sticky; top: 0; align-self: start; height: 100vh;
    display: none; flex-direction: column;
    padding: 22px 16px;
    background: var(--glass-fill);
    border-right: 1px solid var(--glass-border);
    backdrop-filter: blur(var(--glass-blur)) saturate(var(--glass-sat)) brightness(var(--glass-bright));
    -webkit-backdrop-filter: blur(var(--glass-blur)) saturate(var(--glass-sat)) brightness(var(--glass-bright));
    box-shadow: var(--glass-rim);
  }
  @media (min-width: 761px) { .sidebar { display: flex; } }
  .side-spacer { flex: 1; }
</style>
