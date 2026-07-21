import type { Actions, PageServerLoad } from './$types';
import {
  readQuotes,
  addQuote,
  editQuote,
  removeQuote,
  setEnabled,
  setPerm,
  type QuotePerms,
  type QuoteView
} from '$lib/server/quotes-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import { gateModulePage } from '$lib/server/module-gate';
import type { Session } from '$lib/server/session';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';

// The longest quote the modules service will store (repository.QuoteTextMaxLen).
const QUOTE_MAX = 450;

// Who may be granted the save/edit permissions; mirrors the sesame module's
// ParsePerm keys and the module catalog's perm options.
const PERMS = ['mod', 'vip', 'sub', 'everyone'] as const;

// Which module blob key a perm form writes: the save gate or the edit gate.
const PERM_KINDS: Record<string, keyof QuotePerms> = { add: 'addPerm', edit: 'editPerm' };

// parseQuoteText normalizes a submitted quote body (control chars collapse to
// spaces) and reports why it is unusable; add and edit share the boundary.
function parseQuoteText(value: FormDataEntryValue | null): { text?: string; error?: string } {
  const text = String(value ?? '')
    .replace(/[\u0000-\u001f]+/g, ' ')
    .trim();
  if (!text) return { error: 'Enter a quote to save.' };
  if (text.length > QUOTE_MAX) return { error: `Quote is too long (max ${QUOTE_MAX}).` };
  return { text };
}

function quoteDate(value: FormDataEntryValue | null): Date | null {
  const raw = String(value ?? '');
  if (!/^\d{4}-\d{2}-\d{2}$/.test(raw)) return null;
  const parsed = new Date(`${raw}T12:00:00Z`);
  return Number.isNaN(parsed.getTime()) || parsed.toISOString().slice(0, 10) !== raw ? null : parsed;
}

function effectiveId(session: Session | null | undefined): string {
  return session?.delegate_of ?? session?.user_id ?? 'demo';
}

// Delegate scope comes from the quotes catalog def (see module-gate.ts).
function gate(session: Session | null | undefined): void {
  gateModulePage(session, 'quotes');
}

// actingLogin is the login stamped as a quote's added_by (audit only): the
// delegate acting, or the signed-in broadcaster.
function actingLogin(session: Session | null | undefined): string {
  return session?.delegate_login ?? session?.login ?? 'dashboard';
}

// Demo book so the tab renders without a live backend.
function demoQuotes(): QuoteView[] {
  return [
    { number: 1, text: 'I meant to do that.', added_by: 'mod_amy', created_at: '2026-06-02T20:14:00Z' },
    { number: 3, text: 'The bagels are sentient and I welcome them.', added_by: 'mod_amy', created_at: '2026-06-19T02:41:00Z' },
    { number: 4, text: 'Never trust a ferret with a keyboard.', added_by: 'streamer', created_at: '2026-07-01T18:03:00Z' }
  ];
}

export const load: PageServerLoad = async ({ locals }) => {
  gate(locals.session);
  const uid = effectiveId(locals.session);
  if (env.DEMO === '1') return { enabled: true, addPerm: 'mod', editPerm: 'mod', quotes: demoQuotes() };
  try {
    const view = await readQuotes(uid);
    return { enabled: view.enabled, addPerm: view.addPerm, editPerm: view.editPerm, quotes: view.quotes };
  } catch {
    return { enabled: false, addPerm: 'mod', editPerm: 'mod', quotes: [] as QuoteView[], degraded: true };
  }
};

// actionContext runs the shared prologue: scope gate, effective id, auth check,
// and form parse. DEMO runs without a session (branches short-circuit before RPC).
async function actionContext({ request, locals }: { request: Request; locals: App.Locals }) {
  gate(locals.session);
  if (env.DEMO !== '1' && !locals.session) return null;
  return { uid: effectiveId(locals.session), session: locals.session, form: await request.formData() };
}

const notSignedIn = () => fail(401, { ok: false, error: 'Not signed in.' });

export const actions: Actions = {
  add: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn();

    const { text, error } = parseQuoteText(ctx.form.get('text'));
    if (!text) return fail(400, { ok: false, error });

    const createdAt = quoteDate(ctx.form.get('quote_date'));
    if (!createdAt) return fail(400, { ok: false, error: 'Choose a valid quote date.' });

    if (env.DEMO === '1') {
      return { ok: true, action: 'added', quote: { number: 0, text, created_at: createdAt.toISOString() } };
    }

    try {
      const quote = await addQuote(ctx.uid, {
        text,
        addedBy: actingLogin(ctx.session),
        createdAt: createdAt.toISOString()
      });
      auditDashboardImpersonation(ctx.session, 'quotes:add', String(quote.number));
      return { ok: true, action: 'added', quote };
    } catch (e) {
      console.error('[quotes] add failed:', e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false, error: 'Could not save the quote.' });
    }
  },

  // Rewrite one quote's text in place; the number and save date survive.
  edit: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn();

    const num = Math.trunc(Number(ctx.form.get('number')));
    if (!Number.isFinite(num) || num <= 0) return fail(400, { ok: false, error: 'Invalid quote number.' });

    const { text, error } = parseQuoteText(ctx.form.get('text'));
    if (!text) return fail(400, { ok: false, error });

    if (env.DEMO === '1') {
      return { ok: true, action: 'edited', quote: { number: num, text, created_at: new Date().toISOString() } };
    }

    try {
      const quote = await editQuote(ctx.uid, num, text);
      auditDashboardImpersonation(ctx.session, 'quotes:edit', String(num));
      return { ok: true, action: 'edited', quote };
    } catch (e) {
      console.error('[quotes] edit failed:', e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false, error: 'Could not update the quote.' });
    }
  },

  delete: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn();

    const num = Math.trunc(Number(ctx.form.get('number')));
    if (!Number.isFinite(num) || num <= 0) return fail(400, { ok: false, error: 'Invalid quote number.' });

    if (env.DEMO === '1') return { ok: true, action: 'deleted', number: num };

    try {
      await removeQuote(ctx.uid, num);
      auditDashboardImpersonation(ctx.session, 'quotes:delete', String(num));
      return { ok: true, action: 'deleted', number: num };
    } catch (e) {
      console.error('[quotes] delete failed:', e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false, error: 'Could not delete the quote.' });
    }
  },

  // Master on/off for the whole module (whether !quote does anything in chat).
  toggle: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn();

    const enabled = ctx.form.get('is_enabled') === 'on';
    if (env.DEMO === '1') return { ok: true, enabled };

    try {
      await setEnabled(ctx.uid, enabled);
      auditDashboardImpersonation(ctx.session, 'quotes:toggle', String(enabled));
      return { ok: true, enabled };
    } catch (e) {
      console.error('[quotes] toggle failed:', e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false });
    }
  },

  // Change who may save or edit a quote from chat (moderator by default).
  // The form names which gate it writes via kind=add|edit.
  perm: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn();

    const kind = PERM_KINDS[String(ctx.form.get('kind') ?? '')];
    if (!kind) return fail(400, { ok: false });

    const raw = String(ctx.form.get('perm') ?? '');
    const perm = (PERMS as readonly string[]).includes(raw) ? raw : 'mod';
    if (env.DEMO === '1') return { ok: true, kind, perm };

    try {
      await setPerm(ctx.uid, kind, perm);
      auditDashboardImpersonation(ctx.session, `quotes:perm:${kind}`, perm);
      return { ok: true, kind, perm };
    } catch (e) {
      console.error('[quotes] perm failed:', e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false });
    }
  }
};
