<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Inspector body: create/edit the one channel-points reward bound to song
  // requests. Named form inputs post straight to ?/saveReward (the page owns
  // the enhance handler); local state drives the live ChatPreview rehearsal.
  import { tick } from 'svelte';
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Button, Field, EditorFooter, getI18n, type SpotifyRedeemConfig } from '@bagel/shared';
  import ResponseEditor from '$lib/components/commands/ResponseEditor.svelte';
  import ChatPreview from '$lib/components/commands/ChatPreview.svelte';

  let {
    redeem,
    busy = false,
    onSubmit,
    onCancel,
    onRequestDelete
  }: {
    redeem: SpotifyRedeemConfig;
    busy?: boolean;
    onSubmit: SubmitFunction;
    onCancel: () => void;
    onRequestDelete: () => void;
  } = $props();

  const { t } = getI18n();

  const DEFAULT_REPLY = '@{user} queued {track}!';
  const REPLY_TOKENS = [
    { token: '{user}', label: t('spotify.replyTokUser') },
    { token: '{track}', label: t('spotify.replyTokTrack') },
    { token: '{input}', label: t('spotify.replyTokInput') }
  ];
  const replySamples: Record<string, string> = {
    user: t('spotify.previewSamplesUser'),
    track: 'Never Gonna Give You Up',
    input: 'rick roll'
  };

  // Seeded once per mount (the page re-keys this component when switching
  // between create and edit), so capturing the initial binding is intentional.
  // svelte-ignore state_referenced_locally
  const isNew = !redeem.rewardId;

  // svelte-ignore state_referenced_locally
  let title = $state(redeem.reward?.title || t('spotify.defaultTitle'));
  // svelte-ignore state_referenced_locally
  let cost = $state(redeem.reward?.cost ?? 500);
  // svelte-ignore state_referenced_locally
  let color = $state(redeem.reward?.color || '#1db954');
  // svelte-ignore state_referenced_locally
  let cooldown = $state(redeem.reward?.cooldown ?? 0);
  // svelte-ignore state_referenced_locally
  let onRedeem = $state<string>(redeem.onRedeem ?? 'fulfill');
  // svelte-ignore state_referenced_locally
  let replyMessage = $state(redeem.replyMessage ?? '');

  // --- Client-side gate: a blank title is the one thing the server can't
  // recover, so validate it before submit and land the caret on it, with the
  // error associated to the input via aria-describedby. ------------------------
  const TITLE_ERR_ID = 'spotify-title-err';
  let titleError = $state<string | undefined>(undefined);
  let formEl = $state<HTMLFormElement | null>(null);

  async function focusFirstInvalid() {
    await tick();
    formEl?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
  }

  const submit: SubmitFunction = (input) => {
    titleError = title.trim() ? undefined : t('spotify.errTitleRequired');
    if (titleError) {
      input.cancel();
      void focusFirstInvalid();
      return;
    }
    return onSubmit(input);
  };
</script>

<form method="POST" action="?/saveReward" class="editor" novalidate use:enhance={submit} bind:this={formEl}>
  <p class="hint">
    {t('spotify.editorInputHint')} <code>Blinding Lights</code>. {t('spotify.editorInputHintPair')}
    <code>The Weeknd - Blinding Lights</code>. {t('spotify.editorInputHintLink')}
  </p>

  <Field label={t('spotify.fieldTitle')}>
    <input
      class="input"
      type="text"
      name="title"
      maxlength="45"
      bind:value={title}
      aria-invalid={titleError ? 'true' : undefined}
      aria-describedby={titleError ? TITLE_ERR_ID : undefined}
      required
    />
    {#if titleError}<small id={TITLE_ERR_ID} class="field-error" role="alert">{titleError}</small>{/if}
  </Field>

  <div class="field-row">
    <Field label={t('spotify.fieldCost')}>
      <input class="input" type="number" name="cost" min="1" max="10000000" bind:value={cost} required />
    </Field>
    <!-- Colour: a labelled native picker PLUS a text hex readout, so the chosen
         value is legible without relying on the swatch colour alone. -->
    <label class="color-field">
      <span class="color-label">{t('spotify.fieldColor')}</span>
      <span class="color-row">
        <input class="color-in" type="color" name="color" bind:value={color} />
        <span class="color-hex">{color}</span>
      </span>
    </label>
  </div>

  <Field label={t('spotify.fieldCooldown')} tag={t('spotify.fieldCooldownTag')}>
    <input class="input" type="number" name="cooldown" min="0" max="604800" bind:value={cooldown} />
  </Field>

  <Field label={t('spotify.fieldReply')} tag={t('common.optional')}>
    <ResponseEditor bind:value={replyMessage} name="replyMessage" tokens={REPLY_TOKENS} placeholder={DEFAULT_REPLY} />
  </Field>
  <!-- kind="reply" + dynamic={false}: the spotify reply is a bare
       {user}/{track}/{input} string replacer: nothing else ever expands. -->
  <ChatPreview kind="reply" dynamic={false} response={replyMessage || DEFAULT_REPLY} showViewer={false} tag={t('spotify.previewTag')} samples={replySamples} />

  <Field label={t('spotify.afterTitle')}>
    <select class="input" name="onRedeem" bind:value={onRedeem}>
      <option value="fulfill">{t('spotify.afterFulfill')}</option>
      <option value="cancel">{t('spotify.afterCancel')}</option>
      <option value="leave">{t('spotify.afterLeave')}</option>
    </select>
  </Field>

  {#if !isNew}
    <div class="del-row">
      <Button variant="destructive" icon="trash" onclick={onRequestDelete} disabled={busy}>{t('spotify.deleteReward')}</Button>
    </div>
  {/if}

  <EditorFooter
    onCancel={onCancel}
    canSave={!busy}
    status={busy ? 'saving' : 'idle'}
    saveLabel={isNew ? t('spotify.create') : t('spotify.saveChanges')}
    savingLabel={t('spotify.saving')}
    cancelLabel={t('common.cancel')}
  />
</form>

<style>
  .editor { padding: 4px 2px 2px; display: grid; gap: 14px; }
  .hint { margin: 0; font-family: var(--bb-font-body); font-size: 12.5px; line-height: 1.55; color: var(--bb-muted); }

  /* Field owns label + wiring; strip its default bottom margin inside the grid. */
  .editor :global(.field) { margin-bottom: 0; }
  .input {
    padding: 8px 12px;
    border-radius: 6px;
    border: 1px solid var(--rule);
    background: rgba(240, 236, 228, 0.04);
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 13px;
    width: 100%;
    box-sizing: border-box;
    transition: border-color var(--bb-dur-fast, 140ms) ease;
  }
  .input:focus { outline: none; border-color: var(--bb-tan, #c9a87c); }

  .field-error { display: block; margin-top: 4px; font-family: var(--bb-font-body); font-size: 11.5px; color: #cf8a78; }

  .field-row { display: flex; gap: 12px; align-items: flex-start; }
  .field-row :global(.field) { flex: 1; min-width: 0; }

  .color-field { display: flex; flex-direction: column; gap: 6px; flex: none; width: 116px; }
  .color-label { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); }
  .color-row { display: flex; align-items: center; gap: 8px; }
  .color-in {
    width: 44px;
    height: 37px;
    padding: 3px;
    border: 1px solid var(--rule);
    border-radius: 6px;
    background: rgba(240, 236, 228, 0.04);
    cursor: pointer;
    flex: none;
  }
  .color-hex { font-family: var(--bb-font-mono, monospace); font-size: 12px; color: var(--bb-tan-light); text-transform: uppercase; }

  .del-row { display: flex; }

  code { font-family: var(--bb-font-mono, monospace); font-size: 0.86em; color: var(--bb-tan-light); }

  @media (max-width: 480px) {
    .field-row { flex-direction: column; gap: 12px; }
    .color-field { width: 100%; }
  }
</style>
