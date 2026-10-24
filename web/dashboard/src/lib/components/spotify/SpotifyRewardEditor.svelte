<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { namespaceReplySamples, getI18n } from '@bagel/kit';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Code from '@bagel/ui/svelte/Code.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import RewardCoreFields from '$lib/components/channelpoints/RewardCoreFields.svelte';
  import { SPOTIFY_ERROR_KEYS, spotifyErrors, type SpotifyErrorField, type SpotifyRewardDraft } from './spotify-draft';

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

  const failed = $derived(spotifyErrors(draft));
  const shown = (field: SpotifyErrorField) => (attempted && failed[field] ? t(SPOTIFY_ERROR_KEYS[field]) : undefined);
  const errors = $derived({ title: shown('title'), cost: shown('cost'), cooldown: shown('cooldown') });
</script>

<div class="editor">
  <Text size="sm" tone="muted">
    {t('spotify.reward.inputHint')} <Code>Blinding Lights</Code>. {t('spotify.reward.inputHintPair')}
    <Code>The Weeknd - Blinding Lights</Code>. {t('spotify.reward.inputHintLink')}
  </Text>

  <RewardCoreFields
    bind:draft
    {errors}
    idPrefix="spotify"
    labels={{
      title: t('spotify.fields.title'),
      cost: t('spotify.fields.cost'),
      color: t('spotify.fields.color'),
      cooldown: t('spotify.fields.cooldown'),
      cooldownTag: t('spotify.fields.cooldownTag'),
      reply: t('spotify.fields.reply'),
      after: t('spotify.after.title')
    }}
    afterOptions={[
      { value: 'fulfill', label: t('spotify.after.fulfill') },
      { value: 'cancel', label: t('spotify.after.cancel') },
      { value: 'leave', label: t('spotify.after.leave') }
    ]}
    surface="reward:spotify"
    defaultReply={DEFAULT_REPLY}
    {replySamples}
    previewTag={t('spotify.reward.previewTag')}
    cursorColor
  />

  {#if canDelete}
    <div class="del-row">
      <Button type="button" onclick={onRequestDelete} disabled={busy} tone="danger">{t('spotify.delete.reward')}</Button>
    </div>
  {/if}
</div>

<style>
  .editor { --field-mb: 0; padding: 4px 2px 2px; display: grid; gap: 14px; }

  .del-row { display: flex; }
</style>
