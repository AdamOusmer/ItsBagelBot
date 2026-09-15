<script lang="ts">
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
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
  function tone(value: string): 'positive' | 'warning' | 'danger' | 'neutral' {
    if (value === 'completed' || value === 'protected' || value === 'reconciled' || value === 'sent') return 'positive';
    if (value === 'needs_review' || value === 'pending' || value === 'uncertain' || value === 'missing') return 'warning';
    if (value === 'incident' || value === 'failed') return 'danger';
    return 'neutral';
  }

  function operationKey(kind: string): string {
    return `${giveaway?.id ?? 'giveaway'}:${kind}:${giveaway?.version ?? 0}`;
  }

  const drawState = $derived(giveawayDrawState(giveaway?.capabilities));
</script>

<section class="screen active">
    {#if giveaway}
    <PageHead eyebrow={t('admin.giveaways.detail')} description={giveaway.reason}>
      {giveaway.title}
    </PageHead>
    {#if degraded}<AlertBanner>{t('admin.giveaways.degraded')}</AlertBanner>{/if}
    {#if alertsDegraded}<AlertBanner>{t('admin.giveaways.alertsUnavailable')}</AlertBanner>{/if}
    {#if actionMessage || errorMessage}<AlertBanner variant={actionFailed || Boolean(errorMessage) ? 'danger' : 'warn'}>{actionMessage ?? errorMessage}</AlertBanner>{/if}

    <div class="facts">
      <div><span>{t('admin.giveaways.selection')}</span><strong>{t(giveaway.winnerCount === 1 ? 'admin.giveaways.prizePlanOne' : 'admin.giveaways.prizePlan', { winners: giveaway.winnerCount, months: giveaway.prizeMonths })}</strong></div>
      <div><span>{t('admin.giveaways.eligible')}</span><strong>{giveaway.eligible?.eligible ?? giveaway.eligibleCount ?? t('admin.giveaways.notAvailable')}</strong></div>
      {#if giveaway.selectionMethod === 'random_draw'}<div><span>{t('admin.giveaways.selectionMethod')}</span><strong>{t('admin.giveaways.randomDraw')}</strong></div>{/if}
      <div><span>{t('admin.giveaways.drawAlgorithm')}</span><strong>{giveaway.algorithmVersion ?? t('admin.giveaways.notAvailable')}</strong></div>
    </div>

    {#if giveaway.status === 'draft' || giveaway.status === 'frozen'}
      <div class="workflow">
        <form method="POST" action="?/preview" use:enhance={previewSubmit}><Button type="submit" variant="secondary">{t('admin.giveaways.preview')}</Button></form>
        {#if preview}
          <span>{preview.eligible} {t('admin.giveaways.eligible')} · {preview.free} {t('admin.giveaways.free')} · {preview.premium} {t('admin.giveaways.premium')} · {preview.subscribers} {t('admin.giveaways.subscribers')} · {preview.excluded} {t('admin.giveaways.excluded')}</span>
          <form method="POST" action="?/freeze" use:enhance={mutationSubmit}><input type="hidden" name="pool_digest" value={preview.poolDigest ?? ''} /><input type="hidden" name="expected_version" value={giveaway.version} /><input type="hidden" name="idempotency_key" value={operationKey('freeze')} /><Button type="submit" variant="secondary" disabled={!preview.poolDigest}>{t('admin.giveaways.freeze')}</Button></form>
        {/if}
        {#if giveaway.status === 'frozen'}<div class="draw-warning">{drawState.blocked ? t('admin.giveaways.newAwardsDisabled') : drawState.pending ? t('admin.giveaways.capabilityWarning') : t('admin.giveaways.protectionWarning')}</div><form method="POST" action="?/draw" use:enhance={mutationSubmit}><input type="hidden" name="expected_version" value={giveaway.version} /><input type="hidden" name="idempotency_key" value={operationKey('draw')} /><Button type="submit" variant="primary" disabled={drawState.blocked}>{t('admin.giveaways.draw')}</Button></form>{/if}
      </div>
    {/if}

    {#if giveaway.alerts?.length}
      <section class="alerts">
        <h2>{t('admin.giveaways.alerts')}</h2>
        {#each giveaway.alerts as alert (alert.id)}
          <div class:urgent={alert.urgent} class="alert-row">
            <div><strong>{alert.awardId}</strong><span>{alert.reason}</span></div>
            <div><small>{t('admin.giveaways.nextCharge')}: {date(alert.upcomingChargeAt)}</small><small>{t('admin.giveaways.pending')}: {date(alert.pendingSince)}</small></div>
          </div>
        {/each}
      </section>
    {/if}

    <section class="winners">
      <h2>{t('admin.giveaways.winnerHistory')}</h2>
      <div class="table-wrap">
        <table>
          <thead><tr><th>{t('admin.giveaways.account')}</th><th>{t('admin.giveaways.award')}</th><th>{t('admin.giveaways.billing')}</th><th>{t('admin.giveaways.email')}</th><th>{t('admin.giveaways.selection')}</th><th></th></tr></thead>
          <tbody>
            {#each giveaway.winners as winner (winner.id)}
              <tr>
                <td><a href={`/users?q=${encodeURIComponent(winner.userId)}`}>@{winner.login}</a><small>{winner.userId}</small></td>
                <td><StatePill shape="tag" tone={tone(winner.awardState)}>{label(winner.awardState)}</StatePill><small>{t('admin.giveaways.start')}: {date(winner.startAt)}</small><small>{t('admin.giveaways.end')}: {date(winner.endAt)}</small></td>
                <td><StatePill shape="tag" tone={tone(winner.billingState)}>{winner.billingState === 'not_required' ? t('admin.giveaways.notRequired') : label(winner.billingState)}</StatePill><small>{t('admin.giveaways.nextCharge')}: {date(winner.nextChargeAt)}</small></td>
                <td><StatePill shape="tag" tone={tone(winner.emailState)}>{winner.emailState === 'missing_contact' ? t('admin.giveaways.missingEmail') : label(winner.emailState)}</StatePill>{#if winner.emailState === 'missing_contact'}<small>{t('admin.giveaways.missingEmail')}</small>{:else if winner.emailWarning}<small>{winner.emailWarning}</small>{/if}</td>
                <td><small>{date(winner.selectedAt)}</small><small>{t('admin.giveaways.providerVerified')}: {date(winner.providerVerifiedAt)}</small></td>
                <td>{#if winner.awardState === 'needs_review' || winner.billingState === 'pending' || winner.billingState === 'uncertain'}<form method="POST" action="?/retry" use:enhance={mutationSubmit}><input type="hidden" name="award_id" value={winner.id} /><Button type="submit" variant="secondary">{t('admin.giveaways.retry')}</Button></form>{/if}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </section>
  {:else if degraded}
    <AlertBanner>{t('admin.giveaways.unavailable')}</AlertBanner>
  {:else}
    <p>{t('common.loading')}</p>
  {/if}
</section>

<style>
  .facts { display:grid; grid-template-columns:repeat(4,1fr); gap:10px; margin:24px 0; } .facts div { padding:16px; border:1px solid var(--bb-border); border-radius:14px; background:var(--bb-surface); } .facts span,.facts strong { display:block; } .facts span { color:var(--bb-muted); font-size:12px; margin-bottom:7px; } .facts strong { font-size:15px; }
  .workflow { display:flex; align-items:center; flex-wrap:wrap; gap:12px; padding:14px 16px; border:1px solid var(--bb-border); border-radius:14px; background:var(--bb-surface); color:var(--bb-muted); font-size:13px; }
  .draw-warning { flex-basis:100%; color:#f2c879; font-size:12px; line-height:1.45; }
  h2 { margin:28px 0 12px; font-size:18px; } .alerts { max-width:980px; } .alert-row { display:flex; justify-content:space-between; gap:18px; padding:15px; border:1px solid var(--bb-border); border-radius:12px; margin-bottom:8px; } .alert-row.urgent { border-color:#b78144; } .alert-row span,.alert-row small,.alert-row strong { display:block; } .alert-row span,.alert-row small { color:var(--bb-muted); font-size:12px; margin-top:4px; }
  .table-wrap { overflow-x:auto; border:1px solid var(--bb-border); border-radius:14px; background:var(--bb-surface); } table { width:100%; min-width:980px; border-collapse:collapse; } th,td { padding:14px 12px; text-align:left; vertical-align:top; border-bottom:1px solid var(--bb-border); } th { color:var(--bb-muted); font-size:11px; text-transform:uppercase; letter-spacing:.08em; } td a { color:var(--bb-tan-pale); } td small { display:block; color:var(--bb-muted); font-size:11px; margin-top:5px; max-width:180px; }
  @media (max-width:760px) { .facts { grid-template-columns:1fr 1fr; } .alert-row { flex-direction:column; } }
</style>
