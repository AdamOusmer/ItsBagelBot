<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/field.css';
  import '../styles/elements/input.css';
  import type { Snippet } from 'svelte';

  let {
    value = $bindable(''),
    invalid = false,
    fill = false,
    class: className = '',
    children,
    ...rest
  }: {
    value?: string;
    invalid?: boolean;
    fill?: boolean;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-input', 'bb-input--select', fill ? 'bb-input--fill' : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<span class={classes} data-invalid={invalid ? '' : undefined}><select bind:value {...rest}
    >{@render children()}</select
  ><svg class="bb-input__chevron" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6"
    ></path></svg
  ></span>
