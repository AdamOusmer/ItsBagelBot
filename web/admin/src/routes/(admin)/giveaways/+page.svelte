<script lang="ts">
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import StatePill from '$lib/components/StatePill.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { actionPayload } from '@bagel/kit';
  import { fmtDateTime } from '@bagel/kit/format';
  import { giveawaySummary, MAX_PRIZE_MONTHS } from '@bagel/kit/giveaway';
  import type { GiveawayPreviewWire, GiveawayWire } from '$lib/server/giveaways';

  let { data, form } = $props();
  const errorMessage = $derived((form as { error?: string } | null)?.error);
  const { t } = getI18n();
  let history = $state<GiveawayWire[]>([]);
  let degraded = $state(false);
  let title = $state('');
  let reason = $state('');
  let winnerCount = $state(1);
  let prizeMonths = $state(1);
  let preview = $state<GiveawayPreviewWire | null>(null);
  let createdId = $state<string | null>(null);
  let actionMessage = $state<string | null>(null);
  let actionFailed = $state(false);

  $effect(() => {
    let alive = true;
    data.history.then((bundle) => {
      if (!alive) return;
      history = bundle.giveaways;
      degraded = bundle.degraded;
    });
    return () => { alive = false; };
  });

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
  const previewPending = $derived(preview?.capabilities ? !preview.capabilities.newAwardsEnabled || !preview.capabilities.schedulingEnabled || !preview.capabilities.providerMutationsEnabled || !preview.capabilities.intervalRuleVerified : false);

  function date(value?: string | null): string {
    return fmtDateTime(value, t('admin.giveaways.noDate'));
  }

  function statusLabel(status: GiveawayWire['status']): string {
    return t(`admin.giveaways.status${status[0].toUpperCase()}${status.slice(1)}`);
  }
</script>

<section class="screen active">
  <PageHead eyebrow={t('admin.giveaways.eyebrow')} description={t('admin.giveaways.description')}>
    {t('admin.giveaways.titlePre')}<em>{t('admin.giveaways.titleEm')}</em>
  </PageHead>

  {#if degraded}<AlertBanner>{t('admin.giveaways.degraded')}</AlertBanner>{/if}

  <div class="giveaway-grid">
    <article class="panel form-panel">
      <h2>{t('admin.giveaways.newTitle')}</h2>
      <p class="hint">{t('admin.giveaways.oneEntry')}</p>
      <form method="POST" action="?/preview" use:enhance={submit}>
        <Field label={t('admin.giveaways.fieldTitle')}>
          <Input name="title" bind:value={title} required placeholder={t('admin.giveaways.fieldTitlePlaceholder')} />
        </Field>
        <Field label={t('admin.giveaways.fieldReason')}>
          <Input name="reason" bind:value={reason} required placeholder={t('admin.giveaways.fieldReasonPlaceholder')} />
        </Field>
        <div class="two-col">
          <Field label={t('admin.giveaways.fieldWinners')}>
            <Input type="number" name="winner_count" min="1" max={preview?.eligible.eligible ?? undefined} step="1" bind:value={winnerCount} required />
          </Field>
          <Field label={t('admin.giveaways.fieldMonths')}>
            <Input type="number" name="prize_months" min="1" max={MAX_PRIZE_MONTHS} step="1" bind:value={prizeMonths} required />
          </Field>
        </div>
        <p class="hint">{t('admin.giveaways.monthsHint')}</p>
        {#if preview}<p class="hint">{t('admin.giveaways.winnerLimit', { eligible: preview.eligible.eligible })}</p>{/if}
        {#if winnerCountOverPool}<p class="error" role="alert">{t('admin.giveaways.winnersExceedEligible', { eligible: preview?.eligible.eligible ?? 0 })}</p>{/if}
        <output class="summary" aria-live="polite">
          {#if summary.totalMonths === null}
            {t('admin.giveaways.summaryOverflow')}
          {:else}
            {t(summary.winners === 1 ? 'admin.giveaways.previewSummaryOne' : 'admin.giveaways.previewSummaryMany', { winners: summary.winners, months: summary.months, total: summary.totalMonths })}
          {/if}
        </output>
        <div class="actions">
          <Button type="submit" variant="secondary" formnovalidate>{t('admin.giveaways.preview')}</Button>
          <Button type="submit" formaction="?/create" variant="primary" disabled={winnerCountOverPool}>{t('admin.giveaways.create')}</Button>
          <p class="hint">{t('admin.giveaways.notLive')}</p>
        </div>
      </form>
      {#if actionMessage || errorMessage}<p class:error={actionFailed || Boolean(errorMessage)} role="alert">{actionMessage ?? errorMessage}</p>{/if}
      {#if createdId}<a class="created" href={`/giveaways/${encodeURIComponent(createdId)}`}>{t('admin.giveaways.open')}</a>{/if}
    </article>

    <article class="panel preview-panel">
      <h2>{t('admin.giveaways.preview')}</h2>
      {#if preview}
        <div class="stats">
          <strong>{preview.eligible.eligible}</strong><span>{t('admin.giveaways.eligible')}</span>
        </div>
        <div class="breakdown">
          <span>{t('admin.giveaways.free')} <b>{preview.eligible.free}</b></span>
          <span>{t('admin.giveaways.premium')} <b>{preview.eligible.premium}</b></span>
          <span>{t('admin.giveaways.subscribers')} <b>{preview.eligible.subscribers}</b></span>
          <span>{t('admin.giveaways.excluded')} <b>{preview.eligible.excluded}</b></span>
        </div>
        {#if Object.entries(preview.exclusions).length}
          <h3>{t('admin.giveaways.exclusionReasons')}</h3>
          <ul class="exclusions">
            {#each Object.entries(preview.exclusions) as [key, count]}<li><span>{key}</span><b>{count}</b></li>{/each}
          </ul>
        {/if}
        {#if preview.durationProtectionWarnings?.length || previewPending}
          <div class="warning">{t('admin.giveaways.protectionWarning')}</div>
        {/if}
      {:else}
        <p class="muted">{t('admin.giveaways.notLive')}</p>
      {/if}
    </article>
  </div>

  <PageToolbar>
    {#snippet lead()}<span>{t('admin.giveaways.history')}</span>{/snippet}
  </PageToolbar>
  <div class="history">
    {#if history.length === 0}<p class="muted">{t('admin.giveaways.empty')}</p>{/if}
    {#each history as giveaway (giveaway.id)}
      <a class="history-row" href={`/giveaways/${encodeURIComponent(giveaway.id)}`}>
        <span><strong>{giveaway.title}</strong><small>{giveaway.winnerCount} × {giveaway.prizeMonths} · {date(giveaway.createdAt)}</small></span>
        <span class="row-meta"><StatePill shape="tag" tone={giveaway.status === 'complete' ? 'positive' : giveaway.status === 'drawn' ? 'warning' : 'neutral'}>{statusLabel(giveaway.status)}</StatePill>{#if giveaway.pendingAwards}<b class="pending">{giveaway.pendingAwards}</b>{/if}</span>
      </a>
    {/each}
  </div>
</section>

<style>
  .giveaway-grid { display:grid; grid-template-columns:minmax(0,1.25fr) minmax(280px,.75fr); gap:18px; margin-top:22px; }
  .panel { padding:24px; border:1px solid var(--bb-border); border-radius:18px; background:var(--bb-surface); }
  h2 { margin:0 0 8px; font-size:18px; } h3 { margin:24px 0 8px; font-size:12px; text-transform:uppercase; letter-spacing:.08em; color:var(--bb-muted); }
  .hint,.muted { color:var(--bb-muted); font-size:13px; line-height:1.5; } .hint { margin:4px 0 16px; }
  .two-col { display:grid; grid-template-columns:1fr 1fr; gap:12px; } .summary { display:block; margin:16px 0; color:var(--bb-tan-pale); font-size:14px; }
  .actions { display:flex; align-items:center; gap:14px; } .actions .hint { margin:0; }
  .error { color:var(--bb-danger,#f28c8c); font-size:13px; } .warning { padding:12px; border-radius:10px; color:#f2c879; background:color-mix(in srgb,#f2c879 12%,transparent); font-size:13px; line-height:1.5; }
  .stats { display:flex; align-items:baseline; gap:10px; margin:18px 0; } .stats strong { font-size:38px; } .stats span { color:var(--bb-muted); }
  .breakdown { display:grid; gap:10px; } .breakdown span,.exclusions li { display:flex; justify-content:space-between; gap:12px; color:var(--bb-muted); font-size:13px; } .breakdown b,.exclusions b { color:var(--bb-text); }
  .exclusions { list-style:none; margin:0; padding:0; display:grid; gap:8px; }
  .history { display:grid; gap:8px; margin-top:12px; } .history-row { display:flex; justify-content:space-between; align-items:center; gap:16px; padding:16px 18px; color:inherit; text-decoration:none; border:1px solid var(--bb-border); border-radius:14px; background:var(--bb-surface); } .history-row:hover { border-color:var(--bb-tan); }
  .history-row strong { display:block; } .history-row small { display:block; color:var(--bb-muted); margin-top:4px; } .row-meta { display:flex; align-items:center; gap:10px; } .pending { color:#f2c879; }
  @media (max-width:760px) { .giveaway-grid { grid-template-columns:1fr; } .actions { align-items:flex-start; flex-direction:column; gap:8px; } }
</style>
