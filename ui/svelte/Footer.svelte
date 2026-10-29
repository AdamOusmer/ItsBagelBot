<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '../styles/elements/footer.css';
  import Brand from './Brand.svelte';
  import LightField from './LightField.svelte';
  import NavLink from './NavLink.svelte';
  import type { Snippet } from 'svelte';
  import type { UiBrand, UiFooterColumn, UiNavLink } from '../lib/nav-types';

  type Props = {
    brand: UiBrand;
    signoff?: { line: string; sub?: string };
    columns?: UiFooterColumn[];
    legal?: UiNavLink[];
    copyright: string;
    note?: string;
    studio?: string;
    colophon?: Snippet;
  };

  let { brand, signoff, columns = [], legal = [], copyright, note, studio, colophon }: Props = $props();
</script>

<footer class="bb-footer"
  ><LightField />{#if signoff}<div
      class="bb-footer__signoff"
      ><p class="bb-footer__signoff-line"><span data-fit="block">{signoff.line}</span></p>{#if signoff.sub}<p
          class="bb-footer__signoff-sub"><span data-fit>{signoff.sub}</span></p
        >{/if}</div
    >{/if}<div class="bb-footer__cols"
    >{#each columns as column, i (column.title)}<div
        class="bb-footer__col"
        ><div class="bb-footer__group"
          ><p class="bb-footer__col-title"><span data-fit data-fit-group="bb-footer-titles">{column.title}</span></p>{#each column.links as link (link.href)}<NavLink
              href={link.href}
              label={link.label}
              current={link.current}
              external={link.external}
              fit
              fitGroup="bb-footer-links-{i}"
            />{/each}</div
        ></div
      >{/each}</div
  ><div class="bb-footer__mark"
    >{#if studio}<p class="bb-footer__studio"><span data-fit>{studio}</span></p>{/if}<Brand
      class="bb-footer__brand"
      title={brand.title}
      sub={brand.sub}
      href={brand.href}
      logoSrc={brand.logoSrc}
      logoAlt={brand.logoAlt}
      size="lg"
      wordmark
      fit
    /></div
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
