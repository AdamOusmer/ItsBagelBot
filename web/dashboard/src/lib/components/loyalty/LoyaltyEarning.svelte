<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { untrack } from 'svelte';
  import { enhance, type SubmitFunction } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import SaveStatus from '@bagel/ui/svelte/SaveStatus.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import SwitchRow from '@bagel/ui/svelte/SwitchRow.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import { focusFirstInvalid } from '@bagel/ui/svelte/forms';
  import { toast } from '@bagel/ui/svelte/toast';
  import { getI18n, LOYALTY_DEFAULTS, type LoyaltyConfig, type MessageKey } from '@bagel/kit';
  import { reseedOnChange } from '#lib/client-actions.svelte';

  let { initial }: { initial: LoyaltyConfig } = $props();
  const { t, locale } = getI18n();
  let config = $state<LoyaltyConfig>(untrack(() => ({ ...initial })));

  const RATE_MAX = 1_000_000;
  const RATE_KEYS = ['subPoints', 'resubPoints', 'giftSubPoints', 'cheerPointsPer100', 'watchPointsPerTick'] as const;
  type RateKey = (typeof RATE_KEYS)[number];
  type RateState = { on: boolean; amount: number | null; touched: boolean };
  function seedRates(cfg: LoyaltyConfig): Record<RateKey, RateState> {
    return Object.fromEntries(
      RATE_KEYS.map((k) => [k, { on: cfg[k] >= 0, amount: cfg[k] > 0 ? cfg[k] : LOYALTY_DEFAULTS[k], touched: false }])
    ) as Record<RateKey, RateState>;
  }
  let rates = $state(untrack(() => seedRates(initial)));

  const TIER_MIN = 1;
  const TIER_MAX = 10;
  const TIER_KEYS = ['watchTier1Percent', 'watchTier2Percent', 'watchTier3Percent'] as const;
  type TierKey = (typeof TIER_KEYS)[number];
  type TierState = { multiplier: number | null; touched: boolean };
  function seedTiers(cfg: LoyaltyConfig): Record<TierKey, TierState> {
    return Object.fromEntries(
      TIER_KEYS.map((k) => [k, { multiplier: (cfg[k] > 0 ? cfg[k] : LOYALTY_DEFAULTS[k]) / 100, touched: false }])
    ) as Record<TierKey, TierState>;
  }
  let tiers = $state(untrack(() => seedTiers(initial)));
  let attempted = $state(false);
  let formEl = $state<HTMLFormElement | null>(null);
  let busy = $state(false);

  reseedOnChange(() => JSON.stringify(initial), () => {
    config = { ...initial };
    rates = seedRates(initial);
    tiers = seedTiers(initial);
  });

  function encodeRate(key: RateKey): number {
    const r = rates[key];
    if (!r.on) return -1;
    return r.amount === LOYALTY_DEFAULTS[key] ? 0 : (r.amount ?? 0);
  }
  function encodeTier(key: TierKey): number {
    const m = tiers[key].multiplier;
    if (m === null || tierInvalid(key)) return 0;
    const percent = Math.round(m * 100);
    return percent === LOYALTY_DEFAULTS[key] ? 0 : percent;
  }
  const numberFmt = $derived(new Intl.NumberFormat(locale));
  const payload = $derived(
    JSON.stringify({
      ...config,
      ...Object.fromEntries(RATE_KEYS.map((k) => [k, encodeRate(k)])),
      ...Object.fromEntries(TIER_KEYS.map((k) => [k, encodeTier(k)]))
    })
  );

  function oneDecimal(m: number): boolean {
    return Math.abs(m * 10 - Math.round(m * 10)) < 1e-9;
  }
  function tierInvalid(key: TierKey): boolean {
    if (tiersDisabled) return false;
    const m = tiers[key].multiplier;
    return m === null || !oneDecimal(m) || m < TIER_MIN || m > TIER_MAX;
  }
  function tierError(key: TierKey): string | undefined {
    return (attempted || tiers[key].touched) && tierInvalid(key)
      ? t('loyalty.errMultiplier', { min: numberFmt.format(TIER_MIN), max: numberFmt.format(TIER_MAX) })
      : undefined;
  }

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
    if (RATE_KEYS.some(rateInvalid) || TIER_KEYS.some(tierInvalid)) {
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
        toast('success', t('loyalty.toastSaved'));
        await invalidateAll();
        return;
      }
      markSave('error', 4000);
      toast('danger', t('loyalty.toastSaveFailed'));
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

  const TIER_LABELS: Record<TierKey, MessageKey> = {
    watchTier1Percent: 'loyalty.watchTier1',
    watchTier2Percent: 'loyalty.watchTier2',
    watchTier3Percent: 'loyalty.watchTier3'
  };
  const tierFields = $derived(
    TIER_KEYS.map((key) => ({ key, label: t(TIER_LABELS[key]), dflt: LOYALTY_DEFAULTS[key] / 100 }))
  );
  const tiersDisabled = $derived(!config.watchSubBonus || !rates.watchPointsPerTick.on);

  const permToggles = $derived([
    { key: 'modSetPoints', label: t('loyalty.permSet'), hint: t('loyalty.permSetHint') },
    { key: 'modAdjustPoints', label: t('loyalty.permAdjust'), hint: t('loyalty.permAdjustHint') },
    { key: 'viewerTransfers', label: t('loyalty.permTransfers'), hint: t('loyalty.permTransfersHint') }
  ] as const);

</script>

  <section class="block" aria-labelledby="loy-rates-h">
    <Heading level={6} as="h2" variant="title" id="loy-rates-h">{t('loyalty.ratesTitle')}</Heading>
    <Card>
      <div class="hint"><Text size="xs" tone="muted">{t('loyalty.ratesHint')}</Text></div>

      <form method="POST" action="?/save" use:enhance={saveSubmit} class="rates" novalidate bind:this={formEl}>
        <input type="hidden" name="config" value={payload} />

        <Field label={t('loyalty.fieldName')} tag={t('common.optional')}>
          <Input placeholder={t('loyalty.fieldNamePh')} maxlength={32} bind:value={config.pointsName} />
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
                <span class="num">
                  <Input
                    fill
                    type="number"
                    inputmode="numeric"
                    min={1}
                    max={RATE_MAX}
                    step={1}
                    disabled={!rates[rf.key].on}
                    invalid={!!err}
                    aria-invalid={err ? 'true' : undefined}
                    aria-describedby={err ? `rate-err-${rf.key}` : undefined}
                    bind:value={rates[rf.key].amount}
                    onblur={() => (rates[rf.key].touched = true)}
                  />
                </span>
                <Text as="span" size="sm" tone="muted">{rf.unit}</Text>
              </span>
            </Field>
            <span class="rate-switch">
              <Switch
                label={t('loyalty.rateToggleAria', { name: rf.label })}
                checked={rates[rf.key].on}
                onCheckedChange={(v) => (rates[rf.key].on = v)}
              />
            </span>
          </div>
        {/each}

        <div class="perm">
          <SwitchRow
            control="end"
            label={t('loyalty.streamerPoints')}
            hint={t('loyalty.streamerPointsHint')}
            hintId="streamer-points-hint"
            checked={config.streamerPoints >= 0}
            onCheckedChange={(v) => (config.streamerPoints = v ? 0 : -1)}
          />
        </div>

        <div class="hint"><Text size="xs" tone="muted">{t('loyalty.tierHint')}</Text></div>

        <div class="perm">
          <SwitchRow
            control="end"
            label={t('loyalty.watchSubBonus')}
            hint={t('loyalty.watchSubBonusHint')}
            hintId="watch-sub-bonus-hint"
            checked={config.watchSubBonus}
            onCheckedChange={(v) => (config.watchSubBonus = v)}
          />
        </div>

        <div class="tiers">
          {#each tierFields as tf (tf.key)}
            {@const err = tierError(tf.key)}
            <Field
              label={tf.label}
              tag={t('loyalty.defaultTag', { n: numberFmt.format(tf.dflt) })}
              error={err}
              errorId="tier-err-{tf.key}"
            >
              <span class="rate-input">
                <span class="num">
                  <Input
                    fill
                    type="number"
                    inputmode="decimal"
                    min={TIER_MIN}
                    max={TIER_MAX}
                    step={0.1}
                    disabled={tiersDisabled}
                    invalid={!!err}
                    aria-invalid={err ? 'true' : undefined}
                    aria-describedby={err ? `tier-err-${tf.key}` : undefined}
                    bind:value={tiers[tf.key].multiplier}
                    onblur={() => (tiers[tf.key].touched = true)}
                  />
                </span>
                <Text as="span" size="sm" tone="muted">{t('loyalty.unitMultiplier')}</Text>
              </span>
            </Field>
          {/each}
        </div>

        <div class="perm-title"><Heading level={6} as="h3">{t('loyalty.permissionsTitle')}</Heading></div>
        <div class="hint"><Text size="xs" tone="muted">{t('loyalty.permissionsHint')}</Text></div>

        {#each permToggles as pt (pt.key)}
          <div class="perm">
            <SwitchRow
              control="end"
              label={pt.label}
              hint={pt.hint}
              hintId="perm-hint-{pt.key}"
              checked={config[pt.key] >= 0}
              onCheckedChange={(v) => (config[pt.key] = v ? 0 : -1)}
            />
          </div>
        {/each}

        <div class="actions">
          <SaveStatus state={saveState} />
          <Button variant="primary" type="submit" busy={busy}>{t('loyalty.save')}</Button>
        </div>
      </form>
    </Card>
  </section>

<style>
  .block { display: grid; gap: 12px; margin-bottom: 26px; }
  .hint { margin-bottom: 14px; }
  .perm-title { margin: 18px 0 6px; }
  .perm {
    padding: 9px 0;
  }

  .rate { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: start; gap: 12px; }
  .rate-switch { padding-top: 26px; min-height: 44px; display: inline-flex; align-items: flex-start; }
  .rate-input { display: flex; align-items: center; gap: 10px; }
  .num { width: 100%; max-width: 160px; }
  .tiers { display: grid; gap: 12px; padding: 12px 0 9px; }

  .actions { display: flex; align-items: center; justify-content: flex-end; gap: 12px; margin-top: 4px; }
</style>
