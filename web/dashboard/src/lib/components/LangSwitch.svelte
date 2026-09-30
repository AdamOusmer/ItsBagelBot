<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { page } from '$app/state';
  import { getI18n, LOCALES, type Locale } from '@bagel/kit';
  import { localeFlag } from '@bagel/kit/site-links';
  import LanguageSwitcher from '@bagel/ui/svelte/LanguageSwitcher.svelte';

  let { selected }: { selected?: Locale } = $props();
  const i18n = getI18n();
  const active = $derived(selected ?? i18n.locale);
  const next = $derived(page.url.pathname + page.url.search);

  const options = $derived(
    LOCALES.map((code) => ({
      code,
      label: code.toUpperCase(),
      title: page.data.localeNames?.[code] ?? code,
      flag: localeFlag(code),
      current: code === active
    }))
  );
</script>

<LanguageSwitcher action="/lang" name="to" fields={{ next }} {options} label={i18n.t('lang.switchAria')} variant="field" />
