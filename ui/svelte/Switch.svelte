<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/toggle.css';

  type Own = {
    checked?: boolean;
    label: string;
    describedby?: string;
    disabled?: boolean;
    busy?: boolean;
    type?: 'button' | 'submit';
    onCheckedChange?: (checked: boolean) => void;
    class?: string;
  };

  let {
    checked = $bindable(false),
    label,
    describedby,
    disabled = false,
    busy = false,
    type = 'button',
    onCheckedChange,
    class: className,
    ...rest
  }: Own & Omit<SvelteHTMLElements['button'], keyof Own> = $props();

  function flip() {
    if (disabled || busy) return;
    if (type === 'submit') return;
    checked = !checked;
    onCheckedChange?.(checked);
  }
</script>

<button
  {type}
  class={['bb-switch', className]}
  role="switch"
  aria-checked={checked ? 'true' : 'false'}
  aria-label={label}
  aria-describedby={describedby}
  aria-busy={busy ? 'true' : undefined}
  disabled={disabled || busy}
  data-state={checked ? 'on' : 'off'}
  data-busy={busy ? '' : undefined}
  onclick={flip}
  {...rest}
></button>
