<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/footer.css';
  import Brand from './Brand.svelte';
  import LightField from './LightField.svelte';
  import NavLink from './NavLink.svelte';
  import type { Snippet } from 'svelte';
  import { FOOTER_RING, stackColumns } from '../lib/footer-layout';
  import type { UiBrand, UiFooterColumn, UiNavLink } from '../lib/nav-types';

  type Own = {
    brand: UiBrand;
    signoff?: { line: string; sub?: string };
    columns?: UiFooterColumn[];
    legal?: UiNavLink[];
    copyright: string;
    note?: string;
    colophon?: Snippet;
    class?: string;
  };

  let {
    brand,
    signoff,
    columns = [],
    legal = [],
    copyright,
    note,
    colophon,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['footer'], keyof Own> = $props();

  const classes = $derived(['bb-footer', className || null].filter(Boolean).join(' '));
  const stacks = $derived(stackColumns(columns));
</script>

<footer class={classes} {...rest}
  ><div class="bb-footer__ring" aria-hidden="true">{@html FOOTER_RING}</div><LightField />{#if signoff}<div
      class="bb-footer__signoff"
      data-reveal
      ><p class="bb-footer__signoff-line"><span data-fit="block">{signoff.line}</span></p>{#if signoff.sub}<p
          class="bb-footer__signoff-sub"><span data-fit>{signoff.sub}</span></p
        >{/if}</div
    >{/if}<Brand
    class="bb-footer__brand"
    title={brand.title}
    sub={brand.sub}
    href={brand.href}
    logoSrc={brand.logoSrc}
    logoAlt={brand.logoAlt}
    size="lg"
    wordmark
    fit
    data-reveal
  /><div class="bb-footer__cols"
    >{#each stacks as stack, i (stack[0].title)}<div
        class="bb-footer__col"
        data-reveal
        style="--reveal-i: {i + 1}"
        >{#each stack as column (column.title)}<div class="bb-footer__group"
            ><p class="bb-footer__col-title"><span data-fit data-fit-group="bb-footer-titles">{column.title}</span></p>{#each column.links as link (link.href)}<NavLink
                href={link.href}
                label={link.label}
                current={link.current}
                external={link.external}
                fit
                fitGroup="bb-footer-links-{i}"
              />{/each}</div
          >{/each}</div
      >{/each}</div
  ><div class="bb-footer__bottom"
    ><div class="bb-footer__legal"
      >{#each legal as link (link.href)}<NavLink
          href={link.href}
          label={link.label}
          current={link.current}
          external={link.external}
          fit
          fitGroup="bb-footer-legal"
        />{/each}</div
    ><p class="bb-footer__fine"
      ><span data-fit="block"><span>{copyright}</span>{#if note}<span class="bb-footer__sep" aria-hidden="true">·</span
        ><span>{note}</span>{/if}</span
      ></p
    >{#if colophon}<div class="bb-footer__colophon">{@render colophon()}</div>{/if}</div
  ></footer
>
