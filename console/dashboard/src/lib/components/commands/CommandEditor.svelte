<script lang="ts">
  // Inline command editor (create + edit share it). Renders the ?/save form,
  // runs the shared validator client-side before submitting (instant field
  // errors, no round trip on invalid input), and mirrors unsaved drafts to
  // sessionStorage so navigation/refresh can't eat work in progress.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Icon,
    FieldError,
    PERMS,
    PERM_LABELS,
    validateCommand,
    normName,
    COOLDOWN_MAX,
    type CommandErrors
  } from '@bagel/shared';
  import CheckButton from '$lib/components/CheckButton.svelte';
  import AliasChips from './AliasChips.svelte';
  import ResponseEditor from './ResponseEditor.svelte';
  import ChatPreview from './ChatPreview.svelte';
  import { draftKey, type CommandDraft } from './drafts';

  let {
    draft = $bindable<CommandDraft>(),
    serverErrors = null as CommandErrors | null,
    busy = false,
    onCancel,
    onSubmit
  }: {
    draft: CommandDraft;
    serverErrors?: CommandErrors | null;
    busy?: boolean;
    onCancel: () => void;
    onSubmit: SubmitFunction;
  } = $props();

  let aliasDraft = $state('');
  let chips = $state<ReturnType<typeof AliasChips>>();
  let clientErrors = $state<CommandErrors>({});
  const errors = $derived<CommandErrors>({ ...(serverErrors ?? {}), ...clientErrors });

  // Mirror the working draft to sessionStorage (skip the initial unmodified
  // state so merely opening an editor doesn't flag the row as unsaved).
  const key = draftKey(draft.originalName, draft.edit);
  const initial = JSON.stringify(draft);
  $effect(() => {
    const current = JSON.stringify(draft);
    if (current === initial) return;
    try {
      sessionStorage.setItem(key, current);
    } catch {
      /* storage full/unavailable — drafts are best-effort */
    }
  });

  // Client-side gate in front of the form action: fold the uncommitted alias,
  // validate, and only let the request through when the fields are sound.
  const submit: SubmitFunction = (input) => {
    chips?.commit();
    clientErrors = validateCommand({
      name: normName(draft.name),
      aliases: draft.aliases.map(normName).filter(Boolean),
      response: draft.response,
      cooldown: Math.floor(Number(draft.cooldown) || 0),
      allowedUserId: draft.allowed_user_id.replace(/\D/g, '')
    });
    if (Object.keys(clientErrors).length) {
      input.cancel();
      return;
    }
    return onSubmit(input);
  };
</script>

<!-- novalidate: the shared validator owns validation (inline FieldError copy),
     not the browser's native tooltips — enhance must always run. -->
<form method="POST" action="?/save" class="editor" novalidate use:enhance={submit}>
  {#if draft.edit}
    <input type="hidden" name="edit" value="1" />
    <input type="hidden" name="original_name" value={draft.originalName} />
  {/if}

  <label class="field">
    <span>Name</span>
    <input class="search" name="name" placeholder="!command" bind:value={draft.name} required />
    <FieldError message={errors.name} />
    {#if draft.edit}<small>The trigger viewers type in chat. Renaming keeps the response and settings.</small>{/if}
  </label>

  <div class="field">
    <span>Alternate names <small>(optional)</small></span>
    <AliasChips bind:this={chips} bind:aliases={draft.aliases} bind:draft={aliasDraft} commandName={draft.name} />
    {#each draft.aliases as a}
      <input type="hidden" name="aliases" value={a} />
    {/each}
    <FieldError message={errors.aliases} />
  </div>

  <label class="field">
    <span>Response</span>
    <ResponseEditor bind:value={draft.response} />
    <FieldError message={errors.response} />
  </label>

  <ChatPreview name={draft.name} response={draft.response} />

  <div class="field-row">
    <label class="field">
      <span>Access</span>
      <select class="search" name="perm" bind:value={draft.perm}>
        {#each PERMS as p}
          <option value={p}>{PERM_LABELS[p]}</option>
        {/each}
      </select>
    </label>

    <label class="field">
      <span>Cooldown (s)</span>
      <input class="search" type="number" name="cooldown" min="0" max={COOLDOWN_MAX} bind:value={draft.cooldown} />
      <FieldError message={errors.cooldown} />
    </label>
  </div>

  <label class="field">
    <span>Restrict to user ID <small>(optional)</small></span>
    <input
      class="search"
      name="allowed_user_id"
      inputmode="numeric"
      placeholder="Twitch user ID — only they can run it"
      bind:value={draft.allowed_user_id}
    />
    <FieldError message={errors.allowed_user_id} />
  </label>

  <div class="check">
    <CheckButton name="is_active" bind:checked={draft.is_active} label="Active" />
  </div>

  <div class="check">
    <CheckButton name="stream_online_only" bind:checked={draft.stream_online_only} label="Only while live" />
  </div>

  <div class="actions">
    <button type="button" class="btn ghost" onclick={onCancel} disabled={busy}>Cancel</button>
    <button type="submit" class="btn primary" disabled={busy}>
      <Icon name="check" size={14} />
      {busy ? 'Saving…' : draft.edit ? 'Save changes' : 'Create'}
    </button>
  </div>
</form>

<style>
  .editor { padding: 4px 2px 2px; }

  .field { display: flex; flex-direction: column; gap: 6px; margin-bottom: 14px; }
  .field > span {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
    letter-spacing: 0.01em;
  }
  .field small { color: var(--bb-muted); opacity: 0.7; font-size: 11px; }
  .field :global(.search) { width: 100%; box-sizing: border-box; }

  .field-row { display: flex; gap: 12px; }
  .field-row .field { flex: 1; min-width: 0; }

  .check { margin: 4px 0 14px; }
  .check :global(.cb) { align-items: center; }

  .actions { display: flex; gap: 10px; justify-content: flex-end; margin-top: 6px; }

  @media (max-width: 480px) {
    .field-row { flex-direction: column; gap: 0; }
    .actions { flex-direction: column-reverse; }
    .actions .btn { width: 100%; justify-content: center; min-height: 44px; }
  }
</style>
