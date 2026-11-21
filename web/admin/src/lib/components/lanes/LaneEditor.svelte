<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import EditorFooter from '@bagel/ui/svelte/EditorFooter.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { InspectorStatus } from '@bagel/ui/lib/inspector-machine';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { LaneView } from '#lib/server/lanes';
  import { ALIAS_MAX, type LaneDraft } from './lane-view';

  type LaneFact = { term: string; value: string; danger?: boolean; wrap?: boolean };

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

  const { t, locale } = getI18n();
  const id = $props.id();
  const numbers = new Intl.NumberFormat(locale);

  const labels = {
    mode: { pull: 'admin.lanes.modePull', push: 'admin.lanes.modePush' },
    connection: {
      bound: 'admin.lanes.connectionBound',
      waiting: 'admin.lanes.connectionWaiting',
      unbound: 'admin.lanes.connectionUnbound',
      unknown: 'admin.lanes.connectionUnknown'
    },
    category: {
      system: 'admin.lanes.categorySystem',
      projection: 'admin.lanes.categoryProjection',
      ephemeral: 'admin.lanes.categoryEphemeral'
    }
  } as const;

  const identity = $derived([
    { term: t('admin.lanes.factStream'), value: lane.stream },
    { term: t('admin.lanes.factConsumer'), value: lane.consumer },
    { term: t('admin.lanes.factSubject'), value: lane.subject || '-', wrap: true },
    { term: t('admin.lanes.factCategory'), value: t(labels.category[lane.category]) },
    { term: t('admin.lanes.modeLabel'), value: lane.mode ? t(labels.mode[lane.mode]) : '-' },
    { term: t('admin.lanes.connectionLabel'), value: t(labels.connection[lane.connection ?? 'unknown']) }
  ]);
  const metrics = $derived([
    { term: t('admin.lanes.factPending'), value: formatCount(lane.pending) },
    { term: t('admin.lanes.factInFlight'), value: lane.inFlight },
    { term: t('admin.lanes.factRate'), value: lane.rate === '-' ? t('admin.lanes.sampling') : lane.rate },
    { term: t('admin.lanes.factDelivered'), value: formatCount(lane.delivered) },
    { term: t('admin.lanes.factRedelivered'), value: formatCount(lane.redelivered), danger: lane.redelivered > 0 },
    { term: t('admin.lanes.waitingPulls'), value: lane.mode === 'pull' ? formatCount(lane.waiting) : '-' }
  ]);
  function formatCount(value: number | undefined) {
    return value == null ? '-' : numbers.format(value);
  }
</script>

{#snippet facts(title: string, entries: LaneFact[])}
  <Stack gap={2}>
    <Heading level={2} variant="title"><bb-fit><span data-fit>{title}</span></bb-fit></Heading>
    <FactList>
      {#each entries as fact (fact.term)}
        <Fact
          class="lane-fact"
          term={fact.term}
          title={fact.value}
          tone={fact.danger ? 'danger' : undefined}
          style={fact.wrap ? '--lane-fact-height: 2.5rem;' : undefined}
          truncate={!fact.wrap}
          fit
        >
          <span data-fit>{fact.value}</span>
        </Fact>
      {/each}
    </FactList>
  </Stack>
{/snippet}

{#snippet operation(action: {
  key: string;
  title: string;
  hint: string;
  label: string;
  available: boolean;
  danger?: boolean;
  onclick: () => void;
})}
  <Stack gap={2}>
    <div class="action-heading">
      <Heading level={3} variant="title"><bb-fit><span data-fit>{action.title}</span></bb-fit></Heading>
      <div class="action" class:available={action.available} inert={!action.available}>
        <Button
          block
          variant={action.danger ? 'primary' : 'secondary'}
          tone={action.danger ? 'danger' : 'neutral'}
          disabled={busy}
          aria-describedby={id + '-' + action.key}
          onclick={action.onclick}
        >
          <bb-fit><span data-fit>{action.label}</span></bb-fit>
        </Button>
      </div>
    </div>
    <Text id={id + '-' + action.key} class="hint long-hint" size="xs" tone="muted">
      <bb-fit mode="block"><span data-fit>{action.hint}</span></bb-fit>
    </Text>
  </Stack>
{/snippet}

<form class="editor" method="POST" action="?/alias" use:enhance={onSubmit}>
  <input type="hidden" name="stream" value={lane.stream} />
  <input type="hidden" name="consumer" value={lane.consumer} />

  <Scroller fill padding="var(--bb-space-4)" role="region" aria-label={lane.display} tabindex={0}>
    <Stack class="editor-content" gap={5}>
      {@render facts(t('admin.lanes.identityTitle'), identity)}

      <Stack gap={2}>
        {@render facts(t('admin.lanes.telemetryTitle'), metrics)}
        <Text class="hint long-hint" size="xs" tone="muted">
          <bb-fit mode="block"><span data-fit>{t('admin.lanes.capacityHint')}</span></bb-fit>
        </Text>
      </Stack>

      <Stack gap={4}>
        <Heading level={2} variant="title">
          <bb-fit><span data-fit>{t('admin.lanes.configurationTitle')}</span></bb-fit>
        </Heading>
        <Stack gap={2}>
          <Field label={t('admin.lanes.fieldAlias')} for={id + '-alias'} fit style="--field-mb: 0">
            <Input
              id={id + '-alias'}
              aria-describedby={id + '-alias-hint'}
              name="alias"
              fill
              mono
              maxlength={ALIAS_MAX}
              disabled={!canMutate}
              readonly={busy}
              placeholder={lane.defaultDisplay ?? lane.consumer}
              bind:value={() => draft.alias, (value) => (draft.alias = String(value ?? ''))}
            />
          </Field>
          <Text id={id + '-alias-hint'} class="hint" size="xs" tone="muted">
            <bb-fit mode="block"><span data-fit>{t('admin.lanes.aliasHint')}</span></bb-fit>
          </Text>
        </Stack>

        {#if canMutate}
          <div class="action-panels">
            <div class="action-panel" class:available={!lane.orphan} inert={lane.orphan}>
              {@render operation({
                key: 'durable-hint',
                title: t('admin.lanes.durableLabel'),
                hint: t(lane.ephemeral ? 'admin.lanes.durableHint' : 'admin.lanes.durableAlready'),
                label: t('admin.lanes.makePermanent'),
                available: lane.ephemeral,
                onclick: onDurable
              })}
            </div>
            <div class="action-panel" class:available={lane.orphan} inert={!lane.orphan}>
              {@render operation({
                key: 'delete-hint',
                title: t('admin.lanes.dangerTitle'),
                hint: t('admin.lanes.deleteHint'),
                label: t('common.delete'),
                available: true,
                danger: true,
                onclick: onDelete
              })}
            </div>
          </div>
        {/if}
      </Stack>
    </Stack>
  </Scroller>

  {#if canMutate}
    <EditorFooter
      {status}
      {dirty}
      canSave={canSave && !busy}
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
    flex: 1;
    flex-direction: column;
    min-block-size: 0;
    container: lane-editor / inline-size;
  }

  .editor :global(.editor-content) {
    max-inline-size: 36rem;
    margin-inline: auto;
  }

  .editor :global(.lane-fact) {
    display: grid;
    grid-template-columns: minmax(0, 46%) minmax(0, 1fr);
    grid-template-rows: minmax(0, 1fr);
    align-items: stretch;
    block-size: var(--lane-fact-height, 1.25rem);
    overflow: hidden;
  }

  .editor :global(.hint) {
    block-size: 2.25rem;
  }

  .editor :global(.long-hint) {
    block-size: 3.3rem;
  }

  .action-panels, .action-heading {
    display: grid;
  }

  .action-heading {
    grid-template-columns: minmax(0, 1fr) 8.5rem;
    gap: var(--bb-space-2);
    align-items: center;
  }

  .action-panel {
    grid-area: 1 / 1;
    min-inline-size: 0;
  }

  .action, .action-panel {
    opacity: 0;
    transition: opacity var(--bb-dur-fast) ease;
  }

  .available {
    opacity: 1;
  }

  @container lane-editor (width < 21rem) {
    .action-heading {
      grid-template-columns: minmax(0, 1fr);
    }

    .action {
      justify-self: end;
      inline-size: 8.5rem;
    }
  }

  @media (650px <= width < 1080px) {
    .editor :global(.long-hint) {
      block-size: 2.25rem;
    }
  }
</style>
