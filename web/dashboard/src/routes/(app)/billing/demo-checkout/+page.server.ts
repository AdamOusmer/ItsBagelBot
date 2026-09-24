// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { error, redirect } from '@sveltejs/kit';
import { actionError } from '$lib/server/action-errors';

const DEMO = dev && env.DEMO === '1';

type Plan = 'monthly' | 'single';
type Kind = 'premium' | 'gift';

function readPlan(value: string | null): Plan {
  return value === 'monthly' ? 'monthly' : 'single';
}

function readKind(value: string | null): Kind {
  return value === 'gift' ? 'gift' : 'premium';
}

export const load: PageServerLoad = async ({ url, locals }) => {
  if (!DEMO) throw error(404, actionError(locals.locale, 'Not found'));

  const plan = readPlan(url.searchParams.get('plan'));
  const kind = readKind(url.searchParams.get('kind'));
  const recipient = kind === 'gift' ? (url.searchParams.get('recipient') ?? '') : '';

  const copies = import.meta.glob('./copy/*.json', { import: 'default' });
  const loadCopy = copies[`./copy/${locals.locale}.json`] ?? copies['./copy/en.json'];
  const copy = await loadCopy() as typeof import('./copy/en.json');
  return { plan, kind, recipient, copy };
};

export const actions: Actions = {
  pay: async ({ request, locals }) => {
    if (!DEMO) throw error(404, actionError(locals.locale, 'Not found'));

    const form = await request.formData();
    const plan = readPlan(form.get('plan') as string | null);
    const kind = readKind(form.get('kind') as string | null);
    const recipient = kind === 'gift' ? String(form.get('recipient') ?? '') : '';

    const { demoCheckoutComplete, demoRecordTransaction } = await import('$lib/server/demo-data');
    if (kind === 'premium') demoCheckoutComplete(plan);
    demoRecordTransaction(kind, plan, kind === 'gift' ? recipient : null);

    throw redirect(303, '/billing?checkout=complete');
  }
};
