// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { SITE } from '@bagel/kit/site-links';
import { classifyVia, VIA_PARAM } from '@bagel/kit/via';

// Held in memory only: storing it in a cookie or browser storage would need a consent banner.
const via = classifyVia(document.referrer, new URLSearchParams(location.search).get('utm_source'));
const LOGIN = `${SITE.dashboard}/auth/login`;

function tag(): void {
  for (const link of document.querySelectorAll<HTMLAnchorElement>(`a[href^="${LOGIN}"]`)) {
    const url = new URL(link.href);
    url.searchParams.set(VIA_PARAM, via);
    link.href = url.toString();
  }
}

document.addEventListener('astro:page-load', tag);
