<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Code,
    Field,
    Switch,
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
    busy = false
  }: {
    command: CommandView;
    def: BuiltinCommandDef;
    toggleSubmit: SubmitFunction;
    replySubmit?: SubmitFunction;
    busy?: boolean;
  } = $props();

  const { t } = getI18n();
  const c = $derived(command);

  let message = $state('');
  let seededFor = $state<string | null>(null);
  $effect(() => {
    if (command.name !== seededFor) {
      seededFor = command.name;
      message = command.response;
    }
  });

  const rehearsalSamples = $derived(Object.fromEntries((def.tokens ?? []).map((tk) => [tk.name, tk.sample])));
  const effectiveMessage = $derived(message.trim() ? message : def.preview);
</script>

<div class="editor builtin">
  <div class="toggle-row">
    <div class="tr-text">
      <span class="tr-label">{t('builtinInspector.enabled')}</span>
      <span class="tr-help">{t('builtinInspector.enabledHelp')}</span>
    </div>
    <form method="POST" action="?/toggleBuiltin" use:enhance={toggleSubmit}>
      <input type="hidden" name="name" value={c.name} />
      <input type="hidden" name="is_active" value={c.is_active ? '' : 'on'} />
      <Switch type="submit" checked={c.is_active} label={t('commandRow.toggleAria', { name: c.name })} />
    </form>
  </div>

  <p class="desc">{def.description}</p>

  <Field label={t('builtinInspector.usage')}>
    <ul class="usage">
      {#each def.usage as u}<li><Code>{u}</Code></li>{/each}
    </ul>
  </Field>

  {#if def.editable && replySubmit}
    <form class="reply-form" method="POST" action="?/saveBuiltinReply" use:enhance={replySubmit}>
      <input type="hidden" name="name" value={c.name} />
      <input type="hidden" name="is_active" value={c.is_active ? 'on' : ''} />
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
        <button class="bb-btn bb-btn--primary" type="submit" disabled={busy}>
          {t('builtinInspector.saveReply')}
        </button>
      </div>
    </form>
  {:else}
    <Field label={t('builtinInspector.preview')}>
      <ChatPreview kind="reply" dynamic={false} name={def.id} args={def.previewArgs ?? ''} response={def.preview} samples={rehearsalSamples} />
    </Field>
  {/if}

  <div class="field-row">
    <div class="field">
      <span>{t('builtinInspector.access')}</span>
      <div class="ro">{tPerm(t, (c.perm ?? def.defaultPerm) as Perm)}</div>
    </div>
    <div class="field">
      <span>{t('builtinInspector.cooldown')}</span>
      <div class="ro">{c.cooldown ?? def.defaultCooldown}s</div>
    </div>
  </div>
</div>

<style>
  .editor {
    padding: 4px 2px 2px;
    font-family: var(--bb-font-body);
  }

  .toggle-row {
    display: flex;
    align-items: center;
    gap: 14px;
    padding-bottom: 16px;
    margin-bottom: 16px;
    border-bottom: 1px solid var(--rule, var(--glass-border));
  }
  .tr-text {
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .tr-label {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 13.5px;
    color: var(--bb-white);
  }
  .tr-help {
    font-size: 12px;
    color: var(--bb-muted);
    line-height: 1.4;
  }
  .toggle-row form {
    margin-left: auto;
  }

  .desc {
    margin: 0 0 16px;
    font-size: 13px;
    line-height: 1.6;
    color: var(--bb-muted);
  }

  .reply-form { margin-bottom: 14px; }
  .reply-actions { display: flex; justify-content: flex-end; margin-top: 12px; }
  @media (max-width: 480px) {
    .reply-actions { --btn-w: 100%; --btn-justify: center; --btn-min-h: 44px; }
  }

  .field-row {
    display: flex;
    gap: 12px;
  }
  .field-row .field {
    flex: 1;
    min-width: 0;
  }

  .usage {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .ro {
    box-sizing: border-box;
    width: 100%;
    padding: 9px 12px;
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-sm);
    background: rgba(255, 255, 255, 0.02);
    color: var(--bb-white);
    font-family: var(--bb-font-mono);
    font-size: 13px;
  }
</style>
