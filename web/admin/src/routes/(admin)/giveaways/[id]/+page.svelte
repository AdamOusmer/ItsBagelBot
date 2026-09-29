<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Table from '@bagel/ui/svelte/Table.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import StatePill from '$lib/components/StatePill.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { actionPayload } from '@bagel/kit';
  import { fmtDateTime } from '@bagel/kit/format';
  import { giveawayDrawState } from '@bagel/kit/giveaway';
  import { freezePoolDigest } from '$lib/giveaway-workflow';
  import type { GiveawayDetailWire, GiveawayWinnerWire } from '$lib/server/giveaways';

  let { data, form } = $props();
  const errorMessage = $derived((form as { error?: string } | null)?.error);
  const { t } = getI18n();
  let giveaway = $state<GiveawayDetailWire | null>(null);
  let degraded = $state(false);
  let alertsDegraded = $state(false);
  let preview = $state<{ eligible: number; free: number; premium: number; subscribers: number; excluded: number; poolDigest: string | null } | null>(null);
  let actionMessage = $state<string | null>(null);
  let actionFailed = $state(false);

  $effect(() => {
    let alive = true;
    data.detail.then((bundle) => { if (alive) { giveaway = bundle.giveaway; degraded = bundle.degraded; alertsDegraded = bundle.alertsDegraded; } });
    return () => { alive = false; };
  });

  type GiveawayActionPayload = {
    preview?: { poolDigest?: string; eligible?: { eligible?: number; free?: number; premium?: number; subscribers?: number; excluded?: number } };
    action?: { ok?: boolean; notice?: string };
    notice?: string;
    error?: string;
  };

  const previewSubmit: SubmitFunction = () => async ({ result, update }) => {
    const payload = actionPayload<GiveawayActionPayload>(result);
    if (payload?.preview?.eligible) {
      preview = {
        eligible: payload.preview.eligible.eligible ?? 0,
        free: payload.preview.eligible.free ?? 0,
        premium: payload.preview.eligible.premium ?? 0,
        subscribers: payload.preview.eligible.subscribers ?? 0,
        excluded: payload.preview.eligible.excluded ?? 0,
        poolDigest: freezePoolDigest(payload.preview)
      };
    }
    actionMessage = payload?.action?.notice ?? payload?.notice ?? payload?.error ?? null;
    actionFailed = result.type === 'failure' || payload?.action?.ok === false || Boolean(payload?.error);
    await update({ reset: false });
  };

  const mutationSubmit: SubmitFunction = () => async ({ result, update }) => {
    const payload = actionPayload<GiveawayActionPayload>(result);
    actionMessage = payload?.action?.notice ?? payload?.notice ?? payload?.error ?? null;
    actionFailed = result.type === 'failure' || payload?.action?.ok === false || Boolean(payload?.error);
    await update({ reset: false });
  };

  function date(value?: string | null): string {
    return fmtDateTime(value, t('admin.giveaways.noDate'));
  }
  function label(value: string): string {
    return value.split('_').map((part) => part[0].toUpperCase() + part.slice(1)).join(' ');
  }

  const POSITIVE = new Set(['completed', 'protected', 'reconciled', 'sent']);
  const WARNING = new Set(['needs_review', 'pending', 'uncertain', 'missing']);
  const DANGER = new Set(['incident', 'failed']);

  function tone(value: string): 'positive' | 'warning' | 'danger' | 'neutral' {
    if (POSITIVE.has(value)) return 'positive';
    if (WARNING.has(value)) return 'warning';
    if (DANGER.has(value)) return 'danger';
    return 'neutral';
  }

  const RETRY_BILLING = new Set(['pending', 'uncertain']);

  function needsRetry(winner: GiveawayWinnerWire): boolean {
    return winner.awardState === 'needs_review' || RETRY_BILLING.has(winner.billingState);
  }

  function operationKey(kind: string): string {
    return `${giveaway?.id ?? 'giveaway'}:${kind}:${giveaway?.version ?? 0}`;
  }

  const drawState = $derived(giveawayDrawState(giveaway?.capabilities));
  const drawWarning = $derived.by(() => {
    if (drawState.blocked) return t('admin.giveaways.newAwardsDisabled');
    if (drawState.pending) return t('admin.giveaways.capabilityWarning');
    return t('admin.giveaways.protectionWarning');
  });
  const editable = $derived(giveaway?.status === 'draft' || giveaway?.status === 'frozen');
</script>

<section class="screen active giveaway">
  {#if giveaway}
    <PageHead eyebrow={t('admin.giveaways.detail')} description={giveaway.reason}>
      {giveaway.title}
    </PageHead>
    {#if degraded}<AlertBanner>{t('admin.giveaways.degraded')}</AlertBanner>{/if}
    {#if alertsDegraded}<AlertBanner>{t('admin.giveaways.alertsUnavailable')}</AlertBanner>{/if}
    {#if actionMessage || errorMessage}<AlertBanner variant={actionFailed || Boolean(errorMessage) ? 'danger' : 'warn'}>{actionMessage ?? errorMessage}</AlertBanner>{/if}

    <Stack gap={5}>
      <FactList layout="tiles">
        <Fact term={t('admin.giveaways.selection')}>{t(giveaway.winnerCount === 1 ? 'admin.giveaways.prizePlanOne' : 'admin.giveaways.prizePlan', { winners: giveaway.winnerCount, months: giveaway.prizeMonths })}</Fact>
        <Fact term={t('admin.giveaways.eligible')}>{giveaway.eligible?.eligible ?? giveaway.eligibleCount ?? t('admin.giveaways.notAvailable')}</Fact>
        {#if giveaway.selectionMethod === 'random_draw'}<Fact term={t('admin.giveaways.selectionMethod')}>{t('admin.giveaways.randomDraw')}</Fact>{/if}
        <Fact term={t('admin.giveaways.drawAlgorithm')}>{giveaway.algorithmVersion ?? t('admin.giveaways.notAvailable')}</Fact>
      </FactList>

      {#if editable}
        <Card>
          <Stack gap={3}>
            <Cluster gap={3}>
              <form method="POST" action="?/preview" use:enhance={previewSubmit}><Button type="submit" variant="secondary">{t('admin.giveaways.preview')}</Button></form>
              {#if preview}
                <Text as="span" size="sm" tone="muted">{preview.eligible} {t('admin.giveaways.eligible')} · {preview.free} {t('admin.giveaways.free')} · {preview.premium} {t('admin.giveaways.premium')} · {preview.subscribers} {t('admin.giveaways.subscribers')} · {preview.excluded} {t('admin.giveaways.excluded')}</Text>
                <form method="POST" action="?/freeze" use:enhance={mutationSubmit}><input type="hidden" name="pool_digest" value={preview.poolDigest ?? ''} /><input type="hidden" name="expected_version" value={giveaway.version} /><input type="hidden" name="idempotency_key" value={operationKey('freeze')} /><Button type="submit" variant="secondary" disabled={!preview.poolDigest}>{t('admin.giveaways.freeze')}</Button></form>
              {/if}
            </Cluster>
            {#if giveaway.status === 'frozen'}
              <Text size="xs" tone="warn" role="status">{drawWarning}</Text>
              <form method="POST" action="?/draw" use:enhance={mutationSubmit}><input type="hidden" name="expected_version" value={giveaway.version} /><input type="hidden" name="idempotency_key" value={operationKey('draw')} /><Button type="submit" variant="primary" disabled={drawState.blocked}>{t('admin.giveaways.draw')}</Button></form>
            {/if}
          </Stack>
        </Card>
      {/if}

      {#if giveaway.alerts?.length}
        <section class="alerts">
          <Stack gap={3}>
            <Heading level={5} as="h2">{t('admin.giveaways.alerts')}</Heading>
            <Stack gap={2}>
              {#each giveaway.alerts as alert (alert.id)}
                <Card tone={alert.urgent ? 'accent' : undefined}>
                  <Cluster justify="between" gap={4}>
                    <Stack gap={1}>
                      <Heading level={6} as="h3">{alert.awardId}</Heading>
                      <Text as="span" size="xs" tone="muted">{alert.reason}</Text>
                    </Stack>
                    <Stack gap={1}>
                      <Text as="small" size="xs" tone="muted">{t('admin.giveaways.nextCharge')}: {date(alert.upcomingChargeAt)}</Text>
                      <Text as="small" size="xs" tone="muted">{t('admin.giveaways.pending')}: {date(alert.pendingSince)}</Text>
                    </Stack>
                  </Cluster>
                </Card>
              {/each}
            </Stack>
          </Stack>
        </section>
      {/if}

      <section>
        <Stack gap={3}>
          <Heading level={5} as="h2">{t('admin.giveaways.winnerHistory')}</Heading>
          <Card flush>
            <Table label={t('admin.giveaways.winnerHistory')} minWidth="980px">
              <thead><tr><th scope="col">{t('admin.giveaways.account')}</th><th scope="col">{t('admin.giveaways.award')}</th><th scope="col">{t('admin.giveaways.billing')}</th><th scope="col">{t('admin.giveaways.email')}</th><th scope="col">{t('admin.giveaways.selection')}</th><th scope="col"></th></tr></thead>
              <tbody>
                {#each giveaway.winners as winner (winner.id)}
                  <tr>
                    <td><Stack gap={1} align="start"><TextLink variant="inline" href={`/users?q=${encodeURIComponent(winner.userId)}`}>@{winner.login}</TextLink><Text as="small" size="xs" tone="muted">{winner.userId}</Text></Stack></td>
                    <td><Stack gap={1} align="start"><StatePill tone={tone(winner.awardState)}>{label(winner.awardState)}</StatePill><Text as="small" size="xs" tone="muted">{t('admin.giveaways.start')}: {date(winner.startAt)}</Text><Text as="small" size="xs" tone="muted">{t('admin.giveaways.end')}: {date(winner.endAt)}</Text></Stack></td>
                    <td><Stack gap={1} align="start"><StatePill tone={tone(winner.billingState)}>{winner.billingState === 'not_required' ? t('admin.giveaways.notRequired') : label(winner.billingState)}</StatePill><Text as="small" size="xs" tone="muted">{t('admin.giveaways.nextCharge')}: {date(winner.nextChargeAt)}</Text></Stack></td>
                    <td><Stack gap={1} align="start"><StatePill tone={tone(winner.emailState)}>{winner.emailState === 'missing_contact' ? t('admin.giveaways.missingEmail') : label(winner.emailState)}</StatePill>{#if winner.emailState === 'missing_contact'}<Text as="small" size="xs" tone="muted">{t('admin.giveaways.missingEmail')}</Text>{:else if winner.emailWarning}<Text as="small" size="xs" tone="muted">{winner.emailWarning}</Text>{/if}</Stack></td>
                    <td><Stack gap={1} align="start"><Text as="small" size="xs" tone="muted">{date(winner.selectedAt)}</Text><Text as="small" size="xs" tone="muted">{t('admin.giveaways.providerVerified')}: {date(winner.providerVerifiedAt)}</Text></Stack></td>
                    <td>{#if needsRetry(winner)}<form method="POST" action="?/retry" use:enhance={mutationSubmit}><input type="hidden" name="award_id" value={winner.id} /><Button type="submit" variant="secondary">{t('admin.giveaways.retry')}</Button></form>{/if}</td>
                  </tr>
                {/each}
              </tbody>
            </Table>
          </Card>
        </Stack>
      </section>
    </Stack>
  {:else if degraded}
    <AlertBanner>{t('admin.giveaways.unavailable')}</AlertBanner>
  {:else}
    <SkeletonStack rows={3} height="96px" />
  {/if}
</section>

<style>
  .giveaway {
    --card-pad: var(--bb-space-4);
  }
  .alerts {
    max-width: 980px;
  }
</style>
