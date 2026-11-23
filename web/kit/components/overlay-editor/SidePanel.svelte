<script module lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  export type Section = 'layers' | 'settings' | 'layout' | 'text' | '';
</script>

<script lang="ts">
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Disclosure from '@bagel/ui/svelte/Disclosure.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '../../lib/i18n/context';
  import type { OverlayDocument, OverlayWidget } from '../../lib/overlay-document';
  import { ruleKind } from '../../lib/overlay-editor/alert-rules';
  import type { MediaBinding } from '../../lib/overlay-editor/binding';
  import { applySetting, completeSettings, isEditable, type AlertKind, type Settings } from '../../lib/overlay-editor/defaults';
  import { creditHeaders, tierWeights, typography, widgetFields } from '../../lib/overlay-editor/fields';
  import AlertEntry from './AlertEntry.svelte';
  import AlertRules from './AlertRules.svelte';
  import Fit from './Fit.svelte';
  import GeometryFields from './GeometryFields.svelte';
  import LayersList, { type LayerActions } from './LayersList.svelte';
  import PanelHead from './PanelHead.svelte';
  import Reveal from './Reveal.svelte';
  import SettingControl from './SettingControl.svelte';

  let { doc, selected, widget, full, binding, section = $bindable('layers'), actions, update, onadd, onclose }: {
    doc: OverlayDocument;
    selected: string;
    widget?: OverlayWidget;
    full: boolean;
    binding?: MediaBinding;
    section?: Section;
    actions: LayerActions;
    update: (widget: OverlayWidget) => void;
    onadd: () => void;
    onclose: () => void;
  } = $props();

  const { t } = getI18n();
  const nested = {
    goal: { key: 'weights', fields: tierWeights },
    credits: { key: 'headers', fields: creditHeaders }
  } as const;

  let advanced = $state<AlertKind | ''>('');
  let last = $state.raw<OverlayWidget>();
  let seen = '';

  $effect.pre(() => {
    if (selected !== seen) advanced = '';
    seen = selected;
    if (widget) last = widget;
  });

  const shown = $derived(widget ?? last);
  const settings: Settings = $derived(shown ? completeSettings(shown.kind, shown.settings) : {});
  const fields = $derived(shown ? widgetFields[shown.kind] : []);
  const editable = $derived(!!shown && isEditable(shown.kind));
  const font = $derived(typeof settings.style === 'object' ? (settings.style as Settings) : undefined);
  const group = $derived(shown?.kind === 'goal' || shown?.kind === 'credits' ? nested[shown.kind] : undefined);
  const groupValues = $derived(group ? (settings[group.key] as Settings) : {});
  const groupTitle = $derived(shown?.kind === 'goal' ? t('overlayEditor.ui.weightsTitle') : t('overlayEditor.ui.headingsTitle'));
  const rules = $derived(shown ? ruleKind(shown.kind) : undefined);
  const subview = $derived(advanced && rules ? { rules, event: advanced } : undefined);

  const isOpen = (name: Section): boolean => section === name;
  const toggle = (name: Section, open: boolean): void => {
    if (open) section = name;
    else if (section === name) section = '';
  };
  const setSettings = (next: Settings): void => {
    if (shown) update({ ...shown, settings: next });
  };

  function change(key: string, value: unknown): void {
    if (shown) setSettings(applySetting(shown.kind, settings, key, value));
  }
</script>

<aside class="panel" aria-label={t('overlayEditor.ui.sidePanel')}>
  <div class="sheet-only">
    <PanelHead title={t('overlayEditor.ui.sidePanel')} closeLabel={t('overlayEditor.ui.closePanel')} {onclose} />
  </div>
  {#if subview}
    <PanelHead
      title={t('overlayEditor.ui.advancedTitle', { event: t(`overlayEditor.alert.${subview.event}`) })}
      closeLabel={t('overlayEditor.ui.closeAdvanced')}
      onclose={() => (advanced = '')}
    />
  {/if}
  <div class="scroll">
    {#if subview}
      <AlertEntry kind={subview.rules} event={subview.event} {settings} update={setSettings} {binding} />
    {:else}
      <Reveal open={!widget}>
        <div class="welcome">
          <p class="welcome-title"><Fit>{t('overlayEditor.ui.welcomeTitle')}</Fit></p>
          <Text size="sm" tone="muted">{t('overlayEditor.ui.welcomeBody')}</Text>
          <Button block onclick={onadd}>
            <Icon name="plus" size={16} />
            <Fit>{t('overlayEditor.ui.addWidget')}</Fit>
          </Button>
        </div>
      </Reveal>
      <Disclosure summary={t('overlayEditor.ui.layers')} size="sm" bind:open={() => isOpen('layers'), (open) => toggle('layers', open)}>
        <LayersList {doc} {selected} {full} {actions} />
      </Disclosure>
      <Reveal open={!!widget}>
        {#if shown}
          <div class="sections">
            <Disclosure summary={t('overlayEditor.ui.settings')} size="sm" bind:open={() => isOpen('settings'), (open) => toggle('settings', open)}>
              <Stack gap={3}>
                {#if !editable}
                  <Text size="xs" tone="muted">{t('overlayEditor.ui.importedWidget')}</Text>
                {/if}
                {#each fields as field (field.key)}
                  <SettingControl {field} value={settings[field.key]} {binding} update={(next) => change(field.key, next)} />
                {/each}
                {#if rules}
                  <AlertRules kind={rules} {settings} update={setSettings} onadvanced={(event) => (advanced = event)} />
                {/if}
                {#if group}
                  <Disclosure summary={groupTitle} size="sm">
                    <Stack gap={3}>
                      {#each group.fields as field (field.key)}
                        <SettingControl {field} value={groupValues[field.key]} update={(next) => change(group.key, { ...groupValues, [field.key]: next })} />
                      {/each}
                    </Stack>
                  </Disclosure>
                {/if}
              </Stack>
            </Disclosure>
            <Disclosure summary={t('overlayEditor.ui.layoutSection')} size="sm" bind:open={() => isOpen('layout'), (open) => toggle('layout', open)}>
              <GeometryFields widget={shown} canvas={doc} {update} />
            </Disclosure>
            {#if font}
              <Disclosure summary={t('overlayEditor.ui.textSettings')} size="sm" bind:open={() => isOpen('text'), (open) => toggle('text', open)}>
                <Stack gap={3}>
                  {#each typography as field (field.key)}
                    <SettingControl {field} value={font[field.key]} update={(next) => change('style', { ...font, [field.key]: next })} />
                  {/each}
                </Stack>
              </Disclosure>
            {/if}
          </div>
        {/if}
      </Reveal>
    {/if}
  </div>
</aside>

<style>
  .panel { display: flex; flex-direction: column; height: 100%; min-height: 0; }
  .scroll { flex: 1; min-height: 0; overflow-x: hidden; overflow-y: auto; padding: 14px 14px 18px; display: flex; flex-direction: column; gap: 8px; }
  .sections { display: flex; flex-direction: column; gap: 8px; }
  .welcome { display: flex; flex-direction: column; gap: 10px; padding-bottom: 14px; }
  .welcome-title { display: block; margin: 0; font-family: var(--bb-font-body); font-size: 15px; font-weight: 600; color: var(--bb-white); }
  @media (min-width: 1280px) {
    .sheet-only { display: none; }
  }
</style>
