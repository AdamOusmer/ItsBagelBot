<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import { getI18n, GIVEAWAY_LIMITS as pins } from '@bagel/kit';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import SwitchRow from '@bagel/ui/svelte/SwitchRow.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';

  let { config, loyaltyEnabled, blocked, busy, submit }: {
    config: Record<string, string>;
    loyaltyEnabled: boolean;
    blocked: boolean;
    busy: boolean;
    submit: SubmitFunction;
  } = $props();

  const { t } = getI18n();
  let mode = $state<string>(pins.raffleBareMode);
  let auto = $state(false);
  const split = $derived(mode === 'split');
  const disabled = $derived(blocked || busy);
  const manual = $derived(split && !auto);
  const modes = $derived([
    { value: 'single', label: t('modules.catalog.raffle.settings.bareRaffle.options.single') },
    { value: 'split', label: t('modules.catalog.raffle.settings.bareRaffle.options.split') }
  ]);
  const hint = $derived(loyaltyEnabled ? '' : t('giveaways.raffleLoyaltyOff'));
</script>

<form method="POST" action="?/raffleStart" use:enhance={submit} class="start">
  <input type="hidden" name="auto" value="on" disabled={!split || !auto} />
  <div class="fields">
    <Field fitCopy label={t('modules.catalog.raffle.settings.prizePoints.label')}>
      <Input name="amount" type="number" min={1} max={pins.raffleMaxPrize}
        placeholder={config.prizePoints || String(pins.raffleDefaultPrize)} fill {disabled} />
    </Field>
    <Field fitCopy label={t('giveaways.entrySeconds')}>
      <Input name="seconds" type="number" min={pins.raffleMinSeconds} max={pins.maxOpen}
        placeholder={config.durationSeconds || String(pins.raffleDefaultSeconds)} fill {disabled} />
    </Field>
    <Field fitCopy label={t('giveaways.raffleMode')}>
      <Select name="mode" value={mode} options={modes} label={t('giveaways.raffleMode')}
        onchange={(event) => { mode = event.currentTarget.value; }} fill {disabled} />
    </Field>
    <div class="conditional" class:faded={!manual} inert={!manual}>
      <Field fitCopy label={t('giveaways.winners')}>
        <Input name="winners" type="number" min={1} max={pins.maxWinners}
          placeholder={String(pins.splitRaffleDefaultWinners)}
          fill disabled={disabled || !manual} />
      </Field>
    </div>
  </div>
  <div class="conditional auto" class:faded={!split} inert={!split}>
    <SwitchRow fitCopy label={t('giveaways.autoWinners')} checked={auto} {disabled}
      onCheckedChange={(checked) => { auto = checked; }} />
  </div>
  <div class="footer">
    <div class="hint"><Text tone="muted" size="sm"><bb-fit><span data-fit="">{hint}</span></bb-fit></Text></div>
    <Button type="submit" disabled={!loyaltyEnabled || blocked} {busy}>
      <bb-fit><span data-fit="">{t('giveaways.startRaffle')}</span></bb-fit>
    </Button>
  </div>
</form>

<style>
  .start { display: grid; grid-template-columns: minmax(0, 1fr); gap: var(--bb-space-4); }
  .fields { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: var(--bb-space-3); }
  .auto { max-inline-size: 24rem; }
  .conditional { transition: opacity var(--bb-dur-fast); }
  .faded { opacity: 0; }
  .footer { --btn-w: 10rem; display: flex; gap: var(--bb-space-3); justify-content: space-between; align-items: center; }
  .hint { min-inline-size: 0; flex: 1; }
  @media (max-width: 850px) {
    .fields { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  }
  @media (max-width: 450px) {
    .fields { grid-template-columns: minmax(0, 1fr); }
  }
  @media (prefers-reduced-motion: reduce) {
    .conditional { transition: none; }
  }
</style>
