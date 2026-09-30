<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import { getUiI18n } from './i18n';
  import '../styles/elements/skip-link.css';

  const i18n = getUiI18n();
  type Own = {
    href: string;
    label?: string;
    class?: string;
  };

  let {
    href,
    label = i18n.t('nav.skip'),
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['a'], keyof Own> = $props();

  const classes = $derived(['bb-skip-link', className || null].filter(Boolean).join(' '));
</script>

<a class={classes} {href} {...rest}>{label}</a>
