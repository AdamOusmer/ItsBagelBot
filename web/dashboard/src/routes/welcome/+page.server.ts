// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The first-visit tour, as its own page. It used to be a coach-mark layer
// drawn over the home board (OnboardingGuide.svelte): the shell, the rail and
// the live panels all competed with it, and a 380px bubble was the most room
// any step ever got. It is now a full screen outside the (app) group with
// nothing else on it. The home load sends confirmed-empty, not-yet-onboarded
// owners here; anyone signed in can also open it directly.
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { fail, redirect } from '@sveltejs/kit';
import { setOnboarded } from '$lib/server/services';
import type { Actions, PageServerLoad } from './$types';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

// Where an exit may land. The value comes back from a form field, and an open
// redirect off a `next` parameter is the classic shape of that bug, so this is
// an allowlist of the pages the tour itself links to rather than a prefix
// check.
const EXITS = new Set(['/', '/commands', '/modules', '/settings/import']);

function exitFor(raw: FormDataEntryValue | null): string {
  return typeof raw === 'string' && EXITS.has(raw) ? raw : '/';
}

export const load: PageServerLoad = async ({ locals }) => {
  let s = locals.session;
  if (!s && DEMO) s = (await import('$lib/server/demo-data')).demoSession();
  if (!s) throw redirect(302, `/login?next=${encodeURIComponent('/welcome')}`);
  // A delegate operates someone else's board. Accepting the terms and modding
  // the bot are the owner's to do, so the tour is theirs alone.
  if (s.delegate_of) throw redirect(303, '/');
  return { name: s.display_name || s.login };
};

export const actions: Actions = {
  // Every way out of the tour lands here: Done, Skip, and each step's link
  // onto its page. Marking the account onboarded before redirecting is what
  // keeps the home load from bouncing the visitor straight back in.
  done: async ({ locals, request }) => {
    const next = exitFor((await request.formData()).get('next'));
    if (DEMO) throw redirect(303, next);
    const s = locals.session;
    if (!s) return fail(401);
    if (s.delegate_of) return fail(403);
    try {
      await setOnboarded(s.user_id, true);
    } catch {
      return fail(502, { error: 'onboarded failed' });
    }
    throw redirect(303, next);
  }
};
