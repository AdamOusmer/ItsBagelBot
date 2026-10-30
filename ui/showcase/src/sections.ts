// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface ShowcaseLink {
  href: string;
  label: string;
}

export interface ShowcaseGroup {
  title: string;
  links: ShowcaseLink[];
}

export const SECTIONS: ShowcaseGroup[] = [
  {
    title: 'Library',
    links: [
      { href: '/', label: 'Overview' },
      { href: '/foundations/', label: 'Foundations' },
      { href: '/controls/', label: 'Controls' },
    ],
  },
  {
    title: 'Lab',
    links: [
      { href: '/lab/toggles/', label: 'Switch and checkbox' },
      { href: '/lab/color/', label: 'Color picker' },
    ],
  },
];
