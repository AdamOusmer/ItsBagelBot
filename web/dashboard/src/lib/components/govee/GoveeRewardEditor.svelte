<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Code from '@bagel/ui/svelte/Code.svelte';
  import SwitchRow from '@bagel/ui/svelte/SwitchRow.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { namespaceReplySamples, getI18n } from '@bagel/kit';
  import RewardCoreFields from '$lib/components/channelpoints/RewardCoreFields.svelte';
  import { GOVEE_ERROR_KEYS, goveeErrors, type GoveeDraft, type GoveeErrorField } from './govee-draft';

  let {
    draft = $bindable<GoveeDraft>(),
    colors,
    canDelete,
    busy = false,
    attempted = false,
    onRequestDelete
  }: {
    draft: GoveeDraft;
    colors: string[];
    canDelete: boolean;
    busy?: boolean;
    attempted?: boolean;
    onRequestDelete: () => void;
  } = $props();

  const { t } = getI18n();

  const DEFAULT_REPLY = '@{govee:user} set the lights to {govee:color}!';
  const replySamples: Record<string, string> = namespaceReplySamples('govee', { user: 'sesame_sam', input: 'blue', color: 'Blue' });

  const failed = $derived(goveeErrors(draft));
  const shown = (field: GoveeErrorField) => (attempted && failed[field] ? t(GOVEE_ERROR_KEYS[field]) : undefined);
  const errors = $derived({ title: shown('title'), cost: shown('cost'), cooldown: shown('cooldown') });
</script>

<div class="editor">
  <Text size="sm" tone="muted">
    {t('govee.editorHintNames')} <Code>{colors.join(', ')}</Code>. {t('govee.editorHintHex')} <Code>#00ccff</Code>.
  </Text>

  <RewardCoreFields
    bind:draft
    {errors}
    idPrefix="govee"
    labels={{
      title: t('govee.fieldTitle'),
      cost: t('govee.fieldCost'),
      color: t('govee.fieldColor'),
      cooldown: t('govee.fieldCooldown'),
      cooldownTag: t('govee.fieldCooldownTag'),
      reply: t('govee.fieldReply'),
      after: t('govee.afterTitle')
    }}
    afterOptions={[{ value: 'fulfill', label: t('govee.afterFulfill') }, { value: 'cancel', label: t('govee.afterCancel') }, { value: 'leave', label: t('govee.afterLeave') }]}
    surface="reward:govee"
    defaultReply={DEFAULT_REPLY}
    {replySamples}
    previewTag={t('govee.previewTag')}
  />

  <div class="setrow" class:on={draft.allowOff}>
    <SwitchRow
      control="end"
      bind:checked={draft.allowOff}
      label={t('govee.allowOffLabel')}
      hint={t('govee.allowOffHint')}
      hintId="govee-allowoff-desc"
    />
  </div>

  <div class="setrow">
    <SwitchRow
      control="end"
      bind:checked={draft.liveOnly}
      tone={draft.liveOnly ? undefined : 'warning'}
      label={t('govee.liveOnlyLabel')}
      hint={draft.liveOnly ? t('govee.liveOnlyOn') : t('govee.liveOnlyOff')}
      hintId="govee-liveonly-desc"
    />
  </div>

  {#if canDelete}
    <div class="del-row">
      <Button type="button" onclick={onRequestDelete} disabled={busy} tone="danger">{t('govee.deleteReward')}</Button>
    </div>
  {/if}
</div>

<style>
  .editor { --field-mb: 0; padding: 4px 2px 2px; display: grid; gap: 14px; }

  .setrow {
    padding: 11px 12px;
    border-radius: var(--bb-radius-sm);
    box-shadow: var(--bb-elev-1);
  }
  .setrow.on { background: rgba(var(--bb-tan-rgb), 0.08); }

  .del-row { display: flex; }
</style>
