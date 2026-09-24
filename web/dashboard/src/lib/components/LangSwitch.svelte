<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import { page } from '$app/state';
  import { getI18n, LOCALES, type Locale } from '@bagel/kit';
  import { ensureCatalog, localeName } from '@bagel/kit/i18n';
  import '@bagel/ui/styles/elements/nav.css';

  let { selected }: { selected?: Locale } = $props();
  const i18n = getI18n();
  const active = $derived(selected ?? i18n.locale);
  const next = $derived(page.url.pathname + page.url.search);
  const short = (l: Locale) => l.toUpperCase();

  let named = $state(false);
  onMount(async () => {
    await Promise.all(LOCALES.map((l) => ensureCatalog(l)));
    named = true;
  });
</script>

<form
  method="POST"
  action="/lang"
  class="bb-lang-switch"
  aria-label={i18n.t('lang.switchAria')}
>
  <input type="hidden" name="next" value={next} />
  {#each LOCALES as l (l)}
    <button
      type="submit"
      name="to"
      value={l}
      class="bb-lang-switch__opt"
      class:is-active={l === active}
      aria-pressed={l === active}
      title={named ? localeName(l) : l}
    >{short(l)}</button>
  {/each}
</form>
