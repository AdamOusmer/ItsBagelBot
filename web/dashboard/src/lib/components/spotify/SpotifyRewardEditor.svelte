<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { namespaceReplySamples, getI18n } from '@bagel/kit';
  import { Button, Code, Field, Text } from '@bagel/ui/svelte';
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
    user: t('spotify.reward.previewUserSample'),
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
  <Text size="sm" tone="muted">
    {t('spotify.reward.inputHint')} <Code>Blinding Lights</Code>. {t('spotify.reward.inputHintPair')}
    <Code>The Weeknd - Blinding Lights</Code>. {t('spotify.reward.inputHintLink')}
  </Text>

  <Field label={t('spotify.fields.title')} error={titleError} errorId="spotify-title-err">
    <Input
      fill
      invalid={!!titleError}
      type="text"
      maxlength={45}
      bind:value={draft.title}
      aria-invalid={titleError ? 'true' : undefined}
      aria-describedby={titleError ? 'spotify-title-err' : undefined}
      required
    />
  </Field>

  <div class="field-row">
    <div class="field-grow">
      <Field label={t('spotify.fields.cost')} error={costError} errorId="spotify-cost-err">
        <Input
          fill
          invalid={!!costError}
          type="number"
          min={1}
          max={10000000}
          bind:value={draft.cost}
          aria-invalid={costError ? 'true' : undefined}
          aria-describedby={costError ? 'spotify-cost-err' : undefined}
          required
        />
      </Field>
    </div>
    <div class="color-field">
      <Field label={t('spotify.fields.color')}>
        <span class="color-row">
          <span class="swatch"><Input data-cursor type="color" bind:value={draft.color} /></span>
          <Text as="span" size="xs" mono tone="accent">{draft.color.toUpperCase()}</Text>
        </span>
      </Field>
    </div>
  </div>

  <Field label={t('spotify.fields.cooldown')} tag={t('spotify.fields.cooldownTag')} error={cooldownError} errorId="spotify-cooldown-err">
    <DurationField
      bind:value={draft.cooldown}
      min={0}
      max={SPOTIFY_COOLDOWN_MAX}
      label={t('spotify.fields.cooldown')}
      invalid={!!cooldownError}
      describedby={cooldownError ? 'spotify-cooldown-err' : undefined}
    />
  </Field>

  <Field label={t('spotify.fields.reply')} tag={t('common.optional')}>
    <ResponseEditor bind:value={draft.replyMessage} surface="reward:spotify" placeholder={DEFAULT_REPLY} />
  </Field>
  <ChatPreview kind="reply" response={draft.replyMessage || DEFAULT_REPLY} showViewer={false} tag={t('spotify.reward.previewTag')} samples={replySamples} />

  <Field label={t('spotify.after.title')}>
    <Select
      fill
      options={[
        { value: 'fulfill', label: t('spotify.after.fulfill') },
        { value: 'cancel', label: t('spotify.after.cancel') },
        { value: 'leave', label: t('spotify.after.leave') }
      ]}
      bind:value={draft.onRedeem}
    />
  </Field>

  {#if canDelete}
    <div class="del-row">
      <Button type="button" onclick={onRequestDelete} disabled={busy} tone="danger">{t('spotify.delete.reward')}</Button>
    </div>
  {/if}
</div>

<style>
  .editor { --field-mb: 0; padding: 4px 2px 2px; display: grid; gap: 14px; }

  .field-row { display: flex; gap: 12px; align-items: flex-start; }
  .field-grow { flex: 1; min-width: 0; }

  .color-field { flex: none; width: 116px; }
  .color-row { display: flex; align-items: center; gap: 8px; }
  .swatch { display: block; flex: none; width: 44px; }

  .del-row { display: flex; }

  @media (max-width: 480px) {
    .field-row { flex-direction: column; gap: 12px; }
    .color-field { width: 100%; }
  }
</style>
