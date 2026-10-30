// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  SITE_FOOTER,
  SITE_LEGAL,
  resolveSiteColumns,
  resolveSiteLinks,
  type SiteColumn,
  type SiteLink,
  type SiteLinkContext,
} from './site-links';

export interface SiteFooterProps {
  brand: { title: string; logoSrc: string; logoAlt: string };
  columns: SiteColumn[];
  legal: SiteLink[];
  copyright: string;
  note: string;
  studio: string;
}

export const SITE_COPYRIGHT = '© 2026 ItsBagelBot';

export function siteFooterProps(ctx: SiteLinkContext, logoSrc: string): SiteFooterProps {
  return {
    brand: { title: 'ItsBagelBot', logoSrc, logoAlt: ctx.label('logoAlt') },
    columns: resolveSiteColumns(SITE_FOOTER, ctx),
    legal: resolveSiteLinks(SITE_LEGAL, ctx),
    copyright: SITE_COPYRIGHT,
    note: ctx.label('note'),
    studio: ctx.label('studio'),
  };
}
