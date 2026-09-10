// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { error, redirect } from '@sveltejs/kit';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
// Same pattern as billing/+page.server.ts, repeated here rather than shared,
// because this whole route only exists to be erased.
const DEMO = dev && env.DEMO === '1';

type Plan = 'monthly' | 'single';
type Kind = 'premium' | 'gift';

function readPlan(value: string | null): Plan {
  return value === 'monthly' ? 'monthly' : 'single';
}

function readKind(value: string | null): Kind {
  return value === 'gift' ? 'gift' : 'premium';
}

// This route stands in for Tebex-hosted checkout. Outside a demo build there
// is nothing for it to stand in for, so it does not exist: both load and
// actions 404 the moment DEMO is false, rather than quietly rendering a fake
// payment page in production.
export const load: PageServerLoad = async ({ url }) => {
  if (!DEMO) throw error(404, 'Not found');

  const plan = readPlan(url.searchParams.get('plan'));
  const kind = readKind(url.searchParams.get('kind'));
  const recipient = kind === 'gift' ? (url.searchParams.get('recipient') ?? '') : '';

  return { plan, kind, recipient };
};

export const actions: Actions = {
  // Reads plan/kind/recipient from hidden form fields, not the URL: a bare
  // `?/pay` form action replaces the whole query string on submit, so the
  // query params the load rendered from would already be gone by the time
  // this runs.
  pay: async ({ request }) => {
    if (!DEMO) throw error(404, 'Not found');

    const form = await request.formData();
    const plan = readPlan(form.get('plan') as string | null);
    const kind = readKind(form.get('kind') as string | null);
    const recipient = kind === 'gift' ? String(form.get('recipient') ?? '') : '';

    const { demoCheckoutComplete, demoRecordTransaction } = await import('$lib/server/demo-data');
    // A gift never changes the buyer's own plan (mirrors the live gift path,
    // which pays for someone else's entitlement, not the buyer's own).
    if (kind === 'premium') demoCheckoutComplete(plan);
    demoRecordTransaction(kind, plan, kind === 'gift' ? recipient : null);

    // Lands back on the billing page's existing ?checkout=complete handling:
    // same celebration, same toast, same optimistic flip to Premium.
    throw redirect(303, '/billing?checkout=complete');
  }
};
