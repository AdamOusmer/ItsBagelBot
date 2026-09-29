<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { namespaceReplySamples, Button, Code, Field, getI18n } from '@bagel/kit';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import ResponseEditor from '$lib/components/commands/ResponseEditor.svelte';
  import ChatPreview from '$lib/components/commands/ChatPreview.svelte';
  import DurationField from '$lib/components/shared/DurationField.svelte';
  import { SPOTIFY_COOLDOWN_MAX, spotifyErrors, type SpotifyErrorField, type SpotifyRewardDraft } from './spotify-draft';

  let {
    draft = $bindable<SpotifyRewardDraft>(),
    canDelete,
    busy = false,
    attempted = false,
    onRequestDelete
  }: {
    draft: SpotifyRewardDraft;
    canDelete: boolean;
    busy?: boolean;
    attempted?: boolean;
    onRequestDelete: () => void;
  } = $props();

  const { t } = getI18n();

  const DEFAULT_REPLY = '@{songqueue:user} queued {songqueue:track}!';
  const replySamples: Record<string, string> = namespaceReplySamples('songqueue', {
    user: t('spotify.previewUserSample'),
    track: 'Never Gonna Give You Up',
    input: 'rick roll',
    pos: '3'
  });

  const errors = $derived(spotifyErrors(draft));
  const shown = (field: SpotifyErrorField) => (attempted && errors[field] ? t(errors[field]!) : undefined);
  const titleError = $derived(shown('title'));
  const costError = $derived(shown('cost'));
  const cooldownError = $derived(shown('cooldown'));
</script>

<div class="editor">
  <p class="hint">
    {t('spotify.editorInputHint')} <Code>Blinding Lights</Code>. {t('spotify.editorInputHintPair')}
    <Code>The Weeknd - Blinding Lights</Code>. {t('spotify.editorInputHintLink')}
  </p>

  <Field label={t('spotify.fieldTitle')} error={titleError} errorId="spotify-title-err">
    <Input
      fill
      invalid={!!titleError}
      type="text"
      maxlength="45"
      bind:value={draft.title}
      aria-invalid={titleError ? 'true' : undefined}
      aria-describedby={titleError ? 'spotify-title-err' : undefined}
      required
    />
  </Field>

  <div class="field-row">
    <div class="field-grow">
      <Field label={t('spotify.fieldCost')} error={costError} errorId="spotify-cost-err">
        <Input
          fill
          invalid={!!costError}
          type="number"
          min="1"
          max="10000000"
          bind:value={draft.cost}
          aria-invalid={costError ? 'true' : undefined}
          aria-describedby={costError ? 'spotify-cost-err' : undefined}
          required
        />
      </Field>
    </div>
    <label class="color-field">
      <span class="color-label">{t('spotify.fieldColor')}</span>
      <span class="color-row">
        <input data-cursor class="color-in" type="color" bind:value={draft.color} />
        <span class="color-hex">{draft.color}</span>
      </span>
    </label>
  </div>

  <Field label={t('spotify.fieldCooldown')} tag={t('spotify.fieldCooldownTag')} error={cooldownError} errorId="spotify-cooldown-err">
    <DurationField
      bind:value={draft.cooldown}
      min={0}
      max={SPOTIFY_COOLDOWN_MAX}
      label={t('spotify.fieldCooldown')}
      invalid={!!cooldownError}
      describedby={cooldownError ? 'spotify-cooldown-err' : undefined}
    />
  </Field>

  <Field label={t('spotify.fieldReply')} tag={t('common.optional')}>
    <ResponseEditor bind:value={draft.replyMessage} surface="reward:spotify" placeholder={DEFAULT_REPLY} />
  </Field>
  <ChatPreview kind="reply" response={draft.replyMessage || DEFAULT_REPLY} showViewer={false} tag={t('spotify.previewTag')} samples={replySamples} />

  <Field label={t('spotify.afterTitle')}>
    <Select
      fill
      options={[
        { value: 'fulfill', label: t('spotify.afterFulfill') },
        { value: 'cancel', label: t('spotify.afterCancel') },
        { value: 'leave', label: t('spotify.afterLeave') }
      ]}
      bind:value={draft.onRedeem}
    />
  </Field>

  {#if canDelete}
    <div class="del-row">
      <Button variant="destructive" type="button" onclick={onRequestDelete} disabled={busy}>{t('spotify.deleteReward')}</Button>
    </div>
  {/if}
</div>

<style>
  .editor { --field-mb: 0; padding: 4px 2px 2px; display: grid; gap: 14px; }
  .hint { margin: 0; font-family: var(--bb-font-body); font-size: 12.5px; line-height: 1.55; color: var(--bb-muted); }

  .field-row { display: flex; gap: 12px; align-items: flex-start; }
  .field-grow { flex: 1; min-width: 0; }

  .color-field { display: flex; flex-direction: column; gap: 6px; flex: none; width: 116px; }
  .color-label { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); }
  .color-row { display: flex; align-items: center; gap: 8px; }
  .color-in {
    width: 44px;
    height: 37px;
    padding: 3px;
    border: 1px solid var(--rule);
    border-radius: var(--bb-radius-sm);
    background: rgba(240, 236, 228, 0.04);
    cursor: pointer;
    flex: none;
  }
  .color-hex { font-family: var(--bb-font-mono, monospace); font-size: 12px; color: var(--bb-tan-light); text-transform: uppercase; }

  .del-row { display: flex; }

  @media (max-width: 480px) {
    .field-row { flex-direction: column; gap: 12px; }
    .color-field { width: 100%; }
  }
</style>
