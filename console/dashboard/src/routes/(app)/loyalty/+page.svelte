<script lang="ts">
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, Card, PageHead, toast, getI18n, LOYALTY_DEFAULTS, type LoyaltyConfig } from '@bagel/shared';

  let { data } = $props();
  const { t } = getI18n();

  // Local source of truth, reseeded when a fresh SSR load lands.
  // svelte-ignore state_referenced_locally
  let enabled = $state<boolean>(data.enabled ?? false);
  // svelte-ignore state_referenced_locally
  let config = $state<LoyaltyConfig>({ ...data.config });
  let busy = $state(false);
  // svelte-ignore state_referenced_locally
  let seed = data;
  $effect(() => {
    if (data !== seed) {
      seed = data;
      enabled = data.enabled ?? false;
      config = { ...data.config };
    }
  });

  const payload = $derived(JSON.stringify(config));

  const masterSubmit: SubmitFunction = () => {
    const was = enabled;
    enabled = !was;
    return async ({ result }) => {
      if (result.type !== 'success') {
        enabled = was;
        toast('err', t('loyalty.toastToggleFailed'));
      }
    };
  };

  const saveSubmit: SubmitFunction = () => {
    busy = true;
    return async ({ result }) => {
      busy = false;
      if (result.type === 'success') {
        toast('ok', t('loyalty.toastSaved'));
        await invalidateAll();
        return;
      }
      toast('err', t('loyalty.toastSaveFailed'));
    };
  };

  // Rate fields, declared as data so the form is one loop.
  const rateFields = $derived([
    { key: 'subPoints', label: t('loyalty.fieldSub'), dflt: LOYALTY_DEFAULTS.subPoints },
    { key: 'resubPoints', label: t('loyalty.fieldResub'), dflt: LOYALTY_DEFAULTS.resubPoints },
    { key: 'giftSubPoints', label: t('loyalty.fieldGift'), dflt: LOYALTY_DEFAULTS.giftSubPoints },
    { key: 'cheerPointsPer100', label: t('loyalty.fieldCheer'), dflt: LOYALTY_DEFAULTS.cheerPointsPer100 },
    { key: 'watchPointsPerTick', label: t('loyalty.fieldWatch'), dflt: LOYALTY_DEFAULTS.watchPointsPerTick }
  ] as const);

  function hours(seconds: number): string {
    return t('loyalty.hoursShort', { n: (seconds / 3600).toFixed(1) });
  }
</script>

<section class="screen active">
  <PageHead eyebrow={t('loyalty.eyebrow')} description={t('loyalty.description')}>
    {t('loyalty.titlePre')}<em>{t('loyalty.titleEm')}</em>
  </PageHead>

  {#if data.degraded}
    <div class="degraded" role="alert"><Icon name="ban" size={13} /> {t('loyalty.degraded')}</div>
  {/if}

  <div class="toolbar">
    <form method="POST" action="?/toggle" use:enhance={masterSubmit} class="master">
      <input type="hidden" name="is_enabled" value={enabled ? '' : 'on'} />
      <button class="toggle {enabled ? 'on' : ''}" type="submit" aria-label={t('loyalty.botOn')}></button>
      <span class="master-text">
        <span class="master-label">{t('loyalty.botOn')}</span>
        <span class="master-hint">{t('loyalty.botOnHint')}</span>
      </span>
    </form>
  </div>

  <div class="grid">
    <Card style="padding:18px">
      <h3 class="card-title">{t('loyalty.ratesTitle')}</h3>
      <p class="hint">{t('loyalty.ratesHint')}</p>

      <form method="POST" action="?/save" use:enhance={saveSubmit} novalidate>
        <input type="hidden" name="config" value={payload} />

        <label class="field">
          <span>{t('loyalty.fieldName')}</span>
          <input class="search" placeholder={t('loyalty.fieldNamePh')} maxlength="32" bind:value={config.pointsName} />
        </label>

        {#each rateFields as rf (rf.key)}
          <label class="field rate">
            <span>{rf.label} <small class="dflt">{t('loyalty.defaultTag', { n: String(rf.dflt) })}</small></span>
            <input class="search num" type="number" min="-1" max="1000000" bind:value={config[rf.key]} />
          </label>
        {/each}

        <p class="hint">{t('loyalty.tierHint')}</p>

        <div class="actions">
          <button type="submit" class="btn primary" disabled={busy}>
            <Icon name="check" size={14} />
            {busy ? t('loyalty.saving') : t('loyalty.save')}
          </button>
        </div>
      </form>
    </Card>

    <div class="side">
      <Card style="padding:18px">
        <h3 class="card-title">{t('loyalty.topTitle')}</h3>
        {#if (data.top ?? []).length === 0}
          <p class="hint">{t('loyalty.topEmpty')}</p>
        {:else}
          <table class="tbl">
            <thead>
              <tr><th>#</th><th>{t('loyalty.colViewer')}</th><th class="r">{t('loyalty.colPoints')}</th><th class="r">{t('loyalty.colWatch')}</th></tr>
            </thead>
            <tbody>
              {#each data.top ?? [] as row, i (row.viewerId)}
                <tr>
                  <td class="mut">{i + 1}</td>
                  <td>{row.viewerName || row.viewerLogin || row.viewerId}</td>
                  <td class="r">{row.points.toLocaleString()}</td>
                  <td class="r mut">{hours(row.watchSeconds)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
      </Card>

      <Card style="padding:18px">
        <h3 class="card-title">{t('loyalty.chatTitle')}</h3>
        <ul class="cmds">
          <li><code>!points</code><span>{t('loyalty.chatPoints')}</span></li>
          <li><code>!points set/add @user 500</code><span>{t('loyalty.chatPointsMod')}</span></li>
          <li><code>!counter</code><span>{t('loyalty.chatCounter')}</span></li>
        </ul>
      </Card>
    </div>
  </div>
</section>

<style>
  .degraded {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 14px;
    padding: 10px 14px;
    border: 1px solid rgba(176, 90, 70, 0.4);
    border-radius: 8px;
    background: rgba(176, 90, 70, 0.08);
    color: #cf8a78;
    font-family: var(--bb-font-body);
    font-size: 13px;
  }

  .master { display: inline-flex; align-items: center; gap: 12px; }
  .master-text { display: flex; flex-direction: column; gap: 1px; min-width: 0; }
  .master-label {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 13px;
    color: var(--bb-white);
  }
  .master-hint { font-family: var(--bb-font-body); font-size: 11.5px; color: var(--bb-muted); }

  .grid {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 16px;
    align-items: start;
  }
  @media (min-width: 900px) {
    .grid { grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr); }
  }
  .side { display: flex; flex-direction: column; gap: 16px; }

  .card-title {
    margin: 0 0 6px;
    font-family: var(--bb-font-display);
    font-size: 15px;
    color: var(--bb-white);
  }
  .hint { margin: 0 0 14px; font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }

  .field { display: flex; flex-direction: column; gap: 6px; margin-bottom: 14px; }
  .field > span { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); }
  .field :global(.search) { width: 100%; box-sizing: border-box; }
  .rate .num { max-width: 160px; }
  .dflt { opacity: 0.65; font-size: 11px; }

  .actions { display: flex; justify-content: flex-end; margin-top: 4px; }

  .tbl { width: 100%; border-collapse: collapse; font-family: var(--bb-font-body); font-size: 13px; }
  .tbl th {
    text-align: left;
    font-size: 11px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--bb-muted);
    padding: 4px 8px;
    border-bottom: 1px solid var(--bb-border);
  }
  .tbl td { padding: 7px 8px; border-bottom: 1px solid rgba(240, 236, 228, 0.05); color: var(--bb-white); }
  .tbl .r { text-align: right; }
  .tbl .mut { color: var(--bb-muted); }

  .cmds { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 10px; }
  .cmds li { display: flex; flex-direction: column; gap: 2px; }
  .cmds code {
    font-size: 12.5px;
    color: var(--bb-white);
    background: rgba(0, 0, 0, 0.35);
    border: 1px solid var(--bb-border);
    border-radius: 6px;
    padding: 3px 8px;
    width: fit-content;
  }
  .cmds span { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }

  @media (max-width: 480px) {
    .actions .btn { width: 100%; justify-content: center; min-height: 44px; }
  }
</style>
