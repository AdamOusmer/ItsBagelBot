<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    PageHead,
    MasterToggle,
    PageToolbar,
    Switch,
    focusFirstInvalid,
    AlertBanner,
    Card,
    ButtonLink,
    Button,
    Field,
    EmptyState,
    SaveStatus,
    DeckList,
    toast,
    getI18n,
    LOYALTY_DEFAULTS,
    formatPointValue,
    type MessageKey,
    moduleDef,
    catalogChildren,
    type LoyaltyConfig
  } from '@bagel/kit';
  import type { SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import ModuleCommandList from '$lib/components/modules/ModuleCommandList.svelte';
  import LoyaltyGameRow from '$lib/components/loyalty/LoyaltyGameRow.svelte';

  let { data } = $props();
  const { t, locale } = getI18n();
  const loyaltyCommands = moduleDef('loyalty')?.commands ?? [];

  // svelte-ignore state_referenced_locally
  let enabled = $state<boolean>(data.enabled ?? false);
  // svelte-ignore state_referenced_locally
  let config = $state<LoyaltyConfig>({ ...data.config });
  function seedGames(src: typeof data) {
    return catalogChildren('loyalty').map((def) => ({
      def,
      enabled: src.games?.find((g) => g.id === def.id)?.enabled ?? false
    }));
  }
  // svelte-ignore state_referenced_locally
  let games = $state(seedGames(data));

  const RATE_MAX = 1_000_000;
  const RATE_KEYS = ['subPoints', 'resubPoints', 'giftSubPoints', 'cheerPointsPer100', 'watchPointsPerTick'] as const;
  type RateKey = (typeof RATE_KEYS)[number];
  type RateState = { on: boolean; amount: number | null; touched: boolean };
  function seedRates(cfg: LoyaltyConfig): Record<RateKey, RateState> {
    return Object.fromEntries(
      RATE_KEYS.map((k) => [k, { on: cfg[k] >= 0, amount: cfg[k] > 0 ? cfg[k] : LOYALTY_DEFAULTS[k], touched: false }])
    ) as Record<RateKey, RateState>;
  }
  // svelte-ignore state_referenced_locally
  let rates = $state(seedRates(data.config));
  let attempted = $state(false);
  let formEl = $state<HTMLFormElement | null>(null);
  let busy = $state(false);
  // svelte-ignore state_referenced_locally
  let seed = data;
  $effect(() => {
    if (data !== seed) {
      seed = data;
      enabled = data.enabled ?? false;
      config = { ...data.config };
      rates = seedRates(data.config);
      games = seedGames(data);
    }
  });

  let prevOn: boolean | undefined;
  $effect(() => {
    if (prevOn && !enabled) {
      for (const g of games) g.enabled = false;
    }
    prevOn = enabled;
  });

  function encodeRate(key: RateKey): number {
    const r = rates[key];
    if (!r.on) return -1;
    return r.amount === LOYALTY_DEFAULTS[key] ? 0 : (r.amount ?? 0);
  }
  const numberFmt = $derived(new Intl.NumberFormat(locale));
  const payload = $derived(
    JSON.stringify({ ...config, ...Object.fromEntries(RATE_KEYS.map((k) => [k, encodeRate(k)])) })
  );

  function rateInvalid(key: RateKey): boolean {
    const r = rates[key];
    return r.on && !(r.amount !== null && Number.isInteger(r.amount) && r.amount >= 1 && r.amount <= RATE_MAX);
  }
  function rateError(key: RateKey): string | undefined {
    return (attempted || rates[key].touched) && rateInvalid(key)
      ? t('loyalty.errRate', { max: numberFmt.format(RATE_MAX) })
      : undefined;
  }

  let saveState = $state<SaveState>('idle');
  let saveTimer: ReturnType<typeof setTimeout> | undefined;
  function markSave(s: SaveState, resetAfter = 0) {
    clearTimeout(saveTimer);
    saveState = s;
    if (resetAfter) saveTimer = setTimeout(() => (saveState = 'idle'), resetAfter);
  }

  const saveSubmit: SubmitFunction = (input) => {
    if (RATE_KEYS.some(rateInvalid)) {
      attempted = true;
      input.cancel();
      void focusFirstInvalid(formEl);
      return;
    }
    busy = true;
    markSave('saving');
    return async ({ result }) => {
      busy = false;
      if (result.type === 'success') {
        markSave('saved', 4000);
        toast('ok', t('loyalty.toastSaved'));
        await invalidateAll();
        return;
      }
      markSave('error', 4000);
      toast('err', t('loyalty.toastSaveFailed'));
    };
  };

  const pointsLabel = $derived(config.pointsName.trim() || t('loyalty.fieldNamePh'));
  const RATE_COPY: Record<RateKey, { label: MessageKey; unit: MessageKey }> = {
    subPoints: { label: 'loyalty.fieldSub', unit: 'loyalty.unitSub' },
    resubPoints: { label: 'loyalty.fieldResub', unit: 'loyalty.unitResub' },
    giftSubPoints: { label: 'loyalty.fieldGift', unit: 'loyalty.unitGift' },
    cheerPointsPer100: { label: 'loyalty.fieldCheer', unit: 'loyalty.unitCheer' },
    watchPointsPerTick: { label: 'loyalty.fieldWatch', unit: 'loyalty.unitWatch' }
  };
  const rateFields = $derived(
    RATE_KEYS.map((key) => ({
      key,
      label: t(RATE_COPY[key].label),
      unit: t(RATE_COPY[key].unit, { name: pointsLabel }),
      dflt: LOYALTY_DEFAULTS[key]
    }))
  );

  const permToggles = $derived([
    { key: 'modSetPoints', label: t('loyalty.permSet'), hint: t('loyalty.permSetHint') },
    { key: 'modAdjustPoints', label: t('loyalty.permAdjust'), hint: t('loyalty.permAdjustHint') },
    { key: 'viewerTransfers', label: t('loyalty.permTransfers'), hint: t('loyalty.permTransfersHint') }
  ] as const);

  function hours(seconds: number): string {
    const n = new Intl.NumberFormat(locale, { minimumFractionDigits: 1, maximumFractionDigits: 1 });
    return t('loyalty.hoursShort', { n: n.format(seconds / 3600) });
  }

  const top = $derived(data.top ?? []);
</script>

<section class="screen active">
  <PageHead eyebrow={t('loyalty.eyebrow')} description={t('loyalty.description')}>
    {t('loyalty.titlePre')}<em>{t('loyalty.titleEm')}</em>
  </PageHead>

  {#if data.degraded}
    <AlertBanner>{t('loyalty.degraded')}</AlertBanner>
  {/if}

  <PageToolbar>
    {#snippet lead()}
      <MasterToggle
        action="?/toggle"
        bind:enabled
        label={t('loyalty.botOn')}
        hint={t('loyalty.botOnHint')}
        ariaLabel={t('loyalty.botOn')}
        failMessage={t('loyalty.toastToggleFailed')}
      />
    {/snippet}
    {#snippet trail()}
      <ButtonLink href="/counters" variant="ghost">{t('loyalty.countersLink')}</ButtonLink>
    {/snippet}
  </PageToolbar>

  {#if games.length}
    <section class="block" aria-labelledby="loy-games-h">
      <h2 id="loy-games-h" class="block-title">{t('loyalty.gamesTitle')}</h2>
      <Card>
        <p class="hint">{enabled ? t('loyalty.gamesHint') : t('loyalty.gamesNeedsOn')}</p>
        {#each games as g (g.def.id)}
          <LoyaltyGameRow def={g.def} bind:enabled={g.enabled} loyaltyOn={enabled} />
        {/each}
      </Card>
    </section>
  {/if}

  <section class="block" aria-labelledby="loy-rates-h">
    <h2 id="loy-rates-h" class="block-title">{t('loyalty.ratesTitle')}</h2>
    <Card>
      <p class="hint">{t('loyalty.ratesHint')}</p>

      <form method="POST" action="?/save" use:enhance={saveSubmit} class="rates" novalidate bind:this={formEl}>
        <input type="hidden" name="config" value={payload} />

        <Field label={t('loyalty.fieldName')} tag={t('common.optional')}>
          <input class="bb-input" placeholder={t('loyalty.fieldNamePh')} maxlength="32" bind:value={config.pointsName} />
        </Field>

        {#each rateFields as rf (rf.key)}
          {@const err = rateError(rf.key)}
          <div class="rate">
            <Field
              label={rf.label}
              tag={t('loyalty.defaultTag', { n: numberFmt.format(rf.dflt) })}
              error={err}
              errorId="rate-err-{rf.key}"
            >
              <span class="rate-input">
                <input
                  class="bb-input num"
                  type="number"
                  inputmode="numeric"
                  min="1"
                  max={RATE_MAX}
                  step="1"
                  disabled={!rates[rf.key].on}
                  data-invalid={err ? '' : undefined}
                  aria-invalid={err ? 'true' : undefined}
                  aria-describedby={err ? `rate-err-${rf.key}` : undefined}
                  bind:value={rates[rf.key].amount}
                  onblur={() => (rates[rf.key].touched = true)}
                />
                <span class="unit">{rf.unit}</span>
              </span>
            </Field>
            <span class="rate-switch">
              <Switch
                label={t('loyalty.rateToggleAria', { name: rf.label })}
                checked={rates[rf.key].on}
                onchange={(v) => (rates[rf.key].on = v)}
              />
            </span>
          </div>
        {/each}

        <div class="perm">
          <span class="perm-copy">
            <span class="perm-label">{t('loyalty.streamerPoints')}</span>
            <span class="perm-hint" id="streamer-points-hint">{t('loyalty.streamerPointsHint')}</span>
          </span>
          <Switch
            label={t('loyalty.streamerPoints')}
            describedby="streamer-points-hint"
            checked={config.streamerPoints >= 0}
            onchange={(v) => (config.streamerPoints = v ? 0 : -1)}
          />
        </div>

        <p class="hint">{t('loyalty.tierHint')}</p>

        <h3 class="perm-title">{t('loyalty.permissionsTitle')}</h3>
        <p class="hint">{t('loyalty.permissionsHint')}</p>

        {#each permToggles as pt (pt.key)}
          <div class="perm">
            <span class="perm-copy">
              <span class="perm-label">{pt.label}</span>
              <span class="perm-hint" id="perm-hint-{pt.key}">{pt.hint}</span>
            </span>
            <Switch
              label={pt.label}
              describedby="perm-hint-{pt.key}"
              checked={config[pt.key] >= 0}
              onchange={(v) => (config[pt.key] = v ? 0 : -1)}
            />
          </div>
        {/each}

        <div class="actions">
          <SaveStatus state={saveState} />
          <Button variant="primary" type="submit" loading={busy}>{t('loyalty.save')}</Button>
        </div>
      </form>
    </Card>
  </section>

  <section class="block" aria-labelledby="loy-top-h">
    <h2 id="loy-top-h" class="block-title">{t('loyalty.topTitle')}</h2>
    <Card>
      {#if top.length === 0}
        <EmptyState title={t('loyalty.topEmpty')}>
          <ButtonLink href="#loy-rates-h" variant="ghost">{t('loyalty.topEmptyCta')}</ButtonLink>
        </EmptyState>
      {:else}
        <div class="bb-tbl-wrap">
          <table class="bb-tbl standings">
            <caption class="bb-sr-only">{t('loyalty.topCaption')}</caption>
            <thead>
              <tr>
                <th scope="col" class="r">{t('loyalty.colRank')}</th>
                <th scope="col">{t('loyalty.colViewer')}</th>
                <th scope="col" class="r">{t('loyalty.colPoints')}</th>
                <th scope="col" class="r">{t('loyalty.colWatch')}</th>
              </tr>
            </thead>
            <tbody>
              {#each top as row, i (row.viewerId)}
                <tr>
                  <th scope="row" class="r rank">{i + 1}</th>
                  <td>{row.viewerName || row.viewerLogin || row.viewerId}</td>
                  <td class="r">{formatPointValue(row.points, locale)}</td>
                  <td class="r mut">{hours(row.watchSeconds)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </Card>
  </section>

  {#if loyaltyCommands.length}
    <div class="cmd-block">
      <DeckList>
        <ModuleCommandList moduleId="loyalty" commands={loyaltyCommands} headingId="loy-chat-h" />
      </DeckList>
    </div>
  {/if}
</section>

<style>
  .block { margin-bottom: 26px; }
  .block-title {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 16px;
    letter-spacing: -0.01em;
    color: var(--bb-white);
    margin: 0 0 12px;
  }


  .hint { margin: 0 0 14px; font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }

  .perm-title {
    font-family: var(--bb-font-body);
    font-size: 13px;
    font-weight: 600;
    color: var(--bb-white);
    margin: 18px 0 6px;
  }
  .perm {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 9px 0;
    border-bottom: 1px solid rgba(240, 236, 228, 0.05);
  }
  .perm:last-of-type { border-bottom: none; }
  .perm-copy { display: flex; flex-direction: column; gap: 1px; }
  .perm-label { font-family: var(--bb-font-body); font-size: 13px; color: var(--bb-white); }
  .perm-hint { font-family: var(--bb-font-body); font-size: 11.5px; color: var(--bb-muted); }

  .rates :global(.num) { max-width: 160px; }
  .rate { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: start; gap: 12px; }
  .rate-switch { padding-top: 26px; min-height: 44px; display: inline-flex; align-items: flex-start; }
  .rate-input { display: flex; align-items: center; gap: 10px; }
  .unit { font-family: var(--bb-font-body); font-size: 13px; color: var(--bb-muted); }

  .actions { display: flex; align-items: center; justify-content: flex-end; gap: 12px; margin-top: 4px; }

  .cmd-block { margin-top: 26px; }

  .standings th[scope='row'] { font-family: var(--bb-font-mono); font-variant-numeric: tabular-nums; }
  .standings .rank { color: var(--bb-muted); }

  @media (max-width: 480px) {
    .actions { flex-wrap: wrap; }
    .actions { --btn-min-h: 44px; }
  }
</style>
