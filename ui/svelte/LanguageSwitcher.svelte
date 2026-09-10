<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-lang-switch`. Its Astro twin is
  // ../astro/LanguageSwitcher.astro; ../test/parity.test.ts diffs the two.
  //
  // Takes resolved options and nothing else. Every question this element used
  // to answer for itself -- which locales exist, whether the current route has
  // a translated counterpart, what a locale's path looks like -- is routing,
  // and it stays in the surface that owns the routes.
  import '../styles/elements/nav.css';
  import '../styles/tags.css';
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
    ['bb-lang-switch', 'bb-tabs', className || null].filter(Boolean).join(' '),
  );
</script>

<!-- role="group", not a tablist: these are links to other documents. A screen
     reader announcing "tab" here promises in-page panels that do not exist. -->
<div class={classes} role="group" aria-label={ariaLabel} {...rest}
  >{#each options as option (option.code)}<a
      class="bb-lang-switch__opt bb-tab {option.current ? 'is-active' : ''}"
      href={option.href}
      hreflang={option.code}
      aria-current={option.current ? 'true' : undefined}>{option.label}</a
    >{/each}</div
>
