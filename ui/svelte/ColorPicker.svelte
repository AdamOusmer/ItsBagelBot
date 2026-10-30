<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLButtonAttributes } from 'svelte/elements';
  import { onMount } from 'svelte';
  import PickerPanel from './PickerPanel.svelte';
  import { getUiI18n } from './i18n';
  import { hexToHsv, hsvToHex, parseHex, type Hsv } from '../lib/color';
  import '../styles/elements/color-picker.css';

  type Own = {
    value?: string;
    swatches?: readonly string[];
    label?: string;
    id?: string;
    name?: string;
    form?: string;
    disabled?: boolean;
    invalid?: boolean;
    class?: string;
    onValueChange?: (value: string) => void;
  };

  const i18n = getUiI18n();
  let {
    value = $bindable('#000000'), swatches = [], label, id, name, form,
    disabled = false, invalid = false, class: className = '', onValueChange, ...rest
  }: Own & Omit<HTMLButtonAttributes, keyof Own> = $props();

  const uid = $props.id();
  const controlId = $derived(id || uid);
  const hex = $derived(parseHex(value) ?? '#000000');
  const swatchList = $derived(swatches.map((swatch) => parseHex(swatch)).filter((swatch) => swatch !== null));

  let open = $state(false);
  let hsv = $state<Hsv>({ h: 0, s: 0, v: 0 });
  let hexText = $state('');
  let editing = $state(false);
  let btnEl = $state<HTMLButtonElement>();
  let labelledBy = $state<string>();

  $effect.pre(() => {
    if (hsvToHex(hsv) !== hex) hsv = hexToHsv(hex);
  });
  $effect.pre(() => {
    if (!editing) hexText = hex.slice(1).toUpperCase();
  });
  $effect(() => { if (disabled) open = false; });

  onMount(() => {
    // Reuse Field's visible label and keep the current hex in the name.
    const labels = Array.from(btnEl?.labels || []).map((field, i) => {
      const text = field.querySelector<HTMLElement>('.bb-field__label') || field;
      text.id ||= `${controlId}-label-${i}`;
      return text.id;
    });
    if (labels.length) labelledBy = [...labels, `${controlId}-value`].join(' ');
  });

  function commit(next: Hsv) {
    hsv = next;
    const out = hsvToHex(next);
    if (out === value) return;
    value = out;
    onValueChange?.(out);
  }

  const clamp = (n: number) => Math.min(1, Math.max(0, n));

  function fromPointer(area: 'plane' | 'hue', event: PointerEvent) {
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
    const x = clamp((event.clientX - rect.left) / rect.width);
    const y = clamp((event.clientY - rect.top) / rect.height);
    commit(area === 'plane' ? { ...hsv, s: x, v: 1 - y } : { ...hsv, h: x * 359.9 });
  }

  function press(area: 'plane' | 'hue') {
    return (event: PointerEvent) => {
      const target = event.currentTarget as HTMLElement;
      target.setPointerCapture(event.pointerId);
      fromPointer(area, event);
      target.querySelector<HTMLElement>('[role="slider"]')?.focus({ preventScroll: true });
    };
  }

  function drag(area: 'plane' | 'hue') {
    return (event: PointerEvent) => {
      if ((event.currentTarget as HTMLElement).hasPointerCapture(event.pointerId)) fromPointer(area, event);
    };
  }

  const ARROWS: Record<string, [number, number]> = {
    ArrowRight: [1, 0], ArrowLeft: [-1, 0], ArrowUp: [0, 1], ArrowDown: [0, -1],
  };

  function nudge(area: 'plane' | 'hue') {
    return (event: KeyboardEvent) => {
      const arrow = ARROWS[event.key];
      if (!arrow) return;
      event.preventDefault();
      const step = event.shiftKey ? 0.1 : 0.01;
      const [dx, dy] = arrow;
      if (area === 'plane') commit({ ...hsv, s: clamp(hsv.s + dx * step), v: clamp(hsv.v + dy * step) });
      else commit({ ...hsv, h: (hsv.h + (dx + dy) * step * 360 + 360) % 360 });
    };
  }

  function typed(event: Event & { currentTarget: HTMLInputElement }) {
    hexText = event.currentTarget.value;
    const next = parseHex(hexText);
    if (next) commit(hexToHsv(next));
  }

  const percent = (n: number) => `${Math.round(n * 100)}%`;
  const classes = $derived(['bb-color', className].filter(Boolean).join(' '));
</script>

{#if name}<input type="hidden" {name} {form} value={hex} />{/if}
<button bind:this={btnEl} id={controlId} type="button" class={classes} {disabled}
  data-invalid={invalid ? '' : undefined}
  aria-label={label} aria-labelledby={label ? undefined : labelledBy}
  style:--cp-color={hex}
  {...rest}
  onclick={() => (open = !open)}>
  <span class="bb-color__dot" aria-hidden="true"></span>
  <span class="bb-color__hex" id="{controlId}-value">{hex.toUpperCase()}</span>
  <svg class="bb-color__chevron" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6"></path></svg>
</button>

<PickerPanel bind:open anchor={btnEl} label={label || i18n.t('color.label')} placement="below" width={260} maxHeight={420}>
  <div class="bb-color-panel" style:--cp-color={hex} style:--cp-hue="{hsv.h}deg">
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="bb-color-panel__plane" onpointerdown={press('plane')} onpointermove={drag('plane')}>
      <span class="bb-color-panel__knob" role="slider" tabindex="0" aria-label={i18n.t('color.plane')}
        aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(hsv.s * 100)}
        aria-valuetext={i18n.t('color.planeValue', { saturation: percent(hsv.s), brightness: percent(hsv.v) })}
        style:left={percent(hsv.s)} style:top={percent(1 - hsv.v)} onkeydown={nudge('plane')}></span>
    </div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="bb-color-panel__rail" onpointerdown={press('hue')} onpointermove={drag('hue')}>
      <span class="bb-color-panel__thumb" role="slider" tabindex="0" aria-label={i18n.t('color.hue')}
        aria-valuemin={0} aria-valuemax={360} aria-valuenow={Math.round(hsv.h)}
        style:left="{(hsv.h / 360) * 100}%" onkeydown={nudge('hue')}></span>
    </div>
    <div class="bb-color-panel__row">
      <span class="bb-color-panel__preview" aria-hidden="true"></span>
      <label class="bb-color-panel__field">
        <span aria-hidden="true">#</span>
        <input value={hexText} maxlength={7} spellcheck="false" autocomplete="off" aria-label={i18n.t('color.hex')}
          oninput={typed} onfocus={() => (editing = true)}
          onblur={() => { editing = false; hexText = hex.slice(1).toUpperCase(); }}
          onkeydown={(event) => { if (event.key === 'Enter') { event.preventDefault(); open = false; } }} />
      </label>
    </div>
    {#if swatchList.length}
      <div class="bb-color-panel__swatches">
        {#each swatchList as swatch (swatch)}
          <button type="button" class="bb-color-panel__swatch" style:--sw={swatch}
            aria-label={i18n.t('color.swatch', { hex: swatch.toUpperCase() })} aria-pressed={swatch === hex}
            onclick={() => commit(hexToHsv(swatch))}></button>
        {/each}
      </div>
    {/if}
  </div>
</PickerPanel>
