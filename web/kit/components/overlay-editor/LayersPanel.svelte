<script module lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { OverlayWidget } from '../../lib/overlay-document';
  import type { EditableKind } from '../../lib/overlay-editor/defaults';

  export type LayerActions = {
    select: (id: string) => void;
    add: (kind: EditableKind) => void;
    patch: (id: string, change: Partial<OverlayWidget>) => void;
    reorder: (id: string, offset: -1 | 1) => void;
    duplicate: (id: string) => void;
    remove: (id: string) => void;
  };
</script>

<script lang="ts">
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import SwitchRow from '@bagel/ui/svelte/SwitchRow.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '../../lib/i18n/context';
  import type { OverlayDocument } from '../../lib/overlay-document';
  import { EDITABLE_KINDS } from '../../lib/overlay-editor/defaults';
  import { widgetLimitReached } from '../../lib/overlay-editor/document';
  import Fit from './Fit.svelte';

  let { doc, selected, actions }: { doc: OverlayDocument; selected: string; actions: LayerActions } = $props();

  const { t } = getI18n();
  let kind = $state<EditableKind>('text');

  const options = EDITABLE_KINDS.map((value) => ({ value, label: t(`overlayEditor.kind.${value}`) }));
  const layers = $derived(doc.widgets.toReversed());
  const chosen = $derived(doc.widgets.find((widget) => widget.id === selected));
  const full = $derived(widgetLimitReached(doc));

  function rename(name: string): void {
    const trimmed = name.trim();
    if (chosen && trimmed) actions.patch(chosen.id, { name: trimmed });
  }
</script>

<Card>
  <CardHead title={t('overlayEditor.ui.layers')} headingLevel={2} />
  <Stack gap={3}>
    <Stack gap={1} role="group" aria-label={t('overlayEditor.ui.layers')}>
      {#each layers as layer (layer.id)}
        <Button size="sm" block variant={selected === layer.id ? 'primary' : 'secondary'} aria-pressed={selected === layer.id} onclick={() => actions.select(layer.id)}>
          <Fit>{layer.name}</Fit>
          {#if layer.hidden || layer.locked}<Fit>{layer.hidden ? t('overlayEditor.ui.hidden') : t('overlayEditor.ui.locked')}</Fit>{/if}
        </Button>
      {:else}
        <Text size="sm" tone="muted">{t('overlayEditor.ui.noLayers')}</Text>
      {/each}
    </Stack>
    <Field label={t('overlayEditor.ui.widgetType')} fit>
      <Select label={t('overlayEditor.ui.widgetType')} {options} value={kind} fill onchange={(event) => (kind = event.currentTarget.value as EditableKind)} />
    </Field>
    <Button size="sm" variant="secondary" disabled={full} onclick={() => actions.add(kind)}>
      <Fit>{t('overlayEditor.ui.addWidget')}</Fit>
    </Button>
    <Stack gap={3} aria-hidden={!chosen} inert={!chosen}>
      <Field label={t('overlayEditor.ui.layerName')} fit>
        <Input value={chosen?.name ?? ''} disabled={!chosen} maxlength={120} fill onchange={(event) => rename(event.currentTarget.value)} />
      </Field>
      <SwitchRow label={t('overlayEditor.ui.hidden')} fitCopy checked={chosen?.hidden ?? false} disabled={!chosen} onCheckedChange={(hidden) => chosen && actions.patch(chosen.id, { hidden })} />
      <SwitchRow label={t('overlayEditor.ui.locked')} fitCopy checked={chosen?.locked ?? false} disabled={!chosen} onCheckedChange={(locked) => chosen && actions.patch(chosen.id, { locked })} />
      <Cluster>
        <Button size="sm" variant="secondary" disabled={!chosen || layers[0]?.id === chosen.id} onclick={() => chosen && actions.reorder(chosen.id, 1)}>
          <Fit>{t('overlayEditor.ui.raise')}</Fit>
        </Button>
        <Button size="sm" variant="secondary" disabled={!chosen || layers.at(-1)?.id === chosen.id} onclick={() => chosen && actions.reorder(chosen.id, -1)}>
          <Fit>{t('overlayEditor.ui.lower')}</Fit>
        </Button>
        <Button size="sm" variant="secondary" disabled={!chosen || full} onclick={() => chosen && actions.duplicate(chosen.id)}>
          <Fit>{t('overlayEditor.ui.duplicate')}</Fit>
        </Button>
        <Button size="sm" variant="secondary" disabled={!chosen} onclick={() => chosen && actions.remove(chosen.id)}>
          <Fit>{t('common.delete')}</Fit>
        </Button>
      </Cluster>
    </Stack>
  </Stack>
</Card>
