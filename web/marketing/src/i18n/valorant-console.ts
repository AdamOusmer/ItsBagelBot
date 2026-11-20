// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { flatCatalogs, type FlatTree } from '@bagel/kit/i18n/flat';
import { defaultLang, type Lang } from './lang';

const catalog = flatCatalogs(
    import.meta.glob<FlatTree>('../../../../locales/*/console/{common,nav,topbar,bell,modules/index,modules/catalog/valorant}.json', { eager: true, import: 'default' }),
    'console',
);

export function valorantConsoleText(lang: Lang, key: string, fallback = key): string {
    return catalog[lang]?.[key] ?? catalog[defaultLang][key] ?? fallback;
}
