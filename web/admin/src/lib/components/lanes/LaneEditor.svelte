<script lang="ts">
  import Input from '@bagel/ui/svelte/Input.svelte';
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import type { SubmitFunction } from '@sveltejs/kit';
  import { enhance } from '$app/forms';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import EditorFooter from '@bagel/ui/svelte/EditorFooter.svelte';
  import type { InspectorStatus } from '@bagel/kit';
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
      <dl class="facts">
        <div>
          <dt>{t('admin.lanes.factStream')}</dt>
          <dd>{lane.stream}</dd>
        </div>
        <div>
          <dt>{t('admin.lanes.factConsumer')}</dt>
          <dd>{lane.consumer}</dd>
        </div>
        <div>
          <dt>{t('admin.lanes.factSubject')}</dt>
          <dd>{lane.subject || '-'}</dd>
        </div>
        <div>
          <dt>{t('admin.lanes.factCategory')}</dt>
          <dd>{lane.category}</dd>
        </div>
        <div>
          <dt>{t('admin.lanes.factPending')}</dt>
          <dd>{lane.pending.toLocaleString()}</dd>
        </div>
        <div>
          <dt>{t('admin.lanes.factInFlight')}</dt>
          <dd>{lane.inFlight}</dd>
        </div>
        <div>
          <dt>{t('admin.lanes.factRate')}</dt>
          <dd>{lane.rate}</dd>
        </div>
        <div>
          <dt>{t('admin.lanes.factRedelivered')}</dt>
          <dd class:err={lane.redelivered > 0}>{lane.redelivered}</dd>
        </div>
      </dl>

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
      <p class="note">{t('admin.lanes.aliasHint')}</p>

      {#if canMutate}
        <section class="block">
          <h3 class="block-label">{t('admin.lanes.durableLabel')}</h3>
          <Switch
            checked={!lane.ephemeral}
            label={t('admin.lanes.durableLabel')}
            describedby="lane-durable-hint"
            disabled={!lane.ephemeral || lane.orphan}
            pending={busy}
            onchange={onDurable}
          />
          <p class="note" id="lane-durable-hint">
            {lane.ephemeral ? t('admin.lanes.durableHint') : t('admin.lanes.durableAlready')}
          </p>
        </section>

        {#if lane.orphan}
          <section class="block">
            <h3 class="block-label">{t('admin.lanes.dangerTitle')}</h3>
            <p class="note">{t('admin.lanes.deleteHint')}</p>
            <Button variant="destructive" disabled={busy} onclick={onDelete}>
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

  .facts {
    display: flex;
    flex-direction: column;
    gap: 7px;
    margin: 0;
  }
  .facts div {
    display: flex;
    justify-content: space-between;
    gap: 12px;
    align-items: baseline;
  }
  .facts dt {
    font-family: var(--bb-font-body);
    font-size: 12px;
    color: var(--bb-muted);
    flex: none;
  }
  .facts dd {
    margin: 0;
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-tan-light);
    text-align: right;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .facts dd.err {
    color: var(--bb-status-error);
  }

  .block {
    display: flex;
    flex-direction: column;
    gap: 9px;
    align-items: flex-start;
  }
  .block-label {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--bb-muted);
    margin: 0;
  }
  .note {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    line-height: 1.5;
    color: var(--bb-muted);
    margin: 0;
  }
</style>
