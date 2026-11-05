<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { invalidateAll } from '$app/navigation';
  import { getI18n } from '@bagel/kit';
  import type { RunRecord } from '@bagel/kit/automations';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import RunsPanel from '#lib/components/automations/RunsPanel.svelte';
  import FlowShareTools from '#lib/components/flow/FlowShareTools.svelte';
  import FlowThumb from '#lib/components/flow/FlowThumb.svelte';
  import FlowsIntro from '#lib/components/flow/FlowsIntro.svelte';
  import OverlayLinkControl from '#lib/components/flows-overlay/OverlayLinkControl.svelte';
  import PremiumGate from '#lib/components/shared/PremiumGate.svelte';
  import RowDeleteButton from '#lib/components/shared/RowDeleteButton.svelte';
  import { postAction, reseedOnChange } from '#lib/client-actions.svelte';
  import type { FlowCard } from '#lib/flow/cards';
  import { deleteMessage, toggleMessage, warningMessages } from '#lib/flow/messages';
  import type { DeleteReply, ToggleReply } from '#lib/flow/replies';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();

  const { t } = getI18n();

  let introDismissed = $state(false);
  const showIntro = $derived(!data.introSeen && !introDismissed);

  let changes = $state<Record<string, { enabled: boolean; revision: string }>>({});
  let gone = $state<string[]>([]);
  let pending = $state<FlowCard | null>(null);
  let deleting = $state(false);

  reseedOnChange(
    () => data.flows,
    () => {
      changes = {};
      gone = [];
    }
  );

  const cards = $derived(
    data.flows.filter((flow) => !gone.includes(flow.id)).map((flow) => ({ ...flow, ...changes[flow.id] }))
  );

  let query = $state('');
  const needle = $derived(query.trim().toLowerCase());
  const shown = $derived(cards.filter((f) => !needle || `${f.name} ${f.description} ${f.when}`.toLowerCase().includes(needle)));

  const stepCount = (count: number) => (count === 1 ? t('flows.page.stepOne') : t('flows.page.stepMany', { count }));
  const facts = (flow: FlowCard) => [flow.when, stepCount(flow.steps)].filter(Boolean).join(', ');

  let runs = $state<RunRecord[]>([]);
  let runsBusy = $state(false);
  let runsOpen = $state(false);

  async function loadRuns() {
    runsBusy = true;
    const reply = await postAction<{ ok: boolean; runs?: RunRecord[] }>('runs', new FormData());
    runsBusy = false;
    if (reply?.ok) runs = reply.runs ?? [];
    else toast('danger', t('flows.recipe.runsFailed'));
  }

  function openRuns() {
    runsOpen = !runsOpen;
    if (runsOpen) void loadRuns();
  }

  async function toggle(flow: FlowCard, next: boolean) {
    const before = { enabled: flow.enabled, revision: flow.revision };
    changes[flow.id] = { enabled: next, revision: flow.revision };
    const body = new FormData();
    body.set('id', flow.id);
    body.set('enabled', next ? '1' : '0');
    body.set('revision', flow.revision);
    const reply = await postAction<ToggleReply>('toggle', body);
    if (reply?.ok) {
      changes[flow.id] = { enabled: reply.enabled, revision: reply.revision };
      for (const text of warningMessages(t, reply.warnings)) toast('info', text, { ttlMs: 7000 });
      return;
    }
    changes[flow.id] = before;
    toast('danger', reply ? toggleMessage(t, reply) : t('flows.toggleFailed'));
    if (reply && !reply.ok && reply.reason !== 'failed') await invalidateAll();
  }

  async function remove() {
    const flow = pending;
    if (!flow || deleting) return;
    deleting = true;
    const body = new FormData();
    body.set('id', flow.id);
    const reply = await postAction<DeleteReply>('remove', body);
    deleting = false;
    pending = null;
    if (reply?.ok) {
      gone = [...gone, flow.id];
      toast('success', t('flows.deleted', { name: flow.name }));
      return;
    }
    toast('danger', reply ? deleteMessage(t, reply) : t('flows.deleteFailed'));
    if (reply && !reply.ok && reply.reason === 'not_found') await invalidateAll();
  }
</script>

<svelte:head><title>{data.navLabels['nav.flows']} · ItsBagelBot</title></svelte:head>

{#snippet betaChip()}<Chip as="span" pressed>{t('modules.betaChip')}</Chip>{/snippet}

{#if data.locked}
  <PremiumGate />
{:else if showIntro}
  <FlowsIntro name={data.navLabels['nav.flows']} ondismiss={() => (introDismissed = true)} />
{:else}
<div class="landing" data-sveltekit-preload-code="eager">
  <PageHead
    eyebrow={t('flows.page.eyebrow', { name: data.navLabels['nav.flows'] })}
    description={t('flows.page.description')}
  >
    <span class="title">{data.navLabels['nav.flows']} {@render betaChip()}</span>
    {#snippet trailing()}
      <FlowShareTools flows={data.flows.map(({ ref, name }) => ({ ref, name }))} onImported={() => void invalidateAll()} />
      <ButtonLink variant="primary" href="/flows/new"><Icon name="plus" size={16} /> {t('flows.page.new')}</ButtonLink>
    {/snippet}
  </PageHead>

  <div class="tools">
    <div class="search"><SearchInput bind:value={query} placeholder={t('flows.page.search')} fill /></div>
    <Button variant="ghost" aria-expanded={runsOpen} onclick={openRuns}>{t('flows.recipe.runsTitle')}</Button>
  </div>

  {#if runsOpen}
    <RunsPanel {runs} busy={runsBusy} showNames title={t('flows.recipe.runsTitle')} empty={t('flows.recipe.runsEmpty')} onRefresh={() => void loadRuns()} />
  {/if}

  <OverlayLinkControl />

  {#if shown.length > 0}
    <ul class="cards">
      {#each shown as flow (flow.id)}
        <li class="card" class:off={!flow.enabled}>
          <a class="open" href="/flows/{flow.id}">
            <span class="diagram"><FlowThumb preview={flow.preview} /></span>
            <span class="name">{flow.name}</span>
            {#if flow.description}<span class="description">{flow.description}</span>{/if}
          </a>
          <div class="meta">
            <span class="facts">{facts(flow)}</span>
            <span class="controls">
              {#if flow.deletable}
                <RowDeleteButton label={t('flows.deleteLabel', { name: flow.name })} onclick={() => (pending = flow)} />
              {/if}
              <Switch
                checked={flow.enabled}
                label={flow.enabled ? t('flows.page.turnOff', { name: flow.name }) : t('flows.page.turnOn', { name: flow.name })}
                onCheckedChange={(next) => void toggle(flow, next)}
              />
            </span>
          </div>
        </li>
      {/each}
    </ul>
  {:else if needle}
    <EmptyState title={t('flows.page.emptyTitle')} body={t('flows.page.emptyBody')} />
  {:else}
    <EmptyState title={t('flows.page.noneTitle')} body={t('flows.page.noneBody')} />
  {/if}
</div>

<ConfirmDialog
  open={pending !== null}
  title={t('flows.deleteTitle', { name: pending?.name ?? '' })}
  body={t('flows.deleteBody')}
  confirmLabel={t('flows.deleteConfirm')}
  tone="danger"
  busy={deleting}
  onConfirm={remove}
  onCancel={() => (pending = null)}
/>
{/if}

<style>
  .landing {
    display: flex;
    flex-direction: column;
    gap: 32px;
    min-width: 0;
  }
  .title {
    display: inline-flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 12px;
  }
  .tools {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  .search {
    flex: 1 1 280px;
    max-width: 420px;
  }
  .cards {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(300px, 100%), 1fr));
    gap: 16px;
  }
  .card {
    display: flex;
    flex-direction: column;
    gap: 12px;
    height: 100%;
    padding: 16px;
    border-radius: var(--bb-radius-md);
    background: linear-gradient(rgba(var(--bb-white-rgb), 0.05), rgba(var(--bb-white-rgb), 0.02)), var(--bb-card-bg);
    box-shadow: var(--bb-depth-1);
    transition: box-shadow var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .card:hover {
    box-shadow: var(--bb-depth-3);
  }
  .open {
    display: flex;
    flex-direction: column;
    gap: 2px;
    flex: 1;
    border-radius: var(--bb-radius-sm);
    color: inherit;
    text-decoration: none;
  }
  .open:focus-visible {
    outline: 2px solid var(--bb-green-glow);
    outline-offset: 4px;
  }
  .diagram {
    display: block;
    margin-bottom: 10px;
    transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .card.off .diagram {
    opacity: 0.4;
  }
  .name {
    font-family: var(--bb-font-body);
    font-size: 16px;
    font-weight: 600;
    color: var(--bb-white);
  }
  .description {
    font-size: 14px;
    line-height: 1.4;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .meta {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    font-size: 14px;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .facts {
    min-width: 0;
  }
  .controls {
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }
  @media (prefers-reduced-motion: reduce) {
    .card,
    .diagram {
      transition: none;
    }
  }
</style>
