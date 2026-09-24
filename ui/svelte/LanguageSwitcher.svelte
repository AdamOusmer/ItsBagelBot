<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/nav.css';
  import type { UiLocaleOption } from '../lib/nav-types';

  let {
    options,
    ariaLabel,
    class: className = '',
    ...rest
  }: {
    options: UiLocaleOption[];
    ariaLabel: string;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-lang-switch', className || null].filter(Boolean).join(' '),
  );
</script>

<div class={classes} role="group" aria-label={ariaLabel} {...rest}
  >{#each options as option (option.code)}<a
      class="bb-lang-switch__opt {option.current ? 'is-active' : ''}"
      href={option.href}
      hreflang={option.code}
      aria-current={option.current ? 'true' : undefined}>{option.label}</a
    >{/each}</div
>
