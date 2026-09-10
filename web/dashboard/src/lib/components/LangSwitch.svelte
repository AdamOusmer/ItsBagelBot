<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Compact EN/FR toggle. Posts to /lang (plain form, no fetch) with the current
  // path as `next` so the switch keeps you on the same page in the new language.
  //
  // Wears @bagel/ui's `.bb-lang-switch` on the `.bb-tabs` rail, the same element
  // the public bar renders through LanguageSwitcher.svelte. It used to draw its
  // own 999px track with a tan-filled knob under `.lang` / `.lang-opt` — the
  // pill the rail replaced, still alive in the console because nothing shared
  // it. What stays here is the half the library cannot have: this is a FORM,
  // not a row of links, because it also writes the choice to the account, and
  // `.bb-tab` is already a bare `<button>` reset so the contract fits one.
  import { onMount } from 'svelte';
  import { page } from '$app/state';
  import { getI18n, LOCALES, type Locale } from '@bagel/kit';
  import { ensureCatalog, localeName } from '@bagel/kit/i18n';
  // Side-effect imports: this is the one caller that writes the contract's
  // classes itself (it is a form, see above) rather than through an adapter,
  // so nothing else pulls the two stylesheets in on its behalf.
  import '@bagel/ui/styles/elements/nav.css';
  import '@bagel/ui/styles/tags.css';

  let { selected }: { selected?: Locale } = $props();
  const i18n = getI18n();
  const active = $derived(selected ?? i18n.locale);
  const next = $derived(page.url.pathname + page.url.search);
  const short = (l: Locale) => l.toUpperCase();

  // Tooltips show each locale's self-name, which lives in its catalog. Catalogs
  // load lazily now (boot no longer evaluates all of them), so pull them in here
  // where the names are actually displayed; until one lands the title falls
  // back to the bare code.
  let named = $state(false);
  onMount(async () => {
    await Promise.all(LOCALES.map((l) => ensureCatalog(l)));
    named = true;
  });
</script>

<form
  method="POST"
  action="/lang"
  class="bb-lang-switch bb-tabs"
  aria-label={i18n.t('lang.switchAria')}
>
  <input type="hidden" name="next" value={next} />
  {#each LOCALES as l (l)}
    <button
      type="submit"
      name="to"
      value={l}
      class="bb-lang-switch__opt bb-tab"
      class:is-active={l === active}
      aria-pressed={l === active}
      title={named ? localeName(l) : l}
    >{short(l)}</button>
  {/each}
</form>
