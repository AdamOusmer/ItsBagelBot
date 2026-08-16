<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Signed-out surface: the marketing site's chrome around a dashboard-rendered
  // page. Deliberately NO robots noindex (unlike (app)) — these pages are meant
  // to be found. The nav's off-site links follow the visitor's locale; the one
  // local entry (/stats) lights up when you are on it.
  import { page } from '$app/state';
  import { getI18n } from '@bagel/shared';
  import PublicNav from '$lib/components/public/PublicNav.svelte';
  import PublicFooter from '$lib/components/public/PublicFooter.svelte';
  import { webHref } from '$lib/components/public/links';

  let { children } = $props();

  const { t, locale } = getI18n();

  const links = $derived([
    { href: webHref('/pricing', locale), label: t('public.nav.pricing') },
    { href: webHref('/guides', locale), label: t('public.nav.guides') },
    { href: webHref('/contact', locale), label: t('public.nav.contact') },
    { href: '/stats', label: t('public.nav.stats'), active: page.url.pathname === '/stats' }
  ]);
</script>

<PublicNav {links} showLang />
{@render children()}
<PublicFooter />
