<script lang="ts">
  // Inline command editor (create + edit share it). Renders the ?/save form,
  // runs the shared validator client-side before submitting (instant field
  // errors, no round trip on invalid input), and mirrors unsaved drafts to
  // sessionStorage so navigation/refresh can't eat work in progress.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    FieldError,
    Scroller,
    EditorFooter,
    PERMS,
    PERM_LABELS,
    validateCommand,
    normName,
    COOLDOWN_MAX,
    RESPONSE_MAX_LINES,
    getI18n,
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
    status = 'idle' as 'idle' | 'saving' | 'saved' | 'error' | 'conflict',
    dirty = false,
    canSave = true,
    onCancel,
    onSubmit
  }: {
    draft: CommandDraft;
    serverErrors?: CommandErrors | null;
    status?: 'idle' | 'saving' | 'saved' | 'error' | 'conflict';
    dirty?: boolean;
    canSave?: boolean;
    onCancel: () => void;
    onSubmit: SubmitFunction;
  } = $props();

  const busy = $derived(status === 'saving');

  const { t } = getI18n();

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
     not the browser's native tooltips — enhance must always run. The fields
     scroll; the EditorFooter stays pinned so Save/Cancel never fall below the
     fold. -->
<form method="POST" action="?/save" class="editor-form" novalidate use:enhance={submit}>
  <Scroller fill padding="16px" data-lenis-prevent>
   <div class="editor">
  {#if draft.edit}
    <input type="hidden" name="edit" value="1" />
    <input type="hidden" name="original_name" value={draft.originalName} />
  {/if}

  <label class="field">
    <span>{t('commandEditor.name')}</span>
    <input class="search" name="name" placeholder={t('commandEditor.namePlaceholder')} bind:value={draft.name} required />
    <FieldError message={errors.name} />
    {#if draft.edit}<small>{t('commandEditor.renameHint')}</small>{/if}
  </label>

  <div class="field">
    <span>{t('commandEditor.altNames')} <small>{t('common.optional')}</small></span>
    <AliasChips bind:this={chips} bind:aliases={draft.aliases} bind:draft={aliasDraft} commandName={draft.name} />
    {#each draft.aliases as a}
      <input type="hidden" name="aliases" value={a} />
    {/each}
    <FieldError message={errors.aliases} />
  </div>

  <label class="field">
    <span>{t('commandEditor.response')}</span>
    <ResponseEditor bind:value={draft.response} maxLines={RESPONSE_MAX_LINES} />
    <FieldError message={errors.response} />
  </label>

  <ChatPreview name={draft.name} response={draft.response} />

  <div class="field-row">
    <label class="field">
      <span>{t('commandEditor.access')}</span>
      <select class="search" name="perm" bind:value={draft.perm}>
        {#each PERMS as p}
          <option value={p}>{PERM_LABELS[p]}</option>
        {/each}
      </select>
    </label>

    <label class="field">
      <span>{t('commandEditor.cooldownS')}</span>
      <input class="search" type="number" name="cooldown" min="0" max={COOLDOWN_MAX} bind:value={draft.cooldown} />
      <FieldError message={errors.cooldown} />
    </label>
  </div>

  <label class="field">
    <span>{t('commandEditor.restrictUser')} <small>{t('common.optional')}</small></span>
    <input
      class="search"
      name="allowed_user_id"
      inputmode="numeric"
      placeholder={t('commandEditor.restrictPlaceholder')}
      bind:value={draft.allowed_user_id}
    />
    <FieldError message={errors.allowed_user_id} />
  </label>

  <div class="check">
    <CheckButton name="is_active" bind:checked={draft.is_active} label={t('commandEditor.active')} />
  </div>

  <div class="check">
    <CheckButton name="stream_online_only" bind:checked={draft.stream_online_only} label={t('commandEditor.onlyWhileLive')} />
  </div>
   </div>
  </Scroller>

  <EditorFooter
    {status}
    {dirty}
    {canSave}
    saveLabel={draft.edit ? t('commandEditor.saveChanges') : t('commandEditor.create')}
    cancelLabel={t('common.cancel')}
    savingLabel={t('commandEditor.saving')}
    savedLabel={t('commands.saved')}
    errorLabel={t('commands.toastSaveFailed')}
    dirtyLabel={t('commands.unsavedChanges')}
    {onCancel}
  />
</form>

<style>
  .editor-form { display: flex; flex-direction: column; min-height: 0; flex: 1; }
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

  @media (max-width: 480px) {
    .field-row { flex-direction: column; gap: 0; }
  }
</style>
