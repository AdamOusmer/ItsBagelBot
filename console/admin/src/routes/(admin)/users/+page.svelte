<script lang="ts">
  import { enhance } from '$app/forms';
  import { Icon, StatTile, Button } from '@bagel/shared';
  import type { AdminUserWire } from '$lib/server/rpc';
  let { data, form } = $props();

  const lookup = $derived(form?.lookup as Record<string, unknown> | undefined);
  const found = $derived(lookup?.user as AdminUserWire | undefined);
  const lookupError = $derived(lookup?.error as string | undefined);
  const tokenPresent = $derived(Boolean(lookup?.tokenPresent));
  const action = $derived(form?.action as { ok: boolean; notice: string } | undefined);

  function tier(status: string): 'premium' | 'standard' {
    return status === 'paid' || status === 'vip' ? 'premium' : 'standard';
  }
</script>

<section class="screen active">
  <div class="page-head">
    <span class="eyebrow">Broadcaster accounts</span>
    <h1>User <em>management</em></h1>
    <p>
      Grants, resets, and recent users.{#if data.degraded}
        <em> Live user data unavailable; showing sample.</em>{/if}
    </p>
  </div>

  <div class="stat-grid">
    <StatTile icon="users" label="Registered" value={data.stats.total_users.toLocaleString()} unit="total" delta={`${data.stats.active_users} active`} flat />
    <StatTile icon="pulse" tan label="Premium" value={data.stats.premium_users.toLocaleString()} unit="" delta="paid + vip" flat />
    <StatTile icon="check" label="VIP" value={data.stats.vip_users.toLocaleString()} unit="" delta="comped" flat />
    <StatTile icon="commands" tan label="Paid" value={data.stats.paid_users.toLocaleString()} unit="" delta="subscribers" flat />
  </div>

  <div class="card">
    <div class="card-head"><h3>Lookup</h3></div>
    <form method="POST" action="?/lookup" use:enhance style="display:flex;gap:.6rem;flex-wrap:wrap">
      <label class="search" style="flex:1;min-width:240px">
        <Icon name="search" size={15} />
        <input name="q" type="text" placeholder="Twitch user id or username" autocomplete="off" />
      </label>
      <Button variant="primary" icon="search" type="submit">Look up</Button>
    </form>

    {#if lookupError}
      <p class="text-muted" style="font-size:.85rem;margin-top:.8rem">{lookupError}</p>
    {/if}

    {#if found}
      <div class="card" style="margin-top:1rem">
        <div class="card-head">
          <h3>@{found.username}</h3>
          <span class="more">id {found.id} · {tier(found.status)}</span>
        </div>
        <div class="meta" style="margin-bottom:.8rem">
          <span>status {found.status}</span><span class="mid">·</span>
          <span>{found.is_active ? 'active' : 'inactive'}</span><span class="mid">·</span>
          <span>token {tokenPresent ? 'present' : 'absent'}</span>
        </div>

        {#if action}
          <p class="text-muted" style="font-size:.82rem;margin-bottom:.6rem">{action.notice}</p>
        {/if}

        <div class="actions" style="flex-wrap:wrap;gap:.5rem">
          {#each ['free', 'paid', 'vip'] as s}
            <form method="POST" action="?/setStatus" use:enhance>
              <input type="hidden" name="user_id" value={found.id} />
              <input type="hidden" name="status" value={s} />
              <button class="btn ghost" type="submit" disabled={found.status === s}>Set {s}</button>
            </form>
          {/each}
          <form method="POST" action="?/reset" use:enhance>
            <input type="hidden" name="user_id" value={found.id} />
            <button class="btn ghost" type="submit">Reset</button>
          </form>
          <form method="POST" action="?/clearToken" use:enhance>
            <input type="hidden" name="user_id" value={found.id} />
            <button class="btn ghost" type="submit">Clear token</button>
          </form>
        </div>
      </div>
    {/if}
  </div>

  <div class="card" style="padding:18px 6px">
    <div class="card-head" style="padding:0 12px"><h3>Recent</h3></div>
    <div class="table">
      <div class="thead">
        <span>User</span><span>Id</span><span class="perm-cell">Tier</span><span>Status</span><span>Active</span><span></span>
      </div>
      <div class="trows">
        {#each data.recent as u (u.id)}
          <div class="trow">
            <span class="cmd">@{u.username}</span>
            <span class="resp">{u.id}</span>
            <span class="perm-cell"><span class="badge {tier(u.status) === 'premium' ? 'sub' : 'everyone'}">{tier(u.status)}</span></span>
            <span class="cd">{u.status}</span>
            <span class="uses">{u.is_active ? 'yes' : 'no'}</span>
            <span class="row-act"></span>
          </div>
        {/each}
      </div>
    </div>
  </div>
</section>
