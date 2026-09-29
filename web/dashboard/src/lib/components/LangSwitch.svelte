<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import { page } from '$app/state';
  import { getI18n, LOCALES, type Locale } from '@bagel/kit';
  import { ensureCatalog, localeName } from '@bagel/kit/i18n';
  import LanguageSwitcher from '@bagel/ui/svelte/LanguageSwitcher.svelte';

  let { selected }: { selected?: Locale } = $props();
  const i18n = getI18n();
  const active = $derived(selected ?? i18n.locale);
  const next = $derived(page.url.pathname + page.url.search);

  let named = $state(false);
  onMount(async () => {
    await Promise.all(LOCALES.map((l) => ensureCatalog(l)));
    named = true;
  });

  const options = $derived(
    LOCALES.map((l) => ({
      code: l,
      label: l.toUpperCase(),
      current: l === active,
      title: named ? localeName(l) : l
    }))
  );
</script>

<LanguageSwitcher action="/lang" name="to" fields={{ next }} {options} ariaLabel={i18n.t('lang.switchAria')} />
