<script lang="ts">
  import { enhance } from '$app/forms';
  import { Icon, Badge } from '@bagel/shared';
  import type { Perm } from '@bagel/shared';
  let { data, form } = $props();

  // Action results return the fresh list; fall back to the loaded data.
  const commands = $derived(form?.commands ?? data.commands);

  const filters = ['All', 'Custom', 'Built-in', 'Disabled'] as const;
  let active = $state<(typeof filters)[number]>('All');
  let showNew = $state(false);

  const rows = $derived(
    commands.filter((c) => (active === 'Disabled' ? !c.is_active : true))
  );
</script>

<section class="screen active">
  <div class="page-head">
    <span class="eyebrow">Manage</span>
    <h1>Chat <em>commands</em></h1>
    <p>Custom responses your viewers can trigger in chat. {commands.filter((c) => c.is_active).length} active, {commands.filter((c) => !c.is_active).length} disabled.</p>
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
    <button class="btn primary" onclick={() => (showNew = !showNew)}>
      <Icon name="plus" size={14} /> New command
    </button>
  </div>

  {#if showNew}
    <form method="POST" action="?/save" use:enhance={() => async ({ update }) => { await update(); showNew = false; }} class="card" style="padding:16px;margin-bottom:14px;display:flex;gap:12px;align-items:center;flex-wrap:wrap">
      <input class="search" name="name" placeholder="!command" required style="width:160px" />
      <input class="search" name="response" placeholder="Response text…" required style="flex:1;min-width:220px" />
      <input type="hidden" name="is_active" value="on" />
      <button class="btn primary" type="submit"><Icon name="check" size={14} /> Save</button>
    </form>
  {/if}

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
              <form method="POST" action="?/save" use:enhance>
                <input type="hidden" name="name" value={c.name} />
                <input type="hidden" name="response" value={c.response} />
                <input type="hidden" name="is_active" value={c.is_active ? '' : 'on'} />
                <button class="toggle {c.is_active ? 'on' : ''}" type="submit" aria-label="Toggle"></button>
              </form>
              <form method="POST" action="?/delete" use:enhance>
                <input type="hidden" name="name" value={c.name} />
                <button class="mini" type="submit" aria-label="Delete"><Icon name="trash" size={15} /></button>
              </form>
            </span>
          </div>
        {/each}
      </div>
    </div>
  </div>
</section>
