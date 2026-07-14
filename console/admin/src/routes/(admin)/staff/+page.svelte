<script lang="ts">
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Icon,
    Button,
    PageHead,
    PageToolbar,
    AlertBanner,
    DeckList,
    EmptyState,
    ConfirmDialog,
    Scroller,
    Skeleton,
    toast
  } from '@bagel/shared';
  import type { AdminAcct, AdminRole, AuditEntry } from '$lib/server/services';

  let { data } = $props();

  // Streamed roster -> local state, so mutations can reconcile against the
  // authoritative roster echoed by the users service (never a local guess).
  let staff = $state<AdminAcct[]>([]);
  let rosterLoaded = $state(false);
  let degraded = $state(false);
  $effect(() => {
    let alive = true;
    rosterLoaded = false;
    data.roster.then((r) => {
      if (!alive) return;
      staff = r.staff;
      degraded = r.degraded;
      rosterLoaded = true;
    });
    return () => {
      alive = false;
    };
  });

  const me = $derived(data.me);
  const RANK: Record<AdminRole, number> = { moderator: 1, admin: 2, owner: 3 };
  // Mirror of the server ladder: owners manage anyone; admins manage below owner.
  function canManage(target: AdminRole): boolean {
    if (me.role === 'owner') return true;
    if (me.role !== 'admin') return false;
    return target !== 'owner';
  }
  function grantableRoles(): AdminRole[] {
    return me.role === 'owner' ? ['moderator', 'admin', 'owner'] : ['moderator', 'admin'];
  }
  const roster = $derived(
    [...staff].sort(
      (a, b) => (RANK[b.role] ?? 0) - (RANK[a.role] ?? 0) || a.login.localeCompare(b.login)
    )
  );

  type ActionPayload = {
    action?: { ok: boolean; notice: string };
    staff?: AdminAcct[];
    error?: string;
  };
  function payloadOf(result: unknown): ActionPayload | undefined {
    const r = result as { type: string; data?: ActionPayload };
    return r.type === 'success' || r.type === 'failure' ? r.data : undefined;
  }

  let busy = $state(false);

  function rosterAction(after?: () => void): SubmitFunction {
    return () => {
      busy = true;
      const before = staff.map((s) => ({ ...s }));
      return async ({ result, update }) => {
        busy = false;
        after?.();
        const p = payloadOf(result);
        if (result.type === 'success' && p?.action?.ok) {
          toast('ok', p.action.notice);
          if (p.staff) staff = p.staff;
          await update({ reset: true });
          return;
        }
        staff = before;
        toast('err', p?.action?.notice ?? p?.error ?? 'roster change failed');
      };
    };
  }

  let addOpen = $state(false);
  const addSubmit = rosterAction(() => (addOpen = false));

  // Role change: optimistic flip, echoed roster reconciles, failure reverts.
  let roleForms = $state<Record<string, HTMLFormElement | null>>({});
  let roleDraft = $state<Record<string, AdminRole>>({});
  function changeRole(member: AdminAcct, role: AdminRole) {
    if (member.role === role) return;
    roleDraft[String(member.id)] = role;
    const i = staff.findIndex((s) => s.id === member.id);
    if (i >= 0) staff[i] = { ...staff[i], role };
    queueMicrotask(() => roleForms[String(member.id)]?.requestSubmit());
  }
  const roleSubmit = rosterAction();

  let removeTarget = $state<AdminAcct | null>(null);
  let removeForm = $state<HTMLFormElement | null>(null);
  const removeSubmit = rosterAction(() => (removeTarget = null));

  // ── Per-member history (lazy drawer) ───────────────────────────────────────
  let historyFor = $state<AdminAcct | null>(null);
  let history = $state<AuditEntry[] | null>(null);
  let historyError = $state('');

  async function openHistory(member: AdminAcct) {
    historyFor = member;
    history = null;
    historyError = '';
    try {
      const res = await fetch(`/staff/history?actor_id=${member.id}`);
      if (!res.ok) throw new Error(`history fetch failed (${res.status})`);
      const body = (await res.json()) as { entries?: AuditEntry[]; error?: string };
      if (body.error) throw new Error(body.error);
      history = body.entries ?? [];
    } catch (e) {
      historyError = (e as Error).message;
      history = [];
    }
  }

  function closeHistory() {
    historyFor = null;
    history = null;
    historyError = '';
  }

  function ago(iso: string): string {
    const mins = Math.max(Math.round((Date.now() - new Date(iso).getTime()) / 60e3), 0);
    if (mins < 60) return `${mins}m ago`;
    const hours = Math.round(mins / 60);
    if (hours < 48) return `${hours}h ago`;
    return `${Math.round(hours / 24)}d ago`;
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && historyFor) closeHistory();
  }
</script>

<section class="screen active">
  <PageHead eyebrow="Access control" description="Who can operate this console, and what they did with it.">
    Staff <em>roster</em>
  </PageHead>

  {#if degraded}
    <AlertBanner>Roster service unreachable; nothing below is live.</AlertBanner>
  {/if}

  <PageToolbar>
    {#snippet lead()}
      {#if rosterLoaded}
        <span class="roster-count">{roster.length} member{roster.length === 1 ? '' : 's'}</span>
      {:else}
        <Skeleton variant="pill" width="110px" />
      {/if}
    {/snippet}
    {#snippet trail()}
      <Button variant="primary" icon="plus" onclick={() => (addOpen = !addOpen)}>Add member</Button>
    {/snippet}
  </PageToolbar>

  {#if addOpen}
    <div class="card add-card">
      <div class="card-head"><h3>Add staff member</h3></div>
      <form method="POST" action="?/upsert" use:enhance={addSubmit} class="add-form">
        <label>
          Twitch user id
          <input class="text-input" type="text" name="user_id" inputmode="numeric" pattern="[0-9]+" required placeholder="804932984" />
        </label>
        <label>
          Login
          <input class="text-input" type="text" name="login" required placeholder="itsmavey" />
        </label>
        <label>
          Display name
          <input class="text-input" type="text" name="display_name" placeholder="(defaults to login)" />
        </label>
        <label>
          Role
          <select class="text-input" name="role">
            {#each grantableRoles() as r (r)}
              <option value={r}>{r}</option>
            {/each}
          </select>
        </label>
        <div class="add-actions">
          <Button variant="primary" type="submit" disabled={busy}>{busy ? 'Adding…' : 'Add'}</Button>
          <Button variant="ghost" type="button" onclick={() => (addOpen = false)}>Cancel</Button>
        </div>
      </form>
    </div>
  {/if}

  <div class="deck">
    <DeckList>
      {#if !rosterLoaded}
        <div class="row-skeletons">
          {#each [0, 1, 2] as i (i)}<Skeleton variant="block" height="60px" />{/each}
        </div>
      {:else if roster.length}
        <ul class="list" aria-label="Staff">
          {#each roster as member (member.id)}
            <li class="staff-row">
              <span class="avatar">{member.login.slice(0, 1).toUpperCase()}</span>
              <div class="who">
                <span class="login">
                  {member.display_name || member.login}
                  {#if member.id === Number(me.id)}<span class="you">you</span>{/if}
                </span>
                <span class="sub">@{member.login} · #{member.id} · added {ago(member.created_at)}</span>
              </div>
              {#if canManage(member.role) && member.id !== Number(me.id)}
                <select
                  class="role-select role-{member.role}"
                  value={member.role}
                  aria-label="Role for {member.login}"
                  disabled={busy}
                  onchange={(e) => changeRole(member, (e.currentTarget as HTMLSelectElement).value as AdminRole)}
                >
                  {#each grantableRoles() as r (r)}
                    <option value={r}>{r}</option>
                  {/each}
                </select>
              {:else}
                <span class="role-pill role-{member.role}">{member.role}</span>
              {/if}
              <span class="row-actions">
                <button
                  class="mini-act"
                  type="button"
                  title="History"
                  aria-label="History for {member.login}"
                  onclick={() => openHistory(member)}
                >
                  <Icon name="audit" size={14} />
                </button>
                {#if canManage(member.role) && member.id !== Number(me.id)}
                  <button
                    class="mini-act danger"
                    type="button"
                    title="Remove"
                    aria-label="Remove {member.login}"
                    onclick={() => (removeTarget = member)}
                  >
                    <Icon name="trash" size={14} />
                  </button>
                {/if}
              </span>
              <form
                method="POST"
                action="?/upsert"
                use:enhance={roleSubmit}
                bind:this={roleForms[String(member.id)]}
                hidden
              >
                <input type="hidden" name="user_id" value={member.id} />
                <input type="hidden" name="login" value={member.login} />
                <input type="hidden" name="display_name" value={member.display_name} />
                <input type="hidden" name="role" value={roleDraft[String(member.id)] ?? member.role} />
              </form>
            </li>
          {/each}
        </ul>
      {:else}
        <EmptyState icon="moderation" title="No staff yet" body="Add the first member with their Twitch user id." />
      {/if}
    </DeckList>

    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="inspector-backdrop"
      class:open={historyFor !== null}
      role="presentation"
      onclick={closeHistory}
      onkeydown={(e) => {
        if (e.key === 'Enter') closeHistory();
      }}
    ></div>
    <aside class="inspector" class:open={historyFor !== null} aria-label="Member history">
      <div class="inspector-head">
        <span class="inspector-tag">{historyFor ? `@${historyFor.login} — history` : 'History'}</span>
        {#if historyFor}
          <button class="mini" type="button" aria-label="Close" onclick={closeHistory}>
            <Icon name="x" size={14} />
          </button>
        {/if}
      </div>
      {#if historyFor}
        <Scroller fill padding="14px" data-lenis-prevent>
          {#if history === null}
            <p class="hist-note">Loading history…</p>
          {:else if historyError}
            <p class="hist-note err">{historyError}</p>
          {:else if history.length === 0}
            <p class="hist-note">No recorded actions.</p>
          {:else}
            <ul class="hist-list">
              {#each history as e (e.id)}
                <li class="hist-row">
                  <span class="hdot {e.ok ? '' : 'err'}"></span>
                  <div class="hist-body">
                    <span class="hact">{e.action}{e.target ? ` → ${e.target}` : ''}</span>
                    {#if e.detail}<span class="hdetail">{e.detail}</span>{/if}
                    {#if !e.ok && e.error}<span class="hdetail err">{e.error}</span>{/if}
                  </div>
                  <span class="hwhen">{ago(e.created_at)}</span>
                </li>
              {/each}
            </ul>
          {/if}
        </Scroller>
      {:else}
        <div class="inspector-idle">
          <span class="idle-glyph"><Icon name="audit" size={18} /></span>
          <p>Open a member's history to see their recorded operator actions.</p>
        </div>
      {/if}
    </aside>
  </div>
</section>

<svelte:window onkeydown={onKey} />

<ConfirmDialog
  open={removeTarget !== null}
  title="Remove staff member"
  body={removeTarget ? `@${removeTarget.login} loses console access immediately. Their audit history is kept.` : undefined}
  confirmLabel="Remove"
  cancelLabel="Cancel"
  danger
  busy={busy}
  onCancel={() => (removeTarget = null)}
  onConfirm={() => removeForm?.requestSubmit()}
/>
<form method="POST" action="?/remove" use:enhance={removeSubmit} bind:this={removeForm} hidden>
  <input type="hidden" name="user_id" value={removeTarget?.id ?? ''} />
  <input type="hidden" name="target_role" value={removeTarget?.role ?? ''} />
</form>

<style>
  .roster-count { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); }

  .add-card { margin-bottom: 16px; }
  .row-skeletons { display: flex; flex-direction: column; gap: 8px; padding: 12px; }
  .add-form { display: grid; grid-template-columns: repeat(4, 1fr) auto; gap: 12px; align-items: end; }
  .add-form label {
    display: flex; flex-direction: column; gap: 6px;
    font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted);
  }
  .add-actions { display: flex; gap: 8px; }
  @media (max-width: 900px) {
    .add-form { grid-template-columns: 1fr 1fr; }
    .add-actions { grid-column: 1 / -1; }
  }

  .text-input {
    min-width: 0; padding: 8px 11px;
    font-family: var(--bb-font-mono); font-size: 12.5px;
    border: 1px solid var(--rule); border-radius: 8px;
    background: var(--bb-bg-1, #16130f); color: var(--bb-white);
  }
  .text-input:focus { outline: none; border-color: var(--bb-border-strong); }

  .deck { display: grid; grid-template-columns: minmax(0, 1fr); gap: 16px; align-items: start; }
  @media (min-width: 1080px) {
    .deck { grid-template-columns: minmax(0, 1fr) 320px; }
  }

  .list { list-style: none; margin: 0; padding: 0; }
  .staff-row {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto auto;
    align-items: center;
    gap: 14px;
    padding: 13px 14px;
    border-bottom: 1px solid var(--rule);
  }
  .staff-row:last-child { border-bottom: none; }

  .avatar {
    width: 38px; height: 38px; border-radius: 50%; flex: none;
    display: inline-flex; align-items: center; justify-content: center;
    font-family: var(--bb-font-display); font-weight: 800; font-size: 15px;
    color: var(--bb-tan-light);
    background: rgba(201, 168, 124, 0.1); border: 1px solid rgba(201, 168, 124, 0.3);
  }
  .who { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .login {
    font-family: var(--bb-font-body); font-weight: 600; font-size: 13.5px; color: var(--bb-white);
    display: inline-flex; align-items: center; gap: 8px;
  }
  .you {
    font-family: var(--bb-font-mono); font-size: 9.5px; letter-spacing: 0.1em; text-transform: uppercase;
    color: var(--bb-green-glow); background: rgba(82, 183, 136, 0.1);
    border: 1px solid rgba(82, 183, 136, 0.3); border-radius: var(--bb-radius-pill); padding: 1px 7px;
  }
  .sub {
    font-family: var(--bb-font-mono); font-size: 10.5px; color: var(--bb-muted);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }

  .role-pill, .role-select {
    font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.06em; text-transform: uppercase;
    padding: 5px 12px; border-radius: var(--bb-radius-pill);
    border: 1px solid var(--glass-border); background: rgba(255, 255, 255, 0.03); color: var(--bb-muted);
  }
  .role-select { cursor: pointer; }
  .role-owner { color: var(--bb-green-glow); border-color: rgba(82, 183, 136, 0.35); background: rgba(82, 183, 136, 0.1); }
  .role-admin { color: var(--bb-tan-light); border-color: rgba(201, 168, 124, 0.32); background: rgba(201, 168, 124, 0.1); }

  .row-actions { display: flex; gap: 4px; }
  .mini-act {
    width: 28px; height: 28px; border-radius: 7px;
    display: inline-flex; align-items: center; justify-content: center;
    background: none; border: 1px solid transparent; color: var(--bb-muted); cursor: pointer;
  }
  .mini-act :global(svg) { stroke: currentColor; fill: none; stroke-width: 1.7; }
  .mini-act:hover { color: var(--bb-white); background: rgba(255, 255, 255, 0.05); }
  .mini-act.danger:hover { color: #cf8a78; background: rgba(176, 90, 70, 0.1); }

  .inspector {
    position: sticky; top: 62px;
    border: 1px solid var(--rule); border-top-color: var(--rule-strong); border-radius: 8px;
    background: linear-gradient(180deg, rgba(240, 236, 228, 0.03), rgba(240, 236, 228, 0.012));
    display: flex; flex-direction: column;
    max-height: calc(100vh - 62px - 108px);
  }
  .inspector-head {
    display: flex; align-items: center; justify-content: space-between; gap: 10px;
    padding: 12px 16px; border-bottom: 1px solid var(--rule);
  }
  .inspector-tag {
    font-family: var(--bb-font-display); font-weight: 700; font-size: 12px;
    color: var(--bb-tan); overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .inspector-idle {
    padding: 34px 20px; text-align: center; color: var(--bb-muted);
    font-family: var(--bb-font-body); font-size: 13px;
    display: flex; flex-direction: column; align-items: center; gap: 12px;
  }
  .idle-glyph {
    display: inline-flex; align-items: center; justify-content: center;
    width: 40px; height: 40px; border: 1px solid var(--rule-tan); border-radius: 8px;
    color: var(--bb-tan-light);
  }
  .inspector-idle p { margin: 0; max-width: 26ch; line-height: 1.5; }

  .hist-note { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); margin: 6px 4px; }
  .hist-note.err { color: #cf8a78; }
  .hist-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; }
  .hist-row {
    display: flex; align-items: flex-start; gap: 10px;
    padding: 10px 4px; border-bottom: 1px solid var(--rule);
  }
  .hist-row:last-child { border-bottom: none; }
  .hdot { width: 7px; height: 7px; border-radius: 50%; background: var(--bb-green-glow); margin-top: 5px; flex: none; }
  .hdot.err { background: #cf8a78; }
  .hist-body { display: flex; flex-direction: column; gap: 2px; min-width: 0; flex: 1; }
  .hact { font-family: var(--bb-font-mono); font-size: 11.5px; color: var(--bb-white); word-break: break-word; }
  .hdetail { font-family: var(--bb-font-mono); font-size: 10.5px; color: var(--bb-muted); word-break: break-word; }
  .hdetail.err { color: #cf8a78; }
  .hwhen { font-family: var(--bb-font-mono); font-size: 10px; color: var(--bb-muted); white-space: nowrap; }

  .inspector-backdrop { display: none; }
  @media (max-width: 1079px) {
    .inspector { display: none; }
    .inspector.open {
      display: flex;
      position: fixed;
      left: 0; right: 0; bottom: 0; top: auto;
      z-index: 220; max-height: 88vh;
      border-radius: 8px 8px 0 0;
      background: var(--bb-bg-1, #111);
    }
    .inspector-backdrop.open {
      display: block; position: fixed; inset: 0; z-index: 219;
      background: rgba(0, 0, 0, 0.55);
    }
    .staff-row { grid-template-columns: auto minmax(0, 1fr) auto; }
    .role-pill, .role-select { display: none; }
  }
</style>
