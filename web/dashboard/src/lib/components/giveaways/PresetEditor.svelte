<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { untrack } from 'svelte';
  import { enhance, type SubmitFunction } from '$app/forms';
  import { getI18n, GIVEAWAY_LIMITS as pins, giveawayRuleNumbers, giveawayRuleToggles } from '@bagel/kit';
  import '@bagel/ui/styles/elements/collapse.css';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import SwitchRow from '@bagel/ui/svelte/SwitchRow.svelte';
  import type { GiveawayConfig, GiveawayPreset, GiveawayRules } from '#lib/channel-giveaway';

  let { config, revision, enabled, busy, locked, submit }: {
    config: GiveawayConfig;
    revision: number;
    enabled: boolean;
    busy: boolean;
    locked: boolean;
    submit: SubmitFunction;
  } = $props();

  const { t } = getI18n();
  const roles = ['everyone', 'sub', 'vip'] as const;
  type Draft = GiveawayPreset & { id: string };
  const empty: GiveawayPreset = { name: '', rules: {} };

  let nextId = 0;
  const draftOf = (preset: GiveawayPreset): Draft => ({ ...preset, rules: { ...preset.rules }, id: String(nextId++) });
  let drafts = $state(untrack(() => config.presets.map(draftOf)));
  let selected = $state(untrack(() => drafts[0]?.id ?? ''));
  let defaultId = $state(untrack(() => drafts.find((draft) => draft.name === config.defaultPreset)?.id ?? ''));
  const active = $derived(drafts.find((draft) => draft.id === selected));
  const options = $derived(drafts.map((draft) => ({ value: draft.id, label: draft.name || t('giveaways.newPreset') })));
  const defaultPreset = $derived(drafts.find((draft) => draft.id === defaultId)?.name ?? '');
  const preview = $derived(JSON.stringify(drafts.map(({ id, ...preset }) => preset)));
  const signature = (presets: string, preset: string) => JSON.stringify([presets, preset]);
  const saved = $derived(signature(JSON.stringify(config.presets), config.defaultPreset));
  let baseline = $state(untrack(() => saved));
  let expected = $state(untrack(() => revision));
  const dirty = $derived(signature(preview, defaultPreset) !== baseline);
  const stale = $derived(saved !== baseline);
  const invalidLuck = (value = 0) => value > 0 && value < pins.luckMin;
  const invalidDraft = $derived(drafts.some(({ rules }) => invalidLuck(rules.sub_luck_percent)));

  $effect(() => {
    if (!stale) expected = revision;
    else if (!dirty) reset();
  });
  const draft = $derived(active ?? empty);

  function add() {
    const item = draftOf(empty);
    drafts.push(item);
    selected = item.id;
  }

  function remove() {
    drafts = drafts.filter((item) => item.id !== selected);
    if (defaultId === selected) defaultId = '';
    selected = drafts[0]?.id ?? '';
  }

  function rule<Key extends keyof GiveawayRules>(key: Key, value: GiveawayRules[Key]) {
    if (active) active.rules[key] = value;
  }

  function reset() {
    const name = active?.name.trim().toLowerCase();
    drafts = config.presets.map(draftOf);
    selected = drafts.find((item) => item.name === name)?.id ?? drafts[0]?.id ?? '';
    defaultId = drafts.find((item) => item.name === config.defaultPreset)?.id ?? '';
    baseline = saved;
    expected = revision;
  }

  const save: SubmitFunction = async (event) => {
    const after = await submit(event);
    return async (result) => {
      if (after) await after(result);
      if (result.result.type === 'success') reset();
    };
  };
</script>

<Card>
  <form class="editor" method="POST" action="?/savePresets" use:enhance={save}>
    <input type="hidden" name="expected_rev" value={expected} />
    <input type="hidden" name="is_enabled" value={enabled ? 'on' : 'off'} />
    <input type="hidden" name="presets" value={preview} />
    <input type="hidden" name="default_preset" value={defaultPreset} />
    <CardHead title={t('giveaways.presets')} headingLevel={2}>
      {#snippet actions()}
        <Button variant="secondary" size="sm" disabled={locked || busy || drafts.length >= pins.giveawayMaxPresets} onclick={add}>
          <bb-fit><span data-fit="">{t('giveaways.addPreset')}</span></bb-fit>
        </Button>
      {/snippet}
    </CardHead>

    <div class="fields">
      <Field fitCopy label={t('giveaways.editPreset')}>
        <Select value={selected} {options} label={t('giveaways.editPreset')} fill disabled={locked || busy || !drafts.length}
          onchange={(event) => { selected = event.currentTarget.value; }} />
      </Field>
      <Field fitCopy label={t('giveaways.defaultPreset')}>
        <Select value={defaultId} options={[{ value: '', label: t('giveaways.noPreset') }, ...options]}
          label={t('giveaways.defaultPreset')} fill disabled={locked || busy || !drafts.length}
          onchange={(event) => { defaultId = event.currentTarget.value; }} />
      </Field>
    </div>

    <div class="bb-collapse rules" class:is-open={!!active} inert={!active || locked || busy} aria-hidden={!active}>
    <div>
    <div class="fields">
      <Field fitCopy label={t('giveaways.presetName')}>
        <Input bind:value={() => draft.name, (value) => { if (active) active.name = String(value ?? '').toLowerCase(); }}
          maxlength={pins.giveawayPresetNameMax} pattern="[a-z0-9\-]+" fill />
      </Field>
      <Field fitCopy label={t('giveaways.role')}>
        <Select value={draft.rules.role ?? 'everyone'} options={roles.map((role) => ({ value: role, label: t(`perm.${role}`) }))}
          label={t('giveaways.role')} onchange={(event) => rule('role', roles.find((role) => role === event.currentTarget.value))} fill />
      </Field>
      {#each giveawayRuleNumbers as { key, max } (key)}
        {@const invalid = key === 'sub_luck_percent' && invalidLuck(draft.rules[key])}
        <Field fitCopy label={t(`giveaways.rule.${key}`, { min: pins.luckMin, max: pins.luckMax })}>
          <Input type="number" min={0} {max} value={draft.rules[key] ?? 0} {invalid} aria-invalid={invalid || undefined} fill
            oninput={(event) => rule(key, Number(event.currentTarget.value))} />
        </Field>
      {/each}
      {#each giveawayRuleToggles as key (key)}
        <SwitchRow label={t(`giveaways.rule.${key}`)} fitCopy checked={draft.rules[key] ?? false}
          onCheckedChange={(checked) => rule(key, checked)} />
      {/each}
    </div>
    </div>
    </div>

    <div class="actions">
      <Button variant="ghost" class={active ? 'presetControl' : 'presetControl faded'} disabled={!active || locked || busy} onclick={remove}>
        <bb-fit><span data-fit="">{t('giveaways.removePreset')}</span></bb-fit>
      </Button>
      <Button variant="secondary" class={stale ? 'presetControl' : 'presetControl faded'} disabled={!stale || busy} onclick={reset}>
        <bb-fit><span data-fit="">{t('giveaways.reloadPresets')}</span></bb-fit>
      </Button>
      <Button type="submit" {busy} disabled={!dirty || locked || stale || invalidDraft}><bb-fit><span data-fit="">{t('giveaways.savePresets')}</span></bb-fit></Button>
    </div>
  </form>
</Card>

<style>
  .editor { display: flex; flex-direction: column; gap: var(--bb-space-4); }
  .fields { display: grid; gap: var(--bb-space-3); grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .actions {
    --btn-w: 100%;
    display: grid;
    gap: var(--bb-space-2);
    grid-template-columns: repeat(3, minmax(0, 9rem));
    justify-content: end;
  }
  .rules { --bb-collapse-gap: var(--bb-space-4); }
  .actions :global(.presetControl) { transition: opacity var(--bb-dur-fast); }
  .actions :global(.faded) { opacity: 0; pointer-events: none; }
  @media (max-width: 850px) {
    .fields { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  }
</style>
