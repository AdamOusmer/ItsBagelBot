<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { tick } from 'svelte';
  import { goto } from '$app/navigation';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { DEFAULT_CANVAS, resolutions, templateNames, type Template } from '@bagel/kit/overlay-editor';
  import type { IconName } from '@bagel/ui/lib/icons';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import IconTile from '@bagel/ui/svelte/IconTile.svelte';
  import Modal from '@bagel/ui/svelte/Modal.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import SwapStack from '@bagel/ui/svelte/SwapStack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import ImportPanel from './ImportPanel.svelte';

  type Step = 'start' | 'import';
  type Choice = { key: string; icon: IconName; name: string; caption: string };

  let { open = $bindable(false), step = $bindable<Step>('start') }: {
    open?: boolean;
    step?: Step;
  } = $props();

  const { t } = getI18n();
  const EDITOR = '/overlays/custom';
  const BLANK = 'blank';
  const TEMPLATE_ICONS: Record<Template, IconName> = {
    alerts: 'bell',
    justChatting: 'users',
    startingSoon: 'clock'
  };
  const sizeKey = (canvas: { width: number; height: number }) => `${canvas.width}x${canvas.height}`;
  const sizeOptions = resolutions.map((canvas) => ({ value: sizeKey(canvas), label: `${canvas.width} x ${canvas.height}` }));

  let tiles = $state<HTMLDivElement>();
  let back = $state<HTMLDivElement>();
  let leaving = $state(false);
  let picked = $state(BLANK);
  let size = $state(sizeKey(DEFAULT_CANVAS));

  const startUrl = $derived(`${EDITOR}?template=${picked}&size=${size}`);
  const choices = $derived<Choice[]>([
    {
      key: BLANK,
      icon: 'plus',
      name: t('overlays.portal.blank'),
      caption: t('overlays.portal.blankCaption')
    },
    ...templateNames.map((name) => ({
      key: name,
      icon: TEMPLATE_ICONS[name],
      name: t(`overlayEditor.template.${name}`),
      caption: t(`overlays.portal.caption.${name}`)
    }))
  ]);

  async function focusStep(next: Step): Promise<void> {
    step = next;
    await tick();
    (next === 'import' ? back : tiles)?.querySelector('button')?.focus();
  }

  async function leave(url: string): Promise<void> {
    if (leaving) return;
    leaving = true;
    try {
      await goto(url);
      open = false;
    } finally {
      leaving = false;
    }
  }
</script>

<Modal bind:open class="bb-modal--wide" title={t('overlays.portal.title')} busy={leaving}>
  <div class="portal">
    <SwapStack alternate={step === 'import'} class="portal-stack">
      {#snippet primary()}
        <div class="step start">
          <div class="lead bb-fit-box">
            <Text as="p" size="sm" tone="muted"><bb-fit mode="block">{t('overlays.portal.lead')}</bb-fit></Text>
          </div>
          <div class="tiles" bind:this={tiles} data-lenis-prevent>
            {#each choices as choice (choice.key)}
              <button class="tile" style="--bb-cursor: quiet" type="button" aria-pressed={picked === choice.key} disabled={leaving} onclick={() => (picked = choice.key)}>
                <IconTile size="lg"><Icon name={choice.icon} size={20} /></IconTile>
                <span class="tick" aria-hidden="true"><Icon name="check" size={14} /></span>
                <span class="tile-name"><bb-fit><span data-fit="">{choice.name}</span></bb-fit></span>
                <span class="tile-caption bb-fit-box"><Text as="span" size="xs" tone="muted"><bb-fit mode="block">{choice.caption}</bb-fit></Text></span>
              </button>
            {/each}
            <button class="tile" style="--bb-cursor: quiet" type="button" disabled={leaving} onclick={() => focusStep('import')}>
              <IconTile size="lg"><Icon name="importFile" size={20} /></IconTile>
              <span class="tile-name"><bb-fit><span data-fit="">{t('overlays.portal.import')}</span></bb-fit></span>
              <span class="tile-caption bb-fit-box"><Text as="span" size="xs" tone="muted"><bb-fit mode="block">{t('overlays.portal.importCaption')}</bb-fit></Text></span>
            </button>
          </div>
          <div class="foot">
            <div class="size">
              <Field label={t('overlays.portal.size')} fitCopy>
                <Select label={t('overlays.portal.size')} options={sizeOptions} bind:value={size} disabled={leaving} fill />
              </Field>
            </div>
            <Button class="act" variant="secondary" type="button" disabled={leaving} onclick={() => (open = false)}>
              <bb-fit class="action-label"><span data-fit="">{t('common.cancel')}</span></bb-fit>
            </Button>
            <Button class="act" type="button" busy={leaving} disabled={leaving} onclick={() => leave(startUrl)}>
              <bb-fit class="action-label"><span data-fit="">{t('overlays.portal.start')}</span></bb-fit>
            </Button>
          </div>
        </div>
      {/snippet}
      {#snippet secondary()}
        <div class="step">
          <div class="back" bind:this={back}>
            <Button variant="ghost" size="sm" disabled={leaving} onclick={() => focusStep('start')}>
              <Icon name="arrowLeft" size={14} />
              <bb-fit class="back-label"><span data-fit="">{t('overlays.portal.back')}</span></bb-fit>
            </Button>
          </div>
          <ImportPanel onimported={(id) => leave(`${EDITOR}?id=${id}`)} />
        </div>
      {/snippet}
    </SwapStack>
  </div>
</Modal>

<style>
  .portal { inline-size: 100%; }
  .portal :global(.portal-stack) { block-size: 27rem; }
  .step {
    display: grid;
    grid-template-rows: auto minmax(0, 1fr);
    gap: var(--bb-space-3);
    block-size: 27rem;
    min-block-size: 0;
  }
  .step.start { grid-template-rows: auto minmax(0, 1fr) 4.25rem; }
  .lead { block-size: 2.5rem; min-inline-size: 0; align-content: center; }
  .tiles {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    grid-auto-rows: minmax(0, 1fr);
    gap: var(--bb-space-3);
    min-block-size: 0;
  }
  .tile {
    position: relative;
    display: grid;
    grid-template-rows: auto auto minmax(0, 1fr);
    justify-items: start;
    gap: var(--bb-space-2);
    min-inline-size: 0;
    padding: var(--bb-space-3);
    border: 1px solid var(--bb-glass-border);
    border-radius: var(--bb-radius-md);
    background: var(--bb-elev-2);
    color: var(--bb-white);
    font: inherit;
    text-align: start;
    cursor: pointer;
  }
  .tile:hover:not(:disabled), .tile:focus-visible, .tile[aria-pressed='true'] { background: var(--bb-bg-1); }
  .tile:focus-visible { outline: 2px solid var(--bb-green-glow); outline-offset: 2px; }
  .tile:disabled { opacity: 0.6; cursor: default; }
  .tick { position: absolute; inset-block-start: var(--bb-space-3); inset-inline-end: var(--bb-space-3); display: grid; opacity: 0; }
  .tile[aria-pressed='true'] .tick { opacity: 1; }
  .tile-name { display: block; inline-size: 100%; min-inline-size: 0; font-weight: 700; line-height: 1.4; }
  .tile-caption { display: block; inline-size: 100%; min-inline-size: 0; block-size: 2.5em; line-height: 1.25; }
  .foot {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto auto;
    align-items: end;
    gap: var(--bb-space-3);
  }
  .size { min-inline-size: 0; }
  .foot :global(.act) { inline-size: 7.5rem; --btn-justify: center; }
  .back { display: flex; }
  .back-label, .action-label { line-height: 1.4; }
  @media (max-width: 640px) {
    .tiles { grid-template-columns: repeat(2, minmax(0, 1fr)); grid-auto-rows: 8.75rem; overflow-y: auto; }
    .foot :global(.act) { inline-size: 4.5rem; }
  }
  @media (prefers-reduced-motion: no-preference) {
    .tile { transition: background-color var(--bb-dur-fast) ease, border-color var(--bb-dur-fast) ease, opacity var(--bb-dur-fast) ease; }
    .tick { transition: opacity var(--bb-dur-fast) ease; }
  }
</style>
