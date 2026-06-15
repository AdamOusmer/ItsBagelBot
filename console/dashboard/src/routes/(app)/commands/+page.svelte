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

  // Delete confirm modal
  let deleteTarget = $state<string | null>(null);

  function requestDelete(name: string) {
    deleteTarget = name;
  }

  function cancelDelete() {
    deleteTarget = null;
  }
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
    <form
      method="POST"
      action="?/save"
      use:enhance={() => async ({ update }) => { await update(); showNew = false; }}
      class="card new-cmd-form"
    >
      <input class="search" name="name" placeholder="!command" required />
      <input class="search resp-input" name="response" placeholder="Response text…" required />
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
              <!-- Delete: open confirm modal instead of submitting directly -->
              <button
                class="mini"
                type="button"
                aria-label="Delete {c.name}"
                onclick={() => requestDelete(c.name)}
              >
                <Icon name="trash" size={15} />
              </button>
            </span>
          </div>
        {/each}
      </div>
    </div>
  </div>
</section>

<!-- Delete confirm modal -->
{#if deleteTarget !== null}
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div
    class="modal-backdrop"
    role="button"
    tabindex="-1"
    aria-label="Close dialog"
    onclick={cancelDelete}
  ></div>
  <dialog class="confirm-dialog" open aria-modal="true" aria-labelledby="del-modal-title">
    <h3 id="del-modal-title">Delete <code>{deleteTarget}</code>?</h3>
    <p>This command will be permanently removed and cannot be recovered.</p>
    <form
      method="POST"
      action="?/delete"
      use:enhance={() => async ({ update }) => { deleteTarget = null; await update(); }}
      class="modal-actions"
    >
      <input type="hidden" name="name" value={deleteTarget} />
      <button type="button" class="btn ghost" onclick={cancelDelete}>Cancel</button>
      <button type="submit" class="btn delete-btn">Delete</button>
    </form>
  </dialog>
{/if}

<svelte:window onkeydown={(e) => { if (e.key === 'Escape') cancelDelete(); }} />

<style>
  /* New command form: stacks nicely on narrow screens */
  .new-cmd-form {
    display: flex;
    gap: 12px;
    align-items: center;
    flex-wrap: wrap;
    padding: 16px;
    margin-bottom: 14px;
  }

  .new-cmd-form .search[name="name"] {
    width: 160px;
  }

  .resp-input {
    flex: 1;
    min-width: 180px;
  }

  @media (max-width: 760px) {
    /* Toolbar: wrap + make New Command full-row */
    .toolbar {
      gap: 8px;
    }

    .chip-row {
      overflow-x: auto;
      -webkit-overflow-scrolling: touch;
      flex-wrap: nowrap;
      scrollbar-width: none;
    }
    .chip-row::-webkit-scrollbar { display: none; }

    /* New command form goes full width, stacked */
    .new-cmd-form {
      flex-direction: column;
      align-items: stretch;
    }

    .new-cmd-form .search[name="name"],
    .resp-input {
      width: 100%;
      min-width: 0;
    }

    .new-cmd-form .btn {
      min-height: 44px;
      justify-content: center;
    }

    /* Table rows on mobile: show command name + actions only (shared CSS hides resp/cd/perm) */
    /* Ensure trow items have comfortable tap targets */
    :global(.trow .toggle) {
      min-width: 38px;
      min-height: 44px;
      display: flex;
      align-items: center;
    }

    :global(.mini) {
      min-width: 44px;
      min-height: 44px;
    }
  }

  /* Confirm modal */
  .modal-backdrop {
    position: fixed;
    inset: 0;
    z-index: 200;
    background: rgba(0, 0, 0, 0.55);
    backdrop-filter: blur(4px);
    -webkit-backdrop-filter: blur(4px);
  }

  .confirm-dialog {
    position: fixed;
    inset: 0;
    margin: auto;
    z-index: 201;
    width: min(400px, calc(100vw - 32px));
    height: fit-content;
    background: var(--bb-card-bg);
    border: 1px solid var(--bb-border-strong);
    border-radius: var(--bb-radius-lg);
    padding: 28px 24px 20px;
    box-shadow: 0 24px 64px rgba(0, 0, 0, 0.6);
    display: block;
    color: var(--bb-white);
  }

  .confirm-dialog h3 {
    font-family: var(--bb-font-display);
    font-weight: 600;
    font-size: 18px;
    letter-spacing: -0.01em;
    color: var(--bb-white);
    margin: 0 0 10px;
  }

  .confirm-dialog h3 code {
    font-family: var(--bb-font-mono);
    color: var(--bb-tan-light);
    font-size: 16px;
  }

  .confirm-dialog p {
    font-family: var(--bb-font-body);
    font-size: 14px;
    line-height: 1.55;
    color: var(--bb-muted);
    margin: 0 0 22px;
  }

  .modal-actions {
    display: flex;
    gap: 10px;
    justify-content: flex-end;
    /* reset form default margin */
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

  @media (max-width: 480px) {
    .modal-actions {
      flex-direction: column-reverse;
    }

    .modal-actions .btn,
    .modal-actions button {
      width: 100%;
      justify-content: center;
      min-height: 44px;
    }
  }
</style>
