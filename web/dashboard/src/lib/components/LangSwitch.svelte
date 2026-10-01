<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { tick } from 'svelte';
  import { invalidateAll } from '$app/navigation';
  import { page } from '$app/state';
  import { getI18n, LOCALES, ensureCatalog, isLocale, type Locale } from '@bagel/kit';
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

  // Switching in place keeps the scroll position; a native submit would reload from the top.
  async function switchInPlace(event: SubmitEvent) {
    const form = event.target as HTMLFormElement;
    const body = new FormData(form, event.submitter);
    const to = String(body.get('to') ?? '');
    if (!isLocale(to)) return;
    event.preventDefault();
    try {
      await ensureCatalog(to);
      await fetch(form.action, { method: 'POST', body, redirect: 'manual' });
    } catch {
      form.submit();
      return;
    }
    const top = window.scrollY;
    await invalidateAll();
    await tick();
    window.scrollTo({ top, behavior: 'instant' });
  }
</script>

<div onsubmit={switchInPlace} style="display: contents">
  <LanguageSwitcher action="/lang" name="to" fields={{ next }} {options} label={i18n.t('lang.switchAria')} variant="field" />
</div>
