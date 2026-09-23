// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The first-visit setup journey. The home load sends not-yet-onboarded owners
// here; the owner can also return directly.
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { fail, redirect, type Cookies } from '@sveltejs/kit';
import { setOnboarded } from '$lib/server/services';
import { applyFeaturePreset } from '$lib/server/feature-presets';
import { SERVER_STRATEGIES } from '$lib/server/importer';
import type { Session } from '$lib/server/session';
import { IMPORT_SOURCES, type ImportSource } from '@bagel/kit';
import type { Actions, PageServerLoad } from './$types';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

const SETUP_PRESETS = ['start-new', 'integrate', 'quiet'] as const;
type SetupPreset = (typeof SETUP_PRESETS)[number];

// Accepting terms and choosing a channel-wide preset belong to the owner,
// not a delegate or an admin viewing as that owner.
const isOwner = (s: Session) => !s.delegate_of && !s.impersonator_id;
const consented = (form: FormData) => form.get('consent') === 'yes';

async function tourSession(locals: App.Locals): Promise<Session | null> {
  if (locals.session) return locals.session;
  return DEMO ? (await import('$lib/server/demo-data')).demoSession() : null;
}

function sourceConnected(id: ImportSource, cookies: Cookies): boolean {
  const check = SERVER_STRATEGIES[id].connected;
  if (!check) return false;
  return DEMO || check(cookies);
}

function setupPreset(value: FormDataEntryValue | null): SetupPreset | null {
  return SETUP_PRESETS.find((preset) => preset === value) ?? null;
}

function owner(locals: App.Locals) {
  const s = locals.session;
  if (!s) return { failure: fail(401, { error: 'Sign in to finish setup.' }) };
  if (!isOwner(s)) return { failure: fail(403, { error: 'Only the channel owner can finish setup.' }) };
  return { uid: s.user_id };
}

async function markOnboarded(save: () => Promise<unknown>) {
  try {
    await save();
    return null;
  } catch {
    return fail(502, { error: 'Could not save your setup. Try again.' });
  }
}

export const load: PageServerLoad = async ({ locals, cookies }) => {
  const s = await tourSession(locals);
  if (!s) throw redirect(302, `/login?next=${encodeURIComponent('/welcome')}`);
  if (!isOwner(s)) throw redirect(303, '/');
  const connected = Object.fromEntries(IMPORT_SOURCES.map((id) => [id, sourceConnected(id, cookies)])) as Record<
    ImportSource,
    boolean
  >;
  return { name: s.display_name || s.login, connected };
};

export const actions: Actions = {
  // The import choice continues the onboarding journey. The other choices
  // apply their preset and finish before entering the dashboard.
  done: async ({ locals, request }) => {
    const form = await request.formData();
    if (!consented(form)) return fail(400, { error: 'Accept the agreement.' });
    const preset = setupPreset(form.get('preset'));
    if (!preset) return fail(400, { error: 'Choose a setup.' });
    if (DEMO) throw redirect(303, '/');
    const who = owner(locals);
    if ('failure' in who) return who.failure;
    const { uid } = who;
    const failed = await markOnboarded(async () => {
      await applyFeaturePreset(uid, preset);
      await setOnboarded(uid, true);
    });
    if (failed) return failed;
    throw redirect(303, '/');
  },
  finishImport: async ({ locals, request }) => {
    const form = await request.formData();
    if (!consented(form)) return fail(400, { error: 'Accept the agreement.' });
    if (DEMO) return { ok: true };
    const who = owner(locals);
    if ('failure' in who) return who.failure;
    const { uid } = who;
    return (await markOnboarded(() => setOnboarded(uid, true))) ?? { ok: true };
  }
};
