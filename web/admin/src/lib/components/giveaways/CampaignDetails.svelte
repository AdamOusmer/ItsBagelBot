<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { invalidate } from '$app/navigation';
  import { enhance, type SubmitFunction } from '$app/forms';
  import { actionPayload } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { giveawayDrawState } from '@bagel/kit/giveaway';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import {
    CAMPAIGN_STATUS, freezePoolDigest, giveawayNotice, giveawayPrize, type GiveawayActionPayload
  } from '#lib/giveaway-workflow';
  import type { GiveawayAlertWire, GiveawayDetailWire } from '#lib/server/giveaways';
  import CampaignAlerts from './CampaignAlerts.svelte';
  import ExcludedAccounts from './ExcludedAccounts.svelte';
  import WinnerHistory from './WinnerHistory.svelte';

  let { giveaway, alerts, result }: {
    giveaway: GiveawayDetailWire;
    alerts: Promise<GiveawayAlertWire[] | null>;
    result?: unknown;
  } = $props();

  const { locale, t } = getI18n();
  const number = new Intl.NumberFormat(locale);
  const poolBreakdown = ['free', 'premium', 'subscribers', 'excluded'] as const;

  let busy = $state<string>();
  let confirmDraw = $state(false);
  let drawForm = $state<HTMLFormElement>();
  let retainedPreview = $state.raw<GiveawayActionPayload['preview']>();
  const payload = $derived(result as GiveawayActionPayload | null);
  const notice = $derived(giveawayNotice(result));
  const draft = $derived(giveaway.status === 'draft');
  const frozen = $derived(giveaway.status === 'frozen');
  const preview = $derived(payload?.preview ?? retainedPreview);
  const reviewedPool = $derived(draft ? preview : undefined);
  const poolDigest = $derived(reviewedPool ? freezePoolDigest(reviewedPool) : null);
  const excluded = $derived(reviewedPool?.excluded ?? giveaway.excluded ?? []);
  const eligible = $derived(reviewedPool?.eligible.eligible ?? giveaway.eligible?.eligible ?? giveaway.eligibleCount);
  const status = $derived(CAMPAIGN_STATUS[giveaway.status]);
  const capabilities = $derived(giveawayDrawState(giveaway.capabilities));
  const capabilityWarning = $derived.by(() => {
    if (!frozen) return;
    if (capabilities.blocked) return t('admin.giveaways.newAwardsDisabled');
    if (capabilities.pending) return t('admin.giveaways.capabilityWarning');
    return t('admin.giveaways.protectionWarning');
  });

  $effect(() => {
    if (payload?.preview) retainedPreview = payload.preview;
  });

  const submit: SubmitFunction = ({ action, cancel }) => {
    if (busy) return cancel();
    const operation = action.search.slice(2);
    busy = operation;
    return async ({ result, update }) => {
      try {
        await update({ reset: false, refreshAll: false, navigate: false });
        if (actionPayload<GiveawayActionPayload>(result)?.action?.ok) await invalidate('admin:giveaway');
      } finally {
        busy = undefined;
      }
    };
  };

  function operationKey(kind: 'freeze' | 'draw'): string {
    return `${giveaway.id}:${kind}:${giveaway.version}`;
  }

  function requestDraw(event: MouseEvent) {
    event.preventDefault();
    confirmDraw = true;
  }

  function commitDraw() {
    confirmDraw = false;
    drawForm?.requestSubmit();
  }
</script>

<PageHead description={giveaway.reason}>
  <em>{giveaway.title}</em>
</PageHead>

<PageToolbar class="campaign-toolbar">
  {#snippet leading()}
    <Cluster gap={3}>
      <TextLink variant="quiet" href="/giveaways" icon="arrowLeft" touch>{t('admin.giveaways.back')}</TextLink>
      <span class="campaign-status">
        <Badge shape="status" tone={status.tone}><bb-fit><span data-fit>{t(status.label)}</span></bb-fit></Badge>
      </span>
    </Cluster>
  {/snippet}
  {#snippet trailing()}
    <div class="campaign-actions">
      <form class="preview-action" class:available={draft} inert={!draft} method="POST" action="?/preview" use:enhance={submit}>
        <Button type="submit" variant="secondary" block busy={busy === 'preview'} disabled={!draft || Boolean(busy)}>
          <bb-fit><span data-fit>{t('admin.giveaways.preview')}</span></bb-fit>
        </Button>
      </form>
      <form class="freeze-action" class:available={draft} inert={!draft} method="POST" action="?/freeze" use:enhance={submit}>
        <input type="hidden" name="pool_digest" value={poolDigest ?? ''} />
        <input type="hidden" name="expected_version" value={giveaway.version} />
        <input type="hidden" name="idempotency_key" value={operationKey('freeze')} />
        <Button type="submit" block busy={busy === 'freeze'} disabled={!poolDigest || Boolean(busy)}>
          <bb-fit><span data-fit>{t('admin.giveaways.freeze')}</span></bb-fit>
        </Button>
      </form>
      <form class="draw-action" class:available={frozen} inert={!frozen} method="POST" action="?/draw" bind:this={drawForm} use:enhance={submit}>
        <input type="hidden" name="expected_version" value={giveaway.version} />
        <input type="hidden" name="idempotency_key" value={operationKey('draw')} />
        <Button type="submit" block busy={busy === 'draw'} disabled={!frozen || capabilities.blocked || Boolean(busy)} onclick={requestDraw}>
          <bb-fit><span data-fit>{t('admin.giveaways.draw')}</span></bb-fit>
        </Button>
      </form>
    </div>
  {/snippet}
</PageToolbar>

<Stack gap={4}>
  <div class="operation-notice" aria-live="polite">
    {#if notice}
      <AlertBanner tone={notice.failed ? 'danger' : 'success'} role="note" flush>{notice.text}</AlertBanner>
    {:else if capabilityWarning}
      <AlertBanner tone="warning" role="note" flush>{capabilityWarning}</AlertBanner>
    {/if}
  </div>

  <Card as="section">
    <Stack gap={3}>
      <CardHead title={t('admin.giveaways.selection')} headingLevel={2} />
      <FactList layout="tiles">
        <Fact term={t('admin.giveaways.award')} fit>{giveawayPrize(giveaway, number, t)}</Fact>
        <Fact term={t('admin.giveaways.eligible')} fit>{eligible === undefined ? t('admin.giveaways.notAvailable') : number.format(eligible)}</Fact>
        <Fact term={t('admin.giveaways.selectionMethod')} fit>{t(giveaway.selectionMethod === 'random_draw' ? 'admin.giveaways.randomDraw' : 'admin.giveaways.notAvailable')}</Fact>
        <Fact term={t('admin.giveaways.drawAlgorithm')} fit mono>{giveaway.algorithmVersion ?? t('admin.giveaways.notAvailable')}</Fact>
      </FactList>
      {#if draft}
        <FactList layout="tiles">
          {#each poolBreakdown as category (category)}
            <Fact term={t(`admin.giveaways.${category}`)} fit>{reviewedPool ? number.format(reviewedPool.eligible[category]) : t('admin.giveaways.notAvailable')}</Fact>
          {/each}
          <Fact term={t('admin.giveaways.addedBack')} fit wide>{reviewedPool ? number.format(reviewedPool.included) : t('admin.giveaways.notAvailable')}</Fact>
        </FactList>
      {/if}
    </Stack>
  </Card>

  {#if draft || frozen || excluded.length}
    <ExcludedAccounts accounts={excluded} editable={draft} ready={!draft || Boolean(reviewedPool)} busy={Boolean(busy)} {submit} />
  {/if}

  {#if giveaway.winners.length}
    <CampaignAlerts {alerts} />
  {/if}

  <WinnerHistory winners={giveaway.winners} busy={Boolean(busy)} {submit} />
</Stack>

<ConfirmDialog bind:open={confirmDraw} title={t('admin.giveaways.draw')} body={t('admin.giveaways.drawConfirm')} confirmLabel={t('admin.giveaways.draw')} onConfirm={commitDraw} />

<style>
  :global(.campaign-toolbar) {
    flex-wrap: wrap;
  }

  .campaign-status {
    display: inline-grid;
    inline-size: 6.5rem;
  }

  .campaign-actions {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--bb-space-2);
    inline-size: min(100%, 26rem);
    --btn-pad: 0 10px;
  }

  .preview-action,
  .freeze-action,
  .draw-action {
    grid-row: 1;
    min-inline-size: 0;
    opacity: 0;
    transition: opacity var(--bb-dur-fast);
  }

  .preview-action {
    grid-column: 1;
  }

  .freeze-action,
  .draw-action {
    grid-column: 2;
  }

  .available {
    opacity: 1;
  }

  .operation-notice {
    block-size: 3rem;
    overflow: auto;
  }

  @media (max-width: 600px) {
    .operation-notice {
      block-size: 4.5rem;
    }
  }
</style>
