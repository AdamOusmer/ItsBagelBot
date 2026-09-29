<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/field.css';
  import '../styles/elements/input.css';

  type Own = {
    value?: string;
    rows?: number;
    invalid?: boolean;
    fill?: boolean;
    mono?: boolean;
    class?: string;
  };

  let {
    value = $bindable(''),
    rows = 3,
    invalid = false,
    fill = false,
    mono = false,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['textarea'], keyof Own> = $props();

  const classes = $derived(
    ['bb-input', 'bb-input--area', fill ? 'bb-input--fill' : null, mono ? 'bb-input--mono' : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<span class={classes} data-invalid={invalid ? '' : undefined}><textarea {rows} bind:value {...rest}
  ></textarea></span>
