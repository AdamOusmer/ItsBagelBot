// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { demoCheckoutComplete, demoRecordTransaction } from '../../../../dev/fixtures';

type Plan = 'monthly' | 'single';
type Kind = 'premium' | 'gift';

function readPlan(value: string | null): Plan {
  return value === 'monthly' ? 'monthly' : 'single';
}

function readKind(value: string | null): Kind {
  return value === 'gift' ? 'gift' : 'premium';
}

export const load: PageServerLoad = async ({ url, locals }) => {
  const plan = readPlan(url.searchParams.get('plan'));
  const kind = readKind(url.searchParams.get('kind'));
  const recipient = kind === 'gift' ? (url.searchParams.get('recipient') ?? '') : '';

  const copies = import.meta.glob('./copy/*.json', { import: 'default' });
  const loadCopy = copies[`./copy/${locals.locale}.json`] ?? copies['./copy/en.json'];
  const copy = await loadCopy() as typeof import('./copy/en.json');
  return { plan, kind, recipient, copy };
};

export const actions: Actions = {
  pay: async ({ request }) => {
    const form = await request.formData();
    const plan = readPlan(form.get('plan') as string | null);
    const kind = readKind(form.get('kind') as string | null);
    const recipient = kind === 'gift' ? String(form.get('recipient') ?? '') : '';

    if (kind === 'premium') demoCheckoutComplete(plan);
    demoRecordTransaction({ kind, plan, recipient: kind === 'gift' ? recipient : null });

    throw redirect(303, '/billing?checkout=complete');
  }
};
