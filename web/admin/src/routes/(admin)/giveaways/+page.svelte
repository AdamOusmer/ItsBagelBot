<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import StatTile from '@bagel/ui/svelte/StatTile.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import StatePill from '#lib/components/StatePill.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { actionPayload } from '@bagel/kit';
  import { fmtDateTime } from '@bagel/kit/format';
  import { giveawayDrawState, giveawaySummary, MAX_PRIZE_MONTHS } from '@bagel/kit/giveaway';
  import { giveawayExclusionLabel } from '#lib/giveaway-workflow';
  import type { GiveawayPreviewWire, GiveawayWire } from '#lib/server/giveaways';

  let { data, form } = $props();
  const errorMessage = $derived((form as { error?: string } | null)?.error);
  const { t } = getI18n();
  let title = $state('');
  let reason = $state('');
  let winnerCount = $state(1);
  let prizeMonths = $state(1);
  let preview = $state<GiveawayPreviewWire | null>(null);
  let createdId = $state<string | null>(null);
  let actionMessage = $state<string | null>(null);
  let actionFailed = $state(false);

  type GiveawayActionPayload = {
    preview?: GiveawayPreviewWire;
    giveaway?: { id?: string };
    action?: { ok?: boolean; notice?: string };
    notice?: string;
    error?: string;
  };

  const submit: SubmitFunction = () => async ({ result, update }) => {
    const payload = actionPayload<GiveawayActionPayload>(result);
    if (payload?.preview) preview = payload.preview;
    if (payload?.giveaway?.id) createdId = payload.giveaway.id;
    actionMessage = payload?.action?.notice ?? payload?.notice ?? payload?.error ?? null;
    actionFailed = result.type === 'failure' || payload?.action?.ok === false || Boolean(payload?.error);
    await update({ reset: false });
  };

  const summary = $derived(giveawaySummary(winnerCount, prizeMonths));
  const winnerCountOverPool = $derived(preview !== null && winnerCount > preview.eligible.eligible);
  const drawState = $derived(giveawayDrawState(preview?.capabilities));
  const showProtectionWarning = $derived(Boolean(preview?.durationProtectionWarnings?.length) || drawState.blocked || drawState.pending);
  const exclusions = $derived(Object.entries(preview?.exclusions ?? {}));
  const messageFailed = $derived(actionFailed || Boolean(errorMessage));

  function date(value?: string | null): string {
    return fmtDateTime(value, t('admin.giveaways.noDate'));
  }

  function statusLabel(status: GiveawayWire['status']): string {
    return t(`admin.giveaways.status${status[0].toUpperCase()}${status.slice(1)}`);
  }

  const STATUS_TONE: Partial<Record<GiveawayWire['status'], 'success' | 'warning'>> = {
    complete: 'success',
    drawn: 'warning'
  };
</script>

<section class="screen active">
  <PageHead eyebrow={t('admin.giveaways.eyebrow')} description={t('admin.giveaways.description')}>
    {t('admin.giveaways.titlePre')}<em>{t('admin.giveaways.titleEm')}</em>
  </PageHead>

  {#await data.history then { degraded }}
    {#if degraded}<AlertBanner>{t('admin.giveaways.degraded')}</AlertBanner>{/if}
  {/await}

  <Grid cols={2} gap={4} stackAt="md" style="margin-block-start: var(--bb-space-5)">
    <Card as="article">
      <Stack gap={4}>
        <Stack gap={2}>
          <Heading level={5} as="h2">{t('admin.giveaways.newTitle')}</Heading>
          <Text size="sm" tone="muted">{t('admin.giveaways.oneEntry')}</Text>
        </Stack>
        <form method="POST" action="?/preview" use:enhance={submit} style="--field-mb: 0">
          <Stack gap={3}>
            <Field label={t('admin.giveaways.fieldTitle')}>
              <Input name="title" bind:value={title} required placeholder={t('admin.giveaways.fieldTitlePlaceholder')} />
            </Field>
            <Field label={t('admin.giveaways.fieldReason')}>
              <Input name="reason" bind:value={reason} required placeholder={t('admin.giveaways.fieldReasonPlaceholder')} />
            </Field>
            <Grid cols={2} gap={3}>
              <Field label={t('admin.giveaways.fieldWinners')}>
                <Input type="number" name="winner_count" min={1} max={preview?.eligible.eligible ?? undefined} step={1} bind:value={winnerCount} required />
              </Field>
              <Field label={t('admin.giveaways.fieldMonths')}>
                <Input type="number" name="prize_months" min={1} max={MAX_PRIZE_MONTHS} step={1} bind:value={prizeMonths} required />
              </Field>
            </Grid>
            <Text size="sm" tone="muted">{t('admin.giveaways.monthsHint')}</Text>
            {#if preview}<Text size="sm" tone="muted">{t('admin.giveaways.winnerLimit', { eligible: preview.eligible.eligible })}</Text>{/if}
            {#if winnerCountOverPool}<Text size="sm" tone="danger" role="alert">{t('admin.giveaways.winnersExceedEligible', { eligible: preview?.eligible.eligible ?? 0 })}</Text>{/if}
            <output aria-live="polite">
              <Text as="span" size="sm" tone="pale">
                {#if summary.totalMonths === null}
                  {t('admin.giveaways.summaryOverflow')}
                {:else}
                  {t(summary.winners === 1 ? 'admin.giveaways.previewSummaryOne' : 'admin.giveaways.previewSummaryMany', { winners: summary.winners, months: summary.months, total: summary.totalMonths })}
                {/if}
              </Text>
            </output>
            <Cluster gap={4}>
              <Button type="submit" variant="secondary" formnovalidate>{t('admin.giveaways.preview')}</Button>
              <Button type="submit" formaction="?/create" variant="primary" disabled={winnerCountOverPool}>{t('admin.giveaways.create')}</Button>
              <Text as="span" size="sm" tone="muted">{t('admin.giveaways.notLive')}</Text>
            </Cluster>
          </Stack>
        </form>
        {#if actionMessage || errorMessage}<Text size="sm" tone={messageFailed ? 'danger' : 'default'} role="alert">{actionMessage ?? errorMessage}</Text>{/if}
        {#if createdId}<Text size="sm"><TextLink variant="inline" href={`/giveaways/${encodeURIComponent(createdId)}`}>{t('admin.giveaways.open')}</TextLink></Text>{/if}
      </Stack>
    </Card>

    <Card as="article">
      <Stack gap={4}>
        <Heading level={5} as="h2">{t('admin.giveaways.preview')}</Heading>
        {#if preview}
          <StatTile label={t('admin.giveaways.eligible')} value={String(preview.eligible.eligible)} />
          <FactList>
            <Fact term={t('admin.giveaways.free')}>{preview.eligible.free}</Fact>
            <Fact term={t('admin.giveaways.premium')}>{preview.eligible.premium}</Fact>
            <Fact term={t('admin.giveaways.subscribers')}>{preview.eligible.subscribers}</Fact>
            <Fact term={t('admin.giveaways.excluded')}>{preview.eligible.excluded}</Fact>
          </FactList>
          {#if exclusions.length}
            <Stack gap={2}>
              <Heading level={3} variant="eyebrow">{t('admin.giveaways.exclusionReasons')}</Heading>
              <FactList>
                {#each exclusions as [reason, count] (reason)}
                  <Fact term={giveawayExclusionLabel(reason, t)}>{count}</Fact>
                {/each}
              </FactList>
            </Stack>
          {/if}
          {#if showProtectionWarning}
            <AlertBanner tone="warning" role="status">{t('admin.giveaways.protectionWarning')}</AlertBanner>
          {/if}
        {:else}
          <Text size="sm" tone="muted">{t('admin.giveaways.notLive')}</Text>
        {/if}
      </Stack>
    </Card>
  </Grid>

  <PageToolbar>
    {#snippet leading()}<span>{t('admin.giveaways.history')}</span>{/snippet}
  </PageToolbar>
  <DeckList>
    {#await data.history}
      <SkeletonStack rows={3} height="60px" />
    {:then { giveaways }}
      {#if giveaways.length === 0}
        <EmptyState title={t('admin.giveaways.empty')} />
      {:else}
        <ul class="bb-list" aria-label={t('admin.giveaways.history')}>
          {#each giveaways as giveaway (giveaway.id)}
            <li>
              <ManagementRow
                href={`/giveaways/${encodeURIComponent(giveaway.id)}`}
                title={giveaway.title}
                meta={`${giveaway.winnerCount} × ${giveaway.prizeMonths} · ${date(giveaway.createdAt)}`}
              >
                {#snippet marks()}
                  <StatePill tone={STATUS_TONE[giveaway.status] ?? 'neutral'}>{statusLabel(giveaway.status)}</StatePill>
                  {#if giveaway.pendingAwards}<StatePill tone="warning">{giveaway.pendingAwards}</StatePill>{/if}
                {/snippet}
              </ManagementRow>
            </li>
          {/each}
        </ul>
      {/if}
    {/await}
  </DeckList>
</section>
