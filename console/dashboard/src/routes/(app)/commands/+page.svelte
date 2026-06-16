<script lang="ts">
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, Badge, PERMS, PERM_LABELS } from '@bagel/shared';
  import type { Perm, CommandView } from '@bagel/shared';
  let { data } = $props();

  // Local source of truth, seeded from the SSR load. Each action result is
  // reconciled row-by-row into this list (see applyResult) rather than wholesale
  // replacing it with the server's snapshot. Concurrent submits (e.g. toggling
  // two rows fast) then only touch their own row, so a slower reply built from a
  // pre-flush DB snapshot can no longer clobber another in-flight change.
  // svelte-ignore state_referenced_locally
  let items = $state<CommandView[]>(data.commands ?? []);

  // Resync when a fresh SSR load delivers a different list (full reload), but
  // not on optimistic edits — those mutate items without touching data.commands.
  // svelte-ignore state_referenced_locally
  let seed = data.commands;
  $effect(() => {
    if (data.commands !== seed) {
      seed = data.commands;
      items = data.commands ?? [];
    }
  });

  // Reconcile one action result into the local list. Uses only the affected
  // command (looked up by name) plus the original name on rename, never the
  // whole returned list, so it is order-independent across concurrent results.
  type ActionResult = {
    ok: boolean;
    action?: 'created' | 'updated' | 'deleted';
    name?: string;
    original?: string;
    silent?: boolean;
    error?: string;
    commands?: CommandView[];
  };

  function applyResult(d: ActionResult) {
    if (!d.ok) {
      if (d.error) flash('err', d.error);
      return;
    }

    if (d.action === 'deleted') {
      items = items.filter((c) => c.name !== d.name);
    } else {
      const next = d.commands?.find((c) => c.name === d.name);
      if (next) {
        // Drop the pre-rename key, then replace-or-append the affected row.
        const without = items.filter((c) => c.name !== d.name && c.name !== d.original);
        items = [...without, next];
      }
    }

    if (!d.silent) {
      const verb = d.action === 'deleted' ? 'deleted' : (d.action ?? 'updated');
      flash('ok', `Command ${d.name} ${verb}.`);
    }
  }

  // Custom enhance: apply the result locally and skip the default invalidateAll,
  // so navigation does not refetch a write-behind (stale) list mid-edit.
  const reconcile: SubmitFunction = () => async ({ result }) => {
    if (result.type === 'success' && result.data) applyResult(result.data as ActionResult);
    else if (result.type === 'failure' && result.data) applyResult(result.data as ActionResult);
  };

  const filters = ['All', 'Custom', 'Built-in', 'Disabled'] as const;
  let active = $state<(typeof filters)[number]>('All');
  let search = $state('');

  const rows = $derived(
    items
      .filter((c) => (active === 'Disabled' ? !c.is_active : true))
      .filter((c) => c.name.toLowerCase().includes(search.toLowerCase()))
  );

  // --- Editor (create + edit share one modal) -----------------------------
  type Draft = {
    edit: boolean;
    name: string;
    response: string;
    perm: Perm;
    cooldown: number;
    allowed_user_id: string;
    is_active: boolean;
  };

  let editor = $state<Draft | null>(null);

  function openNew() {
    editor = {
      edit: false,
      name: '',
      response: '',
      perm: 'everyone',
      cooldown: 0,
      allowed_user_id: '',
      is_active: true
    };
  }

  function openEdit(c: CommandView) {
    editor = {
      edit: true,
      name: c.name,
      response: c.response,
      perm: (c.perm ?? 'everyone') as Perm,
      cooldown: c.cooldown ?? 0,
      allowed_user_id: c.allowed_user_id ?? '',
      is_active: c.is_active
    };
  }

  function closeEditor() {
    editor = null;
  }

  // --- Delete confirm modal -----------------------------------------------
  let deleteTarget = $state<string | null>(null);
  const requestDelete = (name: string) => (deleteTarget = name);
  const cancelDelete = () => (deleteTarget = null);

  // --- Toast: surfaces created / updated / deleted confirmations ----------
  let toast = $state<{ kind: 'ok' | 'err'; text: string } | null>(null);
  let toastTimer: ReturnType<typeof setTimeout> | undefined;

  function flash(kind: 'ok' | 'err', text: string) {
    toast = { kind, text };
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => (toast = null), 3200);
  }

  const cd = (n?: number) => (n && n > 0 ? `${n}s` : '0s');
</script>

<section class="screen active">
  <div class="page-head">
    <span class="eyebrow">Manage</span>
    <h1>Chat <em>commands</em></h1>
    <p>Custom responses your viewers can trigger in chat. {items.filter((c) => c.is_active).length} active, {items.filter((c) => !c.is_active).length} disabled.</p>
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
      <input type="text" placeholder="Filter commands…" bind:value={search} />
    </label>
    <button class="btn primary" onclick={openNew}>
      <Icon name="plus" size={14} /> New command
    </button>
  </div>

  <div class="card" style="padding:18px 6px">
    <div class="table">
      <div class="thead">
        <span>Command</span><span>Response</span><span class="perm-cell">Access</span><span>Cooldown</span><span>Uses</span><span></span>
      </div>
      <div class="trows">
        {#each rows as c (c.name)}
          <div class="trow {c.is_active ? '' : 'off'}" style={c.is_active ? '' : 'opacity:.55'}>
            <span class="cmd">
              {c.name}
              {#if c.allowed_user_id}
                <span class="lock" title="Locked to user {c.allowed_user_id}"><Icon name="lock" size={11} /></span>
              {/if}
            </span>
            <span class="resp">{c.response}</span>
            <span class="perm-cell"><Badge perm={(c.perm ?? 'everyone') as Perm} /></span>
            <span class="cd">{cd(c.cooldown)}</span>
            <span class="uses">{c.uses ?? '0'}</span>
            <span class="row-act">
              <!-- Toggle: silent upsert that flips is_active, preserving config -->
              <form method="POST" action="?/toggle" use:enhance={reconcile}>
                <input type="hidden" name="name" value={c.name} />
                <input type="hidden" name="response" value={c.response} />
                <input type="hidden" name="perm" value={c.perm ?? 'everyone'} />
                <input type="hidden" name="cooldown" value={c.cooldown ?? 0} />
                <input type="hidden" name="allowed_user_id" value={c.allowed_user_id ?? ''} />
                <input type="hidden" name="is_active" value={c.is_active ? '' : 'on'} />
                <button class="toggle {c.is_active ? 'on' : ''}" type="submit" aria-label="Toggle"></button>
              </form>
              <button class="mini" type="button" aria-label="Edit {c.name}" onclick={() => openEdit(c)}>
                <Icon name="edit" size={15} />
              </button>
              <button class="mini" type="button" aria-label="Delete {c.name}" onclick={() => requestDelete(c.name)}>
                <Icon name="trash" size={15} />
              </button>
            </span>
          </div>
        {/each}
        {#if rows.length === 0}
          <div class="empty">No commands match.</div>
        {/if}
      </div>
    </div>
  </div>
</section>

<!-- Create / edit editor -->
{#if editor}
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div class="modal-backdrop" role="button" tabindex="-1" aria-label="Close editor" onclick={closeEditor}></div>
  <dialog class="editor-dialog" open aria-modal="true" aria-labelledby="editor-title">
    <h3 id="editor-title">{editor.edit ? 'Edit' : 'New'} command</h3>
    <form
      method="POST"
      action="?/save"
      use:enhance={() => async ({ result }) => {
        if (result.type === 'success' && result.data) applyResult(result.data as ActionResult);
        else if (result.type === 'failure' && result.data) applyResult(result.data as ActionResult);
        if (result.type === 'success' && result.data?.ok) closeEditor();
      }}
    >
      {#if editor.edit}
        <input type="hidden" name="edit" value="1" />
        <input type="hidden" name="original_name" value={editor.name} />
      {/if}

      <label class="field">
        <span>Name</span>
        <input
          class="search"
          name="name"
          placeholder="!command"
          bind:value={editor.name}
          required
        />
        {#if editor.edit}<small>The trigger viewers type in chat. Renaming keeps the response and settings.</small>{/if}
      </label>

      <label class="field">
        <span>Response</span>
        <div class="resp-wrap">
          <textarea
            class="resp-area"
            name="response"
            rows="4"
            maxlength="500"
            placeholder="What the bot replies… use {'{user}'}, {'{target}'}, {'{uptime}'} and more."
            bind:value={editor.response}
            required
          ></textarea>
          <span class="resp-count">{(editor.response ?? '').length}/500</span>
        </div>
      </label>

      <div class="field-row">
        <label class="field">
          <span>Access</span>
          <select class="search" name="perm" bind:value={editor.perm}>
            {#each PERMS as p}
              <option value={p}>{PERM_LABELS[p]}</option>
            {/each}
          </select>
        </label>

        <label class="field">
          <span>Cooldown (s)</span>
          <input class="search" type="number" name="cooldown" min="0" max="86400" bind:value={editor.cooldown} />
        </label>
      </div>

      <label class="field">
        <span>Restrict to user ID <small>(optional)</small></span>
        <input
          class="search"
          name="allowed_user_id"
          inputmode="numeric"
          placeholder="Twitch user ID — only they can run it"
          bind:value={editor.allowed_user_id}
        />
      </label>

      <label class="check">
        <input type="checkbox" name="is_active" checked={editor.is_active} />
        <span>Active</span>
      </label>

      <div class="modal-actions">
        <button type="button" class="btn ghost" onclick={closeEditor}>Cancel</button>
        <button type="submit" class="btn primary"><Icon name="check" size={14} /> {editor.edit ? 'Save changes' : 'Create'}</button>
      </div>
    </form>
  </dialog>
{/if}

<!-- Delete confirm modal -->
{#if deleteTarget !== null}
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div class="modal-backdrop" role="button" tabindex="-1" aria-label="Close dialog" onclick={cancelDelete}></div>
  <dialog class="confirm-dialog" open aria-modal="true" aria-labelledby="del-modal-title">
    <h3 id="del-modal-title">Delete <code>{deleteTarget}</code>?</h3>
    <p>This command will be permanently removed and cannot be recovered.</p>
    <form
      method="POST"
      action="?/delete"
      use:enhance={() => async ({ result }) => {
        deleteTarget = null;
        if (result.type === 'success' && result.data) applyResult(result.data as ActionResult);
        else if (result.type === 'failure' && result.data) applyResult(result.data as ActionResult);
      }}
      class="modal-actions"
    >
      <input type="hidden" name="name" value={deleteTarget} />
      <button type="button" class="btn ghost" onclick={cancelDelete}>Cancel</button>
      <button type="submit" class="btn delete-btn">Delete</button>
    </form>
  </dialog>
{/if}

<!-- Confirmation toast -->
{#if toast}
  <div class="toast {toast.kind}" role="status">
    <Icon name={toast.kind === 'ok' ? 'check' : 'ban'} size={15} />
    <span>{toast.text}</span>
  </div>
{/if}

<svelte:window onkeydown={(e) => { if (e.key === 'Escape') { cancelDelete(); closeEditor(); } }} />

<style>
  .empty {
    padding: 28px 18px;
    text-align: center;
    color: var(--bb-muted);
    font-family: var(--bb-font-body);
    font-size: 13px;
  }

  .lock {
    display: inline-flex;
    color: var(--bb-tan-light);
    margin-left: 6px;
    vertical-align: middle;
  }

  @media (max-width: 760px) {
    .toolbar { gap: 8px; }
    .chip-row {
      overflow-x: auto;
      -webkit-overflow-scrolling: touch;
      flex-wrap: nowrap;
      scrollbar-width: none;
    }
    .chip-row::-webkit-scrollbar { display: none; }
    :global(.trow .toggle) { min-width: 38px; min-height: 44px; display: flex; align-items: center; }
    :global(.mini) { min-width: 44px; min-height: 44px; }
  }

  /* Shared modal chrome */
  .modal-backdrop {
    position: fixed;
    inset: 0;
    z-index: 200;
    background: rgba(0, 0, 0, 0.55);
    backdrop-filter: blur(4px);
    -webkit-backdrop-filter: blur(4px);
  }

  .editor-dialog,
  .confirm-dialog {
    position: fixed;
    inset: 0;
    margin: auto;
    z-index: 201;
    height: fit-content;
    background: var(--bb-card-bg);
    border: 1px solid var(--bb-border-strong);
    border-radius: var(--bb-radius-lg);
    box-shadow: 0 24px 64px rgba(0, 0, 0, 0.6);
    display: block;
    color: var(--bb-white);
  }

  .editor-dialog {
    width: min(520px, calc(100vw - 32px));
    padding: 26px 24px 20px;
  }

  .confirm-dialog {
    width: min(400px, calc(100vw - 32px));
    padding: 28px 24px 20px;
  }

  .editor-dialog h3,
  .confirm-dialog h3 {
    font-family: var(--bb-font-display);
    font-weight: 600;
    font-size: 18px;
    letter-spacing: -0.01em;
    color: var(--bb-white);
    margin: 0 0 18px;
  }

  .confirm-dialog h3 { margin-bottom: 10px; }
  .confirm-dialog h3 code { font-family: var(--bb-font-mono); color: var(--bb-tan-light); font-size: 16px; }

  .confirm-dialog p {
    font-family: var(--bb-font-body);
    font-size: 14px;
    line-height: 1.55;
    color: var(--bb-muted);
    margin: 0 0 22px;
  }

  .field { display: flex; flex-direction: column; gap: 6px; margin-bottom: 14px; }
  .field > span {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
    letter-spacing: 0.01em;
  }
  .field small { color: var(--bb-muted); opacity: 0.7; font-size: 11px; }

  .resp-wrap { position: relative; }

  .resp-area {
    width: 100%;
    box-sizing: border-box;
    resize: vertical;
    min-height: 96px;
    padding: 12px 14px 26px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-md, 10px);
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    line-height: 1.6;
    transition: border-color var(--bb-dur-base, 160ms) ease, box-shadow var(--bb-dur-base, 160ms) ease;
  }
  .resp-area::placeholder { color: var(--bb-muted); opacity: 0.7; }
  .resp-area:focus {
    outline: none;
    border-color: rgba(82, 183, 136, 0.5);
    box-shadow: 0 0 0 3px rgba(82, 183, 136, 0.12);
    background: rgba(255, 255, 255, 0.04);
  }

  .resp-count {
    position: absolute;
    right: 10px;
    bottom: 8px;
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    color: var(--bb-muted);
    pointer-events: none;
    opacity: 0.7;
  }

  .field-row { display: flex; gap: 12px; }
  .field-row .field { flex: 1; }

  .check {
    display: flex;
    align-items: center;
    gap: 8px;
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-white);
    margin: 4px 0 18px;
    cursor: pointer;
  }

  .modal-actions {
    display: flex;
    gap: 10px;
    justify-content: flex-end;
    padding: 0;
    margin: 0;
    background: none;
    border: none;
  }

  .delete-btn {
    background: rgba(176, 90, 70, 0.15);
    border-color: rgba(176, 90, 70, 0.4);
    color: #cf8a78;
  }
  .delete-btn:hover {
    background: rgba(176, 90, 70, 0.28);
    box-shadow: 0 0 18px rgba(176, 90, 70, 0.25);
  }

  /* Toast */
  .toast {
    position: fixed;
    right: 20px;
    bottom: 20px;
    z-index: 300;
    display: flex;
    align-items: center;
    gap: 9px;
    padding: 12px 16px;
    border-radius: var(--bb-radius-md, 10px);
    background: var(--bb-card-bg);
    border: 1px solid var(--bb-border-strong);
    box-shadow: 0 14px 40px rgba(0, 0, 0, 0.5);
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    color: var(--bb-white);
    animation: toast-in 220ms var(--bb-ease-out-back, ease-out);
  }
  .toast.ok { border-color: rgba(82, 183, 136, 0.45); color: var(--bb-green-glow); }
  .toast.err { border-color: rgba(176, 90, 70, 0.45); color: #cf8a78; }

  @keyframes toast-in {
    from { opacity: 0; transform: translateY(8px); }
    to { opacity: 1; transform: translateY(0); }
  }

  @media (max-width: 480px) {
    .field-row { flex-direction: column; gap: 0; }
    .modal-actions { flex-direction: column-reverse; }
    .modal-actions .btn, .modal-actions button {
      width: 100%;
      justify-content: center;
      min-height: 44px;
    }
    .toast { left: 16px; right: 16px; bottom: 16px; }
  }
</style>
