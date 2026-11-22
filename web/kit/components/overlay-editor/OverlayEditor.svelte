<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { untrack } from 'svelte';
  import type { Box } from '@bagel/ui/lib/box-transform';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import OverlayCanvas from '@bagel/ui/svelte/OverlayCanvas.svelte';
  import SaveStatus, { type SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '../../lib/i18n/context';
  import type { OverlayDocument, OverlayWidget } from '../../lib/overlay-document';
  import type { MediaBinding, SaveOutcome } from '../../lib/overlay-editor/binding';
  import { newWidget, type EditableKind } from '../../lib/overlay-editor/defaults';
  import { assetRef, toWireDocument, type AssetUrls } from '../../lib/overlay-editor/document';
  import type { MediaSlot } from '../../lib/overlay-editor/fields';
  import { acceptsType, describeUpload, MediaFailure, sha256Hex, slotMaxBytes, type MediaAdapter } from '../../lib/overlay-editor/media';
  import { addWidget, copyOf, removeWidget, reorderWidget, replaceWidget } from '../../lib/overlay-editor/ops';
  import { resizeCanvas, resolutions } from '../../lib/overlay-editor/resolutions';
  import { EditorSession } from '../../lib/overlay-editor/session';
  import Fit from './Fit.svelte';
  import Inspector from './Inspector.svelte';
  import LayersPanel, { type LayerActions } from './LayersPanel.svelte';
  import Notice from './Notice.svelte';
  import WidgetPreview from './WidgetPreview.svelte';

  let { initial, urls: initialUrls = {}, media, preview = false, onsave, ondirty }: {
    initial: OverlayDocument;
    urls?: AssetUrls;
    media?: MediaAdapter;
    preview?: boolean;
    onsave?: (doc: OverlayDocument) => Promise<SaveOutcome>;
    ondirty?: (dirty: boolean) => void;
  } = $props();

  const { t } = getI18n();
  const session = new EditorSession(untrack(() => initial));
  const ZOOMS = [0, 0.5, 1, 2];

  let doc = $state.raw(untrack(() => initial));
  let saved = $state.raw(untrack(() => initial));
  let urls = $state.raw<AssetUrls>(untrack(() => initialUrls));
  let selected = $state('');
  let zoom = $state(0);
  let busy = $state(false);
  let failure = $state('');
  let steps = $state({ undo: false, redo: false });

  const dirty = $derived(doc !== saved);
  const widget = $derived(doc.widgets.find((item) => item.id === selected));
  const resolution = $derived(`${doc.width}x${doc.height}`);
  const resolutionOptions = $derived(
    [...new Set([...resolutions.map((size) => `${size.width}x${size.height}`), resolution])].map((value) => ({
      value,
      label: value.replace('x', ' × ')
    }))
  );
  const zoomOptions = ZOOMS.map((value) => ({ value: String(value), label: value ? `${value * 100}%` : t('overlayEditor.ui.fit') }));
  const boxes = $derived(doc.widgets.map((item) => ({ ...item, label: item.name })));
  const handleLabels = {
    nw: t('overlayEditor.handle.nw'), ne: t('overlayEditor.handle.ne'),
    sw: t('overlayEditor.handle.sw'), se: t('overlayEditor.handle.se')
  };
  const saveState: SaveState = $derived(busy ? 'saving' : failure ? 'error' : dirty ? 'idle' : 'saved');
  const binding: MediaBinding | undefined = $derived(
    media && !preview ? { upload, assets: doc.assets } : undefined
  );

  $effect(() => ondirty?.(dirty));

  function sync(): void {
    steps = { undo: session.canUndo, redo: session.canRedo };
  }

  function apply(next: OverlayDocument): void {
    doc = next;
    session.commit(next);
    sync();
  }

  function step(direction: -1 | 1): void {
    const next = session.step(direction);
    if (next) doc = next;
    sync();
  }

  function patchWidget(id: string, change: Partial<OverlayWidget>): void {
    const target = doc.widgets.find((item) => item.id === id);
    if (target) apply(replaceWidget(doc, { ...target, ...change }));
  }

  const actions: LayerActions = {
    select: (id) => (selected = id),
    add(kind: EditableKind) {
      const created = newWidget(kind, doc, t(`overlayEditor.kind.${kind}`));
      selected = created.id;
      apply(addWidget(doc, created));
    },
    patch: patchWidget,
    reorder: (id, offset) => apply(reorderWidget(doc, id, offset)),
    duplicate(id) {
      const target = doc.widgets.find((item) => item.id === id);
      if (!target) return;
      const copy = copyOf(doc, target);
      selected = copy.id;
      apply(addWidget(doc, copy));
    },
    remove(id) {
      apply(removeWidget(doc, id));
      if (selected === id) selected = '';
    }
  };

  function moveBox(id: string, box: Box): void {
    const target = doc.widgets.find((item) => item.id === id);
    if (target) doc = replaceWidget(doc, { ...target, ...box });
  }

  async function upload(file: File, slot: MediaSlot): Promise<string> {
    if (!media) throw new MediaFailure('premium_required');
    if (file.size > slotMaxBytes(slot)) throw new MediaFailure('asset_too_large');
    const [uploaded, sha256] = await Promise.all([media.upload(file), sha256Hex(file)]);
    if (!acceptsType(slot, uploaded.type)) throw new MediaFailure('unsupported_media');
    urls = { ...urls, [uploaded.asset_id]: uploaded.url };
    apply({ ...doc, assets: { ...doc.assets, [uploaded.asset_id]: describeUpload(uploaded, sha256) } });
    return assetRef(uploaded.asset_id);
  }

  async function save(): Promise<void> {
    if (!onsave || busy || preview) return;
    const submitted = doc;
    busy = true;
    failure = '';
    try {
      const outcome = await onsave(toWireDocument(submitted));
      if (!outcome.ok) {
        failure = outcome.error;
        return;
      }
      const next = { ...submitted, rev: outcome.revision };
      session.markSaved(next);
      saved = next;
      doc = doc === submitted ? next : { ...doc, rev: outcome.revision };
    } finally {
      busy = false;
    }
  }

  function keyboard(event: KeyboardEvent): void {
    if ((event.target as HTMLElement | null)?.closest('input,textarea,[contenteditable]')) return;
    if (!(event.metaKey || event.ctrlKey) || event.key.toLowerCase() !== 'z') return;
    event.preventDefault();
    step(event.shiftKey ? 1 : -1);
  }
</script>

<svelte:window onkeydown={keyboard} />

{#snippet canvasPreview(box: { id: string }, scale: number)}
  {@const item = doc.widgets.find((candidate) => candidate.id === box.id)}
  {#if item}<WidgetPreview widget={item} {scale} {urls} />{/if}
{/snippet}

<Stack gap={4}>
  <Cluster gap={3} justify="between">
    <Field label={t('overlayEditor.ui.overlayName')} fit>
      <Input
        value={doc.name}
        maxlength={120}
        onchange={(event) => event.currentTarget.value.trim() && apply({ ...doc, name: event.currentTarget.value.trim() })}
      />
    </Field>
    <Field label={t('overlayEditor.ui.canvas')} fit>
      <Select
        label={t('overlayEditor.ui.canvas')}
        options={resolutionOptions}
        value={resolution}
        onchange={(event) => {
          const [width, height] = event.currentTarget.value.split('x').map(Number);
          apply(resizeCanvas(doc, { width: width ?? doc.width, height: height ?? doc.height }));
        }}
      />
    </Field>
    <Field label={t('overlayEditor.ui.zoom')} fit>
      <Select label={t('overlayEditor.ui.zoom')} options={zoomOptions} value={String(zoom)} onchange={(event) => (zoom = Number(event.currentTarget.value))} />
    </Field>
    <Button size="sm" variant="secondary" disabled={!steps.undo} onclick={() => step(-1)}><Fit>{t('overlayEditor.ui.undo')}</Fit></Button>
    <Button size="sm" variant="secondary" disabled={!steps.redo} onclick={() => step(1)}><Fit>{t('overlayEditor.ui.redo')}</Fit></Button>
    <SaveStatus state={saveState} />
    <Button size="sm" busy={busy} disabled={!dirty || preview || !onsave} onclick={save}><Fit>{t('common.save')}</Fit></Button>
  </Cluster>
  <Notice visible={!!failure} tone="danger" role="alert">{failure}</Notice>
  <OverlayCanvas
    width={doc.width}
    height={doc.height}
    {boxes}
    {selected}
    {zoom}
    {handleLabels}
    onselect={(id) => (selected = id)}
    onchange={moveBox}
    oncommit={() => apply(doc)}
    oncancel={() => (doc = session.revert())}
    preview={canvasPreview}
  />
  <Grid min="18rem" gap={4}>
    <LayersPanel {doc} {selected} {actions} />
    {#if widget}
      {#key widget.id}
        <Inspector {widget} canvas={doc} {binding} update={(next) => apply(replaceWidget(doc, next))} />
      {/key}
    {:else}
      <Text size="sm" tone="muted">{t('overlayEditor.ui.selectLayer')}</Text>
    {/if}
  </Grid>
</Stack>
