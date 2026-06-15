<script lang="ts">
  import { Icon, Badge, Toggle, Button } from '@bagel/shared';
  import type { Perm } from '@bagel/shared';
  let { data } = $props();

  const filters = ['All', 'Custom', 'Built-in', 'Disabled'] as const;
  let active = $state<(typeof filters)[number]>('All');

  const rows = $derived(
    data.commands.filter((c) =>
      active === 'Disabled' ? !c.is_active : active === 'All' ? true : true
    )
  );
</script>

<section class="screen active">
  <div class="page-head">
    <span class="eyebrow">Manage</span>
    <h1>Chat <em>commands</em></h1>
    <p>Custom responses your viewers can trigger in chat. {data.commands.filter((c) => c.is_active).length} active, {data.commands.filter((c) => !c.is_active).length} disabled.</p>
  </div>

  <div class="toolbar">
    <div class="chip-row">
      {#each filters as f}
        <button class="chip {active === f ? 'on' : ''}" onclick={() => (active = f)}>{f}</button>
      {/each}
    </div>
    <div class="grow"></div>
    <label class="search" style="width:200px">
      <Icon name="search" size={15} />
      <input type="text" placeholder="Filter commands…" />
    </label>
    <Button variant="primary" icon="plus">New command</Button>
  </div>

  <div class="card" style="padding:18px 6px">
    <div class="table">
      <div class="thead">
        <span>Command</span><span>Response</span><span class="perm-cell">Access</span><span>Cooldown</span><span>Uses</span><span></span>
      </div>
      <div class="trows">
        {#each rows as c (c.name)}
          <div class="trow {c.is_active ? '' : 'off'}" style={c.is_active ? '' : 'opacity:.55'}>
            <span class="cmd">{c.name}</span>
            <span class="resp">{c.response}</span>
            <span class="perm-cell"><Badge perm={(c.perm ?? 'everyone') as Perm} /></span>
            <span class="cd">{c.cooldown ?? '0s'}</span>
            <span class="uses">{c.uses ?? '0'}</span>
            <span class="row-act">
              <Toggle on={c.is_active} />
              <button class="mini" aria-label="Edit"><Icon name="edit" size={15} /></button>
            </span>
          </div>
        {/each}
      </div>
    </div>
  </div>
</section>
