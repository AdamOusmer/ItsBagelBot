<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import { namespaceReplyTemplate } from '@bagel/kit';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Button,
    Code,
    Field,
    Input,
    Select,
    SwitchRow,
    Text
  } from '@bagel/ui/svelte';
  import {
    PERMS,
    getI18n,
    tPerm,
    type CommandView,
    type BuiltinCommandDef,
    type Perm
  } from '@bagel/kit';
  import ChatPreview from './ChatPreview.svelte';
  import ResponseEditor from './ResponseEditor.svelte';

  let {
    command,
    def,
    toggleSubmit,
    replySubmit,
    accessSubmit,
    busy = false
  }: {
    command: CommandView;
    def: BuiltinCommandDef;
    toggleSubmit: SubmitFunction;
    replySubmit?: SubmitFunction;
    accessSubmit: SubmitFunction;
    busy?: boolean;
  } = $props();

  const { t } = getI18n();
  const c = $derived(command);

  let message = $state('');
  let access = $state<Perm>('everyone');
  let seededFor = $state<string | null>(null);
  let savedAccess = $state<Perm | null>(null);
  $effect(() => {
    const currentAccess = (command.perm ?? def.defaultPerm) as Perm;
    if (command.name !== seededFor) {
      seededFor = command.name;
      message = namespaceReplyTemplate(def.id, def, command.response);
      access = currentAccess;
      savedAccess = currentAccess;
    } else if (currentAccess !== savedAccess) {
      adoptServerAccessUnlessEdited(currentAccess);
    }
  });

  function adoptServerAccessUnlessEdited(next: Perm) {
    if (access === savedAccess) access = next;
    savedAccess = next;
  }

  const rehearsalSamples = $derived(Object.fromEntries((def.tokens ?? []).map((tk) => [tk.name, tk.sample])));
  const effectiveMessage = $derived(message.trim() ? message : def.preview);
  const saveReply: SubmitFunction = (input) => {
    message = namespaceReplyTemplate(def.id, def, message);
    input.formData.set('reply', message);
    return replySubmit?.(input);
  };
</script>

<div class="editor builtin">
  <form class="toggle-row" method="POST" action="?/toggleBuiltin" use:enhance={toggleSubmit}>
    <input type="hidden" name="name" value={c.name} />
    <input type="hidden" name="perm" value={c.perm ?? def.defaultPerm} />
    <input type="hidden" name="response" value={c.response} />
    <input type="hidden" name="is_active" value={c.is_active ? '' : 'on'} />
    <SwitchRow
      control="end"
      type="submit"
      checked={c.is_active}
      label={t('builtinInspector.enabled')}
      hint={t('builtinInspector.enabledHelp')}
      hintId="builtin-enabled-hint"
      switchLabel={t('commandRow.toggleAria', { name: c.name })}
    />
  </form>

  <div class="desc"><Text size="sm" tone="muted">{def.description}</Text></div>

  <Field label={t('builtinInspector.usage')}>
    <ul class="usage">
      {#each def.usage as u}<li><Code>{u}</Code></li>{/each}
    </ul>
  </Field>

  {#if def.editable && replySubmit}
    <form class="reply-form" method="POST" action="?/saveBuiltinReply" use:enhance={saveReply}>
      <input type="hidden" name="name" value={c.name} />
      <input type="hidden" name="is_active" value={c.is_active ? 'on' : ''} />
      <input type="hidden" name="perm" value={c.perm ?? def.defaultPerm} />
      <Field label={t('builtinInspector.replyMessage')} hint={t('builtinInspector.replyHint')}>
        <ResponseEditor name="reply" bind:value={message} surface={{ builtin: def.id }} placeholder={def.preview} />
      </Field>
      <ChatPreview
        kind="reply"
        dynamic={false}
        name={def.id}
        args={def.previewArgs ?? ''}
        response={effectiveMessage}
        samples={rehearsalSamples}
      />
      <div class="reply-actions">
        <Button variant="primary" type="submit" disabled={busy}>
          {t('builtinInspector.saveReply')}
        </Button>
      </div>
    </form>
  {:else}
    <Field label={t('builtinInspector.preview')}>
      <ChatPreview kind="reply" dynamic={false} name={def.id} args={def.previewArgs ?? ''} response={def.preview} samples={rehearsalSamples} />
    </Field>
  {/if}

  <div class="field-row">
    <form class="access-form" method="POST" action="?/saveBuiltinAccess" use:enhance={accessSubmit}>
      <input type="hidden" name="name" value={c.name} />
      <input type="hidden" name="response" value={c.response} />
      <input type="hidden" name="is_active" value={c.is_active ? 'on' : ''} />
      <Field label={t('builtinInspector.access')}>
        <Select
          fill
          name="perm"
          bind:value={access}
          options={PERMS.map((p) => ({ value: p, label: tPerm(t, p) }))}
        />
      </Field>
      <div class="access-save">
        <Button variant="primary" type="submit" disabled={busy || access === (c.perm ?? def.defaultPerm)}>
          {t('builtinInspector.saveAccess')}
        </Button>
      </div>
    </form>
    <Field label={t('builtinInspector.cooldown')}>
      <Input readonly mono fill value={`${c.cooldown ?? def.defaultCooldown}s`} />
    </Field>
  </div>
</div>

<style>
  .editor {
    padding: 4px 2px 2px;
    font-family: var(--bb-font-body);
  }

  .toggle-row {
    padding-bottom: 16px;
    margin-bottom: 16px;
    border-bottom: 1px solid var(--bb-border);
  }

  .desc { margin-bottom: 16px; }

  .reply-form { margin-bottom: 14px; }
  .reply-actions { display: flex; justify-content: flex-end; margin-top: 12px; }
  @media (max-width: 480px) {
    .reply-actions { --btn-w: 100%; --btn-justify: center; --btn-min-h: 44px; }
  }

  .field-row {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 12px;
    --field-mb: 0;
  }
  .access-form { display: flex; flex-direction: column; gap: 10px; }
  .access-save { align-self: flex-end; }

  .usage {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
</style>
