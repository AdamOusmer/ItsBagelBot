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
    pending?: boolean;
    type?: 'button' | 'submit';
    onchange?: (v: boolean) => void;
    class?: string;
  };

  let {
    checked = $bindable(false),
    label,
    describedby,
    disabled = false,
    pending = false,
    type = 'button',
    onchange,
    class: className,
    ...rest
  }: Own & Omit<SvelteHTMLElements['button'], keyof Own> = $props();

  function flip() {
    if (disabled || pending) return;
    if (type === 'submit') return;
    checked = !checked;
    onchange?.(checked);
  }
</script>

<button
  {type}
  class={['bb-switch', className]}
  role="switch"
  aria-checked={checked ? 'true' : 'false'}
  aria-label={label}
  aria-describedby={describedby}
  aria-busy={pending ? 'true' : undefined}
  disabled={disabled || pending}
  data-state={checked ? 'on' : 'off'}
  data-pending={pending ? '' : undefined}
  onclick={flip}
  {...rest}
></button>
