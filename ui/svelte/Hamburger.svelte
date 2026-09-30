<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import { getUiI18n } from './i18n';
  import '../styles/elements/nav.css';

  const i18n = getUiI18n();
  type Own = {
    label?: string;
    closeLabel?: string;
    controls?: string;
    class?: string;
  };

  let {
    label = i18n.t('nav.menuOpen'),
    closeLabel = i18n.t('nav.menuClose'),
    controls = 'bb-mobile-menu',
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['button'], keyof Own> = $props();

  const classes = $derived(['bb-hamburger', className || null].filter(Boolean).join(' '));
</script>

<button
  class={classes}
  type="button"
  data-menu-toggle=""
  aria-label={label}
  aria-expanded="false"
  aria-controls={controls}
  data-label-close={closeLabel}
  {...rest}
  ><span class="bb-hamburger__bar"></span><span class="bb-hamburger__bar"></span><span
    class="bb-hamburger__bar"
  ></span></button
>
