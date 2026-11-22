<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { clampBox } from '@bagel/ui/lib/box-transform';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Disclosure from '@bagel/ui/svelte/Disclosure.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '../../lib/i18n/context';
  import type { OverlayWidget } from '../../lib/overlay-document';
  import type { MediaBinding } from '../../lib/overlay-editor/binding';
  import { completeSettings, isEditable, type Canvas, type Settings } from '../../lib/overlay-editor/defaults';
  import { creditHeaders, geometryFields, tierWeights, typography, widgetFields } from '../../lib/overlay-editor/fields';
  import AlertRules from './AlertRules.svelte';
  import SettingControl from './SettingControl.svelte';

  let { widget, canvas, update, binding }: {
    widget: OverlayWidget;
    canvas: Canvas;
    update: (widget: OverlayWidget) => void;
    binding?: MediaBinding;
  } = $props();

  const { t } = getI18n();
  const nested = {
    goal: { key: 'weights', fields: tierWeights },
    credits: { key: 'headers', fields: creditHeaders }
  } as const;

  const settings = $derived(completeSettings(widget.kind, widget.settings));
  const fields = $derived(widgetFields[widget.kind]);
  const editable = $derived(isEditable(widget.kind));
  const font = $derived(typeof settings.style === 'object' ? (settings.style as Settings) : undefined);
  const group = $derived(widget.kind === 'goal' || widget.kind === 'credits' ? nested[widget.kind] : undefined);
  const groupValues = $derived(group ? (settings[group.key] as Settings) : {});
  const groupTitle = $derived(widget.kind === 'goal' ? t('overlayEditor.ui.weightsTitle') : t('overlayEditor.ui.headingsTitle'));
  const hasRules = $derived(widget.kind === 'alertbox' || widget.kind === 'eventlist');

  const read = (source: object, key: string): unknown => (source as Settings)[key];
  const setSettings = (next: Settings): void => update({ ...widget, settings: next });

  function change(key: string, value: unknown): void {
    const next = { ...settings, [key]: value };
    if (widget.kind === 'timer' && key === 'mode' && value === 'countup') next.at = new Date().toISOString();
    setSettings(next);
  }

  function moveBox(key: string, value: unknown): void {
    const next = { ...widget, [key]: value };
    update({ ...next, ...clampBox(next, canvas) });
  }
</script>

<Card>
  <CardHead title={widget.name} headingLevel={2} />
  <Text size="xs" tone="muted">{t(`overlayEditor.kind.${widget.kind}`)}</Text>
  <Stack gap={3}>
    <Disclosure summary={t('overlayEditor.ui.geometry')} open size="sm">
      <Stack gap={3}>
        {#each geometryFields as field (field.key)}
          <SettingControl {field} value={read(widget, field.key)} disabled={widget.locked} update={(next) => moveBox(field.key, next)} />
        {/each}
      </Stack>
    </Disclosure>
    {#if !editable}
      <Text size="xs" tone="muted">{t('overlayEditor.ui.importedWidget')}</Text>
    {/if}
    {#each fields as field (field.key)}
      <SettingControl {field} value={settings[field.key]} {binding} update={(next) => change(field.key, next)} />
    {/each}
    {#if hasRules}
      <AlertRules kind={widget.kind === 'alertbox' ? 'alertbox' : 'eventlist'} {settings} update={setSettings} {binding} />
    {/if}
    {#if font}
      <Disclosure summary={t('overlayEditor.ui.typography')} size="sm">
        <Stack gap={3}>
          {#each typography as field (field.key)}
            <SettingControl {field} value={read(font, field.key)} update={(next) => change('style', { ...font, [field.key]: next })} />
          {/each}
        </Stack>
      </Disclosure>
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
</Card>
