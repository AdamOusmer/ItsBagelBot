<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import { getI18n, GIVEAWAY_LIMITS as pins } from '@bagel/kit';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { EligibleCount, GiveawayConfig } from '#lib/channel-giveaway';

  let { config, enabled, blocked, busy, demo, active, submit }: {
    config: GiveawayConfig;
    enabled: boolean;
    blocked: boolean;
    busy: boolean;
    demo: boolean;
    active: boolean;
    submit: SubmitFunction;
  } = $props();

  const { t, locale } = getI18n();
  const number = new Intl.NumberFormat(locale);

  let chosen = $state('');
  let count = $state<EligibleCount | null>();
  const preset = $derived(config.presets.some((item) => item.name === chosen) ? chosen : '');
  const presets = $derived([
    { value: '', label: config.defaultPreset ? t('giveaways.useDefault', { preset: config.defaultPreset }) : t('giveaways.noPreset') },
    ...config.presets.map((item) => ({ value: item.name, label: item.name }))
  ]);

  const countable = () => active && enabled && !demo && !blocked;

  $effect(() => {
    const selected = preset;
    void config.defaultPreset;
    void config.presets;
    if (!countable()) return;
    const controller = new AbortController();
    count = undefined;
    void fetch(`/giveaways/eligible?preset=${encodeURIComponent(selected)}`, { signal: controller.signal })
      .then(async (response) => response.ok ? await response.json() as EligibleCount | null : null)
      .catch(() => null)
      .then((reply) => {
        if (controller.signal.aborted) return;
        count = reply;
      });
    return () => controller.abort();
  });

  function eligibleCopy(value: EligibleCount | null | undefined) {
    if (!enabled) return t('giveaways.disabled');
    if (blocked) return t('giveaways.eligibleLabel');
    if (!value) return t(value === undefined ? 'giveaways.eligibleLoading' : 'giveaways.eligibleUnavailable');
    return t('giveaways.eligibleCount', { count: number.format(value.count) });
  }
</script>

<form method="POST" action="?/start" use:enhance={submit} class="start">
  <div class="fields">
    <Field fitCopy label={t('giveaways.preset')}>
      <Select name="preset" value={preset} options={presets} label={t('giveaways.preset')}
        onchange={(event) => { chosen = event.currentTarget.value; }} fill disabled={blocked || busy} />
    </Field>
    <Field fitCopy label={t('giveaways.prize')}>
      <Input name="prize" maxlength={pins.giveawayPrizeMaxLen} placeholder={config.fields.defaultPrize || t('giveaways.prize')} fill disabled={blocked || busy} />
    </Field>
    <Field fitCopy label={t('giveaways.winners')}>
      <Input name="winners" type="number" min={1} max={pins.maxWinners} placeholder={config.fields.winners} fill disabled={blocked || busy} />
    </Field>
    <Field fitCopy label={t('giveaways.entrySeconds')}>
      <Input name="window_seconds" type="number" min={pins.giveawayMinEntryWindow} max={pins.maxOpen} placeholder={config.fields.windowSeconds} fill disabled={blocked || busy} />
    </Field>
  </div>
  <div class="footer">
    <div class="count" role="status"><Text tone="muted" size="sm"><bb-fit><span data-fit="">{eligibleCopy(count)}</span></bb-fit></Text></div>
    <Button type="submit" disabled={!enabled || blocked} {busy}><bb-fit><span data-fit="">{t('giveaways.startGiveaway')}</span></bb-fit></Button>
  </div>
</form>

<style>
  .start { display: grid; gap: var(--bb-space-4); }
  .fields { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: var(--bb-space-3); }
  .footer { --btn-w: 7rem; display: flex; gap: var(--bb-space-3); justify-content: space-between; align-items: center; }
  .count { min-width: 0; flex: 1; }
  @media (max-width: 850px) {
    .fields { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  }
  @media (max-width: 450px) {
    .fields { grid-template-columns: minmax(0, 1fr); }
  }
</style>
