<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  let {
    checked = $bindable(false),
    label,
    describedby,
    disabled = false,
    pending = false,
    type = 'button',
    onchange,
    ...rest
  }: {
    checked?: boolean;
    label: string;
    describedby?: string;
    disabled?: boolean;
    pending?: boolean;
    type?: 'button' | 'submit';
    onchange?: (v: boolean) => void;
    [key: string]: unknown;
  } = $props();

  function flip() {
    if (disabled || pending) return;
    if (type === 'submit') return;
    checked = !checked;
    onchange?.(checked);
  }
</script>

<button
  {type}
  class="bb-switch"
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
