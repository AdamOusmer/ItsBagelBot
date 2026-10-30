<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import '../styles/elements/footer.css';
  import Brand from './Brand.svelte';
  import NavLink from './NavLink.svelte';
  import { FOOTER_CREW, mountFooterCrew } from '../lib/footer-crew';
  import type { Snippet } from 'svelte';
  import type { UiBrand, UiFooterColumn, UiNavLink } from '../lib/nav-types';

  type Props = {
    brand: UiBrand;
    columns?: UiFooterColumn[];
    legal?: UiNavLink[];
    copyright: string;
    note?: string;
    studio?: string;
    colophon?: Snippet;
  };

  let { brand, columns = [], legal = [], copyright, note, studio, colophon }: Props = $props();

  let footer: HTMLElement;

  onMount(() => mountFooterCrew(footer));
</script>

<footer class="bb-footer" bind:this={footer}
  ><div class="bb-footer__sky" aria-hidden="true"
    ><span class="bb-footer__grain"></span><span class="bb-footer__aurora"
      ><span class="bb-footer__band"></span><span class="bb-footer__band"></span><span class="bb-footer__band"></span></span
    ></div
  ><div class="bb-footer__top"
    ><div class="bb-footer__id">{#if studio}<p class="bb-footer__studio"><span data-fit>{studio}</span></p>{/if}</div
    ><div class="bb-footer__cols"
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
    ></div
  ><div class="bb-footer__mark"
    ><Brand
      class="bb-footer__brand"
      title={brand.title}
      logoSrc={brand.logoSrc}
      logoAlt={brand.logoAlt}
      size="lg"
      wordmark
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
  ><div class="bb-footer__crew" aria-hidden="true"
    >{#each FOOTER_CREW as seat (seat.name)}<span
        class="bb-footer__peeker"
        data-crew={seat.name}
        data-head={seat.head}
        style="--seat: {seat.size}"><svg viewBox="0 0 100 100"></svg></span
      >{/each}</div
  ></footer
>
