<script lang="ts">
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, Badge, PERMS, PERM_LABELS, Modal, Drawer, Card, PageHead } from '@bagel/shared';
  import type { Perm, CommandView } from '@bagel/shared';
  import CheckButton from '$lib/components/CheckButton.svelte';
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
    aliases: string[];
    response: string;
    perm: Perm;
    cooldown: number;
    allowed_user_id: string;
    stream_online_only: boolean;
    is_active: boolean;
  };

  let editor = $state<Draft | null>(null);
  // Draft of the alias being typed in the editor's alias input.
  let aliasDraft = $state('');

  function openNew() {
    aliasDraft = '';
    editor = {
      edit: false,
      name: '',
      aliases: [],
      response: '',
      perm: 'everyone',
      cooldown: 0,
      allowed_user_id: '',
      stream_online_only: false,
      is_active: true
    };
  }

  function openEdit(c: CommandView) {
    aliasDraft = '';
    editor = {
      edit: true,
      name: c.name,
      aliases: [...(c.aliases ?? [])],
      response: c.response,
      perm: (c.perm ?? 'everyone') as Perm,
      cooldown: c.cooldown ?? 0,
      allowed_user_id: c.allowed_user_id ?? '',
      stream_online_only: c.stream_online_only === true,
      is_active: c.is_active
    };
  }

  function handleRowKey(e: KeyboardEvent, c: CommandView) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      openEdit(c);
    }
  }

  function closeEditor() {
    editor = null;
  }

  // Commit the typed alias as a pill. De-duplicates case-insensitively against
  // both existing aliases and the command's own name; blanks are ignored.
  function addAlias() {
    if (!editor) return;
    const a = aliasDraft.trim();
    aliasDraft = '';
    if (!a) return;
    const key = a.toLowerCase();
    if (key === editor.name.trim().toLowerCase()) return;
    if (editor.aliases.some((x) => x.toLowerCase() === key)) return;
    editor.aliases = [...editor.aliases, a];
  }

  function removeAlias(alias: string) {
    if (!editor) return;
    editor.aliases = editor.aliases.filter((a) => a !== alias);
  }

  // Enter or comma commits the alias; Backspace on an empty input pops the last.
  function aliasKey(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      addAlias();
    } else if (e.key === 'Backspace' && aliasDraft === '' && editor && editor.aliases.length) {
      editor.aliases = editor.aliases.slice(0, -1);
    }
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
  <PageHead eyebrow="Manage" description="Custom responses your viewers can trigger in chat. {items.filter((c) => c.is_active).length} active, {items.filter((c) => !c.is_active).length} disabled.">Chat <em>commands</em></PageHead>

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

  <Card style="padding:18px 6px">
    <div class="table">
      <div class="thead">
        <span>Command</span><span>Response</span><span class="perm-cell">Access</span><span>Cooldown</span><span>Uses</span><span></span>
      </div>
      <div class="trows">
        {#each rows as c (c.name)}
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div
            class="trow trow-clickable {c.is_active ? '' : 'off'}"
            style={c.is_active ? '' : 'opacity:.55'}
            role="button"
            tabindex="0"
            onclick={() => openEdit(c)}
            onkeydown={(e) => handleRowKey(e, c)}
          >
            <span class="cmd">
              <span class="cmd-name">
                {c.name}
                {#if c.allowed_user_id}
                  <span class="lock" title="Locked to user {c.allowed_user_id}"><Icon name="lock" size={11} /></span>
                {/if}
                {#if c.stream_online_only}
                  <span class="lock" title="Only runs while live"><Icon name="pulse" size={11} /></span>
                {/if}
              </span>
              {#if c.aliases?.length}
                <span class="aliases" title="Also: {c.aliases.join(', ')}">
                  {#each c.aliases as a}<span class="alias-tag">{a}</span>{/each}
                </span>
              {/if}
            </span>
            <span class="resp">{c.response}</span>
            <span class="perm-cell"><Badge perm={(c.perm ?? 'everyone') as Perm} /></span>
            <span class="cd">{cd(c.cooldown)}</span>
            <span class="uses">{c.uses ?? '0'}</span>
            <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
            <span class="row-act" onclick={(e) => e.stopPropagation()}>
              <!-- Toggle: silent upsert that flips is_active, preserving config -->
              <form method="POST" action="?/toggle" use:enhance={reconcile}>
                <input type="hidden" name="name" value={c.name} />
                {#each c.aliases ?? [] as a}<input type="hidden" name="aliases" value={a} />{/each}
                <input type="hidden" name="response" value={c.response} />
                <input type="hidden" name="perm" value={c.perm ?? 'everyone'} />
                <input type="hidden" name="cooldown" value={c.cooldown ?? 0} />
                <input type="hidden" name="allowed_user_id" value={c.allowed_user_id ?? ''} />
                <input type="hidden" name="stream_online_only" value={c.stream_online_only ? 'on' : ''} />
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
  </Card>
</section>

<!-- Create / edit editor (drawer) -->
{#if editor}
  <div
    class="drawer-backdrop"
    role="button"
    tabindex="-1"
    aria-label="Close editor"
    onclick={closeEditor}
    onkeydown={(e) => { if (e.key === 'Enter') closeEditor(); }}
  ></div>
  <div class="drawer open" role="dialog" aria-modal="true" aria-labelledby="editor-title">
    <header class="drawer-head">
      <div class="drawer-id">
        <h2 id="editor-title">{editor.edit ? 'Edit' : 'New'} command</h2>
        <span class="drawer-sub">{editor.edit ? editor.name : 'create a custom response'}</span>
      </div>
      <button class="drawer-close" type="button" onclick={closeEditor} aria-label="Close">
        <Icon name="x" size={16} />
      </button>
    </header>

    <div class="drawer-body">
    <form
      method="POST"
      action="?/save"
      use:enhance={({ formData }) => {
        // Fold a typed-but-uncommitted alias into the submission.
        const pending = aliasDraft.trim();
        if (pending) formData.append('aliases', pending);
        return async ({ result }) => {
          if (result.type === 'success' && result.data) applyResult(result.data as ActionResult);
          else if (result.type === 'failure' && result.data) applyResult(result.data as ActionResult);
          if (result.type === 'success' && result.data?.ok) closeEditor();
        };
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

      <div class="field">
        <span>Alternate names <small>(optional)</small></span>
        <input
          class="search"
          placeholder="Type a name, press Enter"
          bind:value={aliasDraft}
          onkeydown={aliasKey}
          onblur={addAlias}
        />
        <!-- Committed aliases submit as repeated `aliases` fields. -->
        {#each editor.aliases as a}
          <input type="hidden" name="aliases" value={a} />
        {/each}
        {#if editor.aliases.length}
          <div class="pills">
            {#each editor.aliases as a (a)}
              <button type="button" class="pill" onclick={() => removeAlias(a)} aria-label="Remove {a}">
                <span>{a}</span>
                <Icon name="x" size={11} />
              </button>
            {/each}
          </div>
        {/if}
      </div>

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

      <div class="check">
        <CheckButton name="is_active" bind:checked={editor.is_active} label="Active" />
      </div>

      <div class="check">
        <CheckButton name="stream_online_only" bind:checked={editor.stream_online_only} label="Only while live" />
      </div>

      <div class="modal-actions">
        <button type="button" class="btn ghost" onclick={closeEditor}>Cancel</button>
        <button type="submit" class="btn primary"><Icon name="check" size={14} /> {editor.edit ? 'Save changes' : 'Create'}</button>
      </div>
    </form>
    </div>
  </div>
{/if}

<!-- Delete confirm modal -->
<Modal open={deleteTarget !== null} closeModal={cancelDelete}>
  {#if deleteTarget !== null}
    <h3 id="del-modal-title">Delete <code>{deleteTarget}</code>?</h3>
    <p class="modal-body">This command will be permanently removed and cannot be recovered.</p>
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
  {/if}
</Modal>

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

  /* Row: command name stacks above its alternate-name tags. */
  .cmd { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
  .cmd-name { display: inline-flex; align-items: center; }
  .aliases { display: flex; flex-wrap: wrap; gap: 4px; }
  .alias-tag {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    color: var(--bb-muted);
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid var(--glass-border);
    border-radius: 999px;
    padding: 1px 7px;
    white-space: nowrap;
  }

  /* Editor: alias pills sit right under the alternate-names input. */
  .pills { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
  .pill {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-family: var(--bb-font-mono);
    font-size: 12px;
    color: var(--bb-tan-light);
    background: rgba(201, 168, 124, 0.1);
    border: 1px solid rgba(201, 168, 124, 0.28);
    border-radius: 999px;
    padding: 3px 10px;
    cursor: pointer;
    transition: all var(--bb-dur-fast, 140ms) var(--bb-ease-out-expo, ease);
  }
  /* The X stays hidden until the pill is hovered or focused. */
  .pill :global(svg) {
    width: 0;
    opacity: 0;
    transition: width var(--bb-dur-fast, 140ms) ease, opacity var(--bb-dur-fast, 140ms) ease;
  }
  .pill:hover, .pill:focus-visible {
    color: #cf8a78;
    background: rgba(176, 90, 70, 0.16);
    border-color: rgba(176, 90, 70, 0.45);
    outline: none;
  }
  .pill:hover :global(svg), .pill:focus-visible :global(svg) {
    width: 11px;
    opacity: 1;
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
    .drawer {
      width: 100vw; height: 92vh; top: auto; bottom: 0; right: 0;
      border-left: none; border-top: 1px solid var(--glass-border);
      border-radius: var(--bb-radius-lg, 16px) var(--bb-radius-lg, 16px) 0 0;
      transform: translateY(100%);
      animation: sheet-in var(--bb-dur-med, 320ms) var(--bb-ease-out-expo, cubic-bezier(.16,1,.3,1)) forwards;
    }
    @keyframes sheet-in { to { transform: translateY(0); } }
  }



  /* clickable command row */
  .trow-clickable { cursor: pointer; user-select: none; transition: background var(--bb-dur-fast, 140ms) var(--bb-ease-out-expo, ease); }
  .trow-clickable:hover { background: rgba(201, 168, 124, 0.06); }
  .trow-clickable:focus-visible { outline: 2px solid var(--bb-tan, #c9a87c); outline-offset: -2px; }

  /* ---- Detail drawer (matches admin user drawer) ---- */
  .drawer-backdrop {
    position: fixed; inset: 0; z-index: 200;
    background: rgba(0, 0, 0, 0.5);
    backdrop-filter: blur(2px); -webkit-backdrop-filter: blur(2px);
    animation: fade var(--bb-dur-fast, 160ms) var(--bb-ease-out-expo, ease) both;
  }
  @keyframes fade { from { opacity: 0; } to { opacity: 1; } }

  .drawer {
    position: fixed; top: 0; right: 0; z-index: 201;
    height: 100vh; width: min(460px, 92vw);
    display: flex; flex-direction: column;
    background:
      linear-gradient(var(--glass-fill), var(--glass-fill)),
      var(--bb-bg-1, #111);
    border-left: 1px solid var(--glass-border);
    backdrop-filter: blur(var(--glass-blur)); -webkit-backdrop-filter: blur(var(--glass-blur));
    box-shadow: -16px 0 48px rgba(0, 0, 0, 0.45);
    transform: translateX(100%);
    animation: slide-in var(--bb-dur-med, 320ms) var(--bb-ease-out-expo, cubic-bezier(.16,1,.3,1)) forwards;
  }
  @keyframes slide-in { to { transform: translateX(0); } }

  .drawer-head {
    display: flex; align-items: flex-start; justify-content: space-between;
    gap: 1rem; padding: 22px 22px 16px;
    border-bottom: 1px solid var(--glass-border);
  }
  .drawer-id h2 {
    font-family: var(--bb-font-display); font-weight: 700; font-size: 20px;
    color: var(--bb-white); margin: 0 0 4px; letter-spacing: -0.01em;
  }
  .drawer-sub { font-family: var(--bb-font-mono); font-size: 12px; color: var(--bb-muted); }
  .drawer-close {
    display: inline-flex; align-items: center; justify-content: center;
    width: 32px; height: 32px; flex: none;
    border: 1px solid var(--glass-border); border-radius: var(--bb-radius-sm, 8px);
    background: transparent; color: var(--bb-muted); cursor: pointer;
    transition: all var(--bb-dur-fast, 140ms) var(--bb-ease-out-expo, ease);
  }
  .drawer-close:hover { color: var(--bb-white); border-color: var(--bb-border-strong); background: rgba(255,255,255,0.04); }

  .drawer-body { flex: 1; overflow-y: auto; padding: 20px 22px 32px; }



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
  .field-row .field { flex: 1; min-width: 0; }

  /* The global .search sets a fixed 240px width; in the modal's flex columns
     that overflows (e.g. Cooldown spilling past the drawer). Let each control
     fill its field instead. */
  .field .search { width: 100%; box-sizing: border-box; }

  /* Center the box against its single-line label ("Active"). */
  .check { margin: 4px 0 18px; }
  .check :global(.cb) { align-items: center; }

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
