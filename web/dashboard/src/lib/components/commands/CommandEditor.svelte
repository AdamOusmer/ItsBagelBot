<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Inline command editor (create + edit share it). Renders the ?/save form,
  // runs the shared validator client-side before submitting (instant field
  // errors, no round trip on invalid input), and mirrors unsaved drafts to
  // sessionStorage so navigation/refresh can't eat work in progress.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Field,
    Grid,
    FieldError,
    Scroller,
    EditorFooter,
    Switch,
    PERMS,
    PERM_LABELS,
    validateCommand,
    commandContentSnapshot,
    normName,
    COOLDOWN_MAX,
    RESPONSE_MAX_LINES,
    getI18n,
    type CommandErrors
  } from '@bagel/kit';
  import CheckButton from '$lib/components/CheckButton.svelte';
  import AliasChips from './AliasChips.svelte';
  import ResponseEditor from './ResponseEditor.svelte';
  import type { SourceDef } from './fetches/FetchSourcePicker.svelte';
  import ChatPreview from './ChatPreview.svelte';
  import { draftKey, type CommandDraft } from './drafts';

  let {
    draft = $bindable<CommandDraft>(),
    serverErrors = null as CommandErrors | null,
    status = 'idle' as 'idle' | 'saving' | 'saved' | 'error' | 'conflict',
    dirty = false,
    canSave = true,
    fetchDefs = [],
    fetchKeys = [],
    onFetchDefsChanged,
    onCancel,
    onSubmit,
    liveActive = false,
    onToggleActive
  }: {
    draft: CommandDraft;
    serverErrors?: CommandErrors | null;
    status?: 'idle' | 'saving' | 'saved' | 'error' | 'conflict';
    dirty?: boolean;
    canSave?: boolean;
    /** Saved data sources, for the {urlfetch:…} palette chip. */
    fetchDefs?: SourceDef[];
    fetchKeys?: { label: string }[];
    onFetchDefsChanged?: (defs: SourceDef[]) => void;
    onCancel: () => void;
    onSubmit: SubmitFunction;
    // Edit: live row enabled state. The inspector Switch posts the same toggle
    // as the row so Active is not a draft field (#221). Create still uses the
    // checkbox below: there is no row yet.
    liveActive?: boolean;
    onToggleActive?: (next: boolean) => void;
  } = $props();

  const busy = $derived(status === 'saving');

  const { t } = getI18n();

  let aliasDraft = $state('');
  let chips = $state<ReturnType<typeof AliasChips>>();
  let clientErrors = $state<CommandErrors>({});
  const errors = $derived<CommandErrors>({ ...(serverErrors ?? {}), ...clientErrors });

  // Mirror the working draft to sessionStorage (skip the initial unmodified
  // state so merely opening an editor doesn't flag the row as unsaved). Active
  // is compared out: a live toggle must not persist a draft that only differs
  // in is_active, or the row would show an unsaved chip after a pause/resume.
  const key = draftKey(draft.originalName, draft.edit);
  const initial = commandContentSnapshot(draft);
  $effect(() => {
    const current = commandContentSnapshot(draft);
    if (current === initial) return;
    try {
      sessionStorage.setItem(key, current);
    } catch {
      /* storage full/unavailable: drafts are best-effort */
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
     not the browser's native tooltips. enhance must always run. The fields
     scroll; the EditorFooter stays pinned so Save/Cancel never fall below the
     fold. -->
<form method="POST" action="?/save" class="editor-form" novalidate use:enhance={submit}>
  <Scroller fill padding="16px" data-lenis-prevent>
   <div class="editor">
  {#if draft.edit}
    <input type="hidden" name="edit" value="1" />
    <input type="hidden" name="original_name" value={draft.originalName} />
  {/if}

  <Field label={t('commandEditor.name')} hint={draft.edit ? t('commandEditor.renameHint') : undefined}>
    <input class="bb-input" name="name" placeholder={t('commandEditor.namePlaceholder')} bind:value={draft.name} required />
    <FieldError message={errors.name} />
  </Field>

  <Field label={t('commandEditor.altNames')} tag={t('common.optional')}>
    <AliasChips bind:this={chips} bind:aliases={draft.aliases} bind:draft={aliasDraft} commandName={draft.name} />
    {#each draft.aliases as a}
      <input type="hidden" name="aliases" value={a} />
    {/each}
    <FieldError message={errors.aliases} />
  </Field>

  <Field label={t('commandEditor.response')}>
    <ResponseEditor
      bind:value={draft.response}
      maxLines={RESPONSE_MAX_LINES}
      {fetchDefs}
      {fetchKeys}
      onFetchDefsChanged={onFetchDefsChanged}
    />
    <FieldError message={errors.response} />
  </Field>

  <ChatPreview name={draft.name} response={draft.response} />

  <!-- Two fields sharing a row: the `Grid` block, not a scoped flex rule.
       Equal columns on the 12px step, which is what `.field-row`'s
       `flex: 1; min-width: 0` pair spelled the long way. The phone collapse
       comes with the contract (one column under 640px) and used to be a
       480px media query here; the wider breakpoint is the system's, and a
       select plus a number input at 480-640px was already tight. -->
  <Grid cols={2} gap={3}>
    <Field label={t('commandEditor.access')}>
      <select class="bb-input" name="perm" bind:value={draft.perm}>
        {#each PERMS as p}
          <option value={p}>{PERM_LABELS[p]}</option>
        {/each}
      </select>
    </Field>

    <Field label={t('commandEditor.cooldownS')}>
      <input class="bb-input" type="number" name="cooldown" min="0" max={COOLDOWN_MAX} bind:value={draft.cooldown} />
      <FieldError message={errors.cooldown} />
    </Field>
  </Grid>

  <Field label={t('commandEditor.restrictUser')} tag={t('common.optional')}>
    <input
      class="bb-input"
      name="allowed_user_id"
      inputmode="numeric"
      placeholder={t('commandEditor.restrictPlaceholder')}
      bind:value={draft.allowed_user_id}
    />
    <FieldError message={errors.allowed_user_id} />
  </Field>

  <div class="check">
    {#if draft.edit && onToggleActive}
      <!-- type=button: this form is ?/save; a nested toggle form is invalid
           HTML. The Switch posts via onToggleActive (same optimistic toggle
           as the row) so Save cannot write a stale Active snapshot (#221). -->
      <input type="hidden" name="is_active" value={liveActive ? 'on' : ''} />
      <div class="live-active">
        <span class="live-lbl">{t('commandEditor.active')}</span>
        <Switch
          checked={liveActive}
          pending={busy}
          onchange={() => onToggleActive(!liveActive)}
          label={t('commandEditor.active')}
        />
      </div>
    {:else}
      <CheckButton name="is_active" bind:checked={draft.is_active} label={t('commandEditor.active')} />
    {/if}
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


  .check { margin: 4px 0 14px; }
  .check :global(.cb) { align-items: center; }
  .live-active {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  .live-lbl {
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    color: var(--bb-white, var(--bb-text, #e8e0d6));
  }
</style>
