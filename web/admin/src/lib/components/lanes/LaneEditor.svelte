<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Input from '@bagel/ui/svelte/Input.svelte';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { enhance } from '$app/forms';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import EditorFooter from '@bagel/ui/svelte/EditorFooter.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import type { InspectorStatus } from '@bagel/ui/lib/inspector-machine';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { LaneView } from '$lib/server/lanes';
  import { ALIAS_MAX, type LaneDraft } from './lane-view';

  let {
    draft = $bindable(),
    lane,
    canMutate,
    status,
    dirty,
    canSave,
    busy,
    onCancel,
    onSubmit,
    onDurable,
    onDelete
  }: {
    draft: LaneDraft;
    lane: LaneView;
    canMutate: boolean;
    status: InspectorStatus;
    dirty: boolean;
    canSave: boolean;
    busy: boolean;
    onCancel: () => void;
    onSubmit: SubmitFunction;
    onDurable: () => void;
    onDelete: () => void;
  } = $props();

  const { t } = getI18n();
</script>

<form class="editor" method="POST" action="?/alias" use:enhance={onSubmit}>
  <input type="hidden" name="stream" value={lane.stream} />
  <input type="hidden" name="consumer" value={lane.consumer} />
  <input type="hidden" name="alias" value={draft.alias} />

  <Scroller fill padding="18px" smooth>
    <div class="body">
      <FactList>
        <Fact term={t('admin.lanes.factStream')}>{lane.stream}</Fact>
        <Fact term={t('admin.lanes.factConsumer')}>{lane.consumer}</Fact>
        <Fact term={t('admin.lanes.factSubject')}>{lane.subject || '-'}</Fact>
        <Fact term={t('admin.lanes.factCategory')}>{lane.category}</Fact>
        {#if lane.mode}
          <Fact term={t('admin.lanes.modeLabel')}>
            {lane.mode === 'pull' ? t('admin.lanes.modePull') : t('admin.lanes.modePush')}
          </Fact>
        {/if}
        {#if lane.connection}
          <Fact term={t('admin.lanes.connectionLabel')}>
            {lane.connection === 'bound' ? t('admin.lanes.connectionBound')
              : lane.connection === 'waiting' ? t('admin.lanes.connectionWaiting')
              : lane.connection === 'unbound' ? t('admin.lanes.connectionUnbound')
              : t('admin.lanes.connectionUnknown')}
          </Fact>
        {/if}
        {#if lane.mode === 'pull'}
          <Fact term={t('admin.lanes.waitingPulls')}>{lane.waiting?.toLocaleString() ?? '-'}</Fact>
        {/if}
        <Fact term={t('admin.lanes.factPending')}>{lane.pending.toLocaleString()}</Fact>
        <Fact term={t('admin.lanes.factDelivered')}>{lane.delivered?.toLocaleString() ?? '-'}</Fact>
        <Fact term={t('admin.lanes.awaitingAck')}>{lane.inFlight}</Fact>
        <Fact term={t('admin.lanes.factRate')}>{lane.rate === '-' ? t('admin.lanes.sampling') : lane.rate}</Fact>
        <Fact term={t('admin.lanes.factRedelivered')} tone={lane.redelivered > 0 ? 'danger' : undefined}>
          {lane.redelivered}
        </Fact>
      </FactList>
      <Text size="sm" tone="muted">{t('admin.lanes.capacityHint')}</Text>

      <Field label={t('admin.lanes.fieldAlias')}>
        <Input
          fill mono
          type="text"
          maxlength={ALIAS_MAX}
          disabled={!canMutate}
          placeholder={lane.consumer}
          bind:value={draft.alias}
        />
      </Field>
      <Text size="sm" tone="muted">{t('admin.lanes.aliasHint')}</Text>

      {#if canMutate}
        <section class="block">
          <Heading level={3} variant="label">{t('admin.lanes.durableLabel')}</Heading>
          <Switch
            checked={!lane.ephemeral}
            label={t('admin.lanes.durableLabel')}
            describedby="lane-durable-hint"
            disabled={!lane.ephemeral || lane.orphan}
            busy={busy}
            onCheckedChange={onDurable}
          />
          <Text size="sm" tone="muted" id="lane-durable-hint">
            {lane.ephemeral ? t('admin.lanes.durableHint') : t('admin.lanes.durableAlready')}
          </Text>
        </section>

        {#if lane.orphan}
          <section class="block">
            <Heading level={3} variant="label">{t('admin.lanes.dangerTitle')}</Heading>
            <Text size="sm" tone="muted">{t('admin.lanes.deleteHint')}</Text>
            <Button disabled={busy} onclick={onDelete} tone="danger">
              {t('common.delete')}
            </Button>
          </section>
        {/if}
      {/if}
    </div>
  </Scroller>

  {#if canMutate}
    <EditorFooter
      {status}
      {dirty}
      {canSave}
      saveLabel={t('common.save')}
      cancelLabel={t('common.cancel')}
      savingLabel={t('admin.saving')}
      savedLabel={t('admin.saved')}
      dirtyLabel={t('admin.unsaved')}
      errorLabel={t('admin.saveFailed')}
      {onCancel}
    />
  {/if}
</form>

<style>
  .editor {
    display: flex;
    flex-direction: column;
    min-height: 0;
    max-height: 100%;
  }
  .body {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .block {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-2);
    align-items: flex-start;
  }
</style>
