// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import type { CommandView, Perm } from '@bagel/shared';
import {
  PERMS,
  RESPONSE_MAX,
  normName,
  normalizeCommandResponse,
  validateCommand,
  firstError,
  BUILTIN_COMMANDS,
  BUILTIN_NAMES,
  builtinDef,
  DEFS_PER_BROADCASTER,
  parseJsonPath,
  slugifyName,
  validateFetchDef,
  type FetchDefErrors
} from '@bagel/shared';
import { ValkeyRateLimiter } from '@bagel/shared/server/rate-limit';
import { listCommands, upsertCommand, deleteCommand, listModules, upsertModule, type ModuleView } from '$lib/server/commands-store';
import { listFetches, upsertFetchDef, deleteFetchDef, rehearseFetch } from '$lib/server/fetches-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import { logger } from '@bagel/shared/server/logger';
import type { Session } from '$lib/server/session';
import { effectiveId } from '$lib/server/board';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { fail, redirect } from '@sveltejs/kit';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

function gateCommands(session: Session | null | undefined): void {
  if (session?.delegate_of && !(session.sections ?? []).includes('commands')) {
    throw redirect(302, '/');
  }
}

// configString reads one string field out of a module's opaque config blob,
// tolerating any non-object/absent shape. Used to pull a built-in's saved reply
// template out of the modules-service config.
function configString(configs: unknown, key: string): string {
  if (configs && typeof configs === 'object') {
    const v = (configs as Record<string, unknown>)[key];
    if (typeof v === 'string') return v;
  }
  return '';
}

// builtinViews turns the built-in catalog into command rows, reading each
// built-in's on/off state (and, for editable built-ins, its saved reply
// template) from the modules service (key = the built-in id). A missing module
// row means the catalog default. Non-editable built-ins render read-only with a
// toggle + preview; editable ones (e.g. clip) expose a reply template whose
// current value seeds the inspector's editor.
function builtinViews(modules: ModuleView[]): CommandView[] {
  const byName = new Map(modules.map((m) => [m.name, m]));
  return BUILTIN_COMMANDS.map((def) => {
    const row = byName.get(def.id);
    const savedReply = def.editable && def.replyKey ? configString(row?.configs, def.replyKey) : '';
    return {
      name: def.id,
      // Editable built-ins carry the saved template (or the default) so the
      // inspector's editor and rehearsal start from the real value; others show
      // the static summary.
      response: def.editable ? savedReply || def.preview : def.summary,
      is_active: row ? row.is_enabled : def.defaultActive,
      perm: def.defaultPerm,
      cooldown: def.defaultCooldown,
      stream_online_only: def.liveOnly,
      builtin: true
    } satisfies CommandView;
  });
}

// mergeCommands lists built-ins first, then the user's custom commands with any
// name colliding with a built-in dropped (built-ins reserve their trigger).
function mergeCommands(custom: CommandView[], modules: ModuleView[]): CommandView[] {
  const builtins = builtinViews(modules);
  const customs = custom.filter((c) => !BUILTIN_NAMES.has(c.name));
  return [...builtins, ...customs];
}

export const load: PageServerLoad = async ({ locals }) => {
  gateCommands(locals.session);
  const uid = effectiveId(locals.session);
  if (DEMO) {
    const { demoCommandRows, demoFetches } = await import('$lib/server/demo-data');
    return { commands: mergeCommands(demoCommandRows, []), ...demoFetches() };
  }
  try {
    // Fetch definitions ride the command list because the data-source picker
    // lives inside the command editor now; there is no separate page to load
    // them. Their failure is isolated so a gossip outage costs you the picker,
    // not the ability to edit commands at all.
    const [custom, modules, fetches] = await Promise.all([
      listCommands(uid),
      listModules(uid).catch(() => []),
      listFetches(uid).catch(() => ({ defs: [], keys: [] }))
    ]);
    return { commands: mergeCommands(custom, modules), ...fetches };
  } catch {
    // Don't show fabricated rows in production; surface a degraded state.
    // Built-ins still render (their defaults) so the list is never empty.
    return { commands: mergeCommands([], []), defs: [], keys: [], degraded: true };
  }
};

// Parses and normalizes the shared command fields out of a submitted form.
// Normalization (normName: drop the leading "!", lower-case) matches the
// commands service, so the optimistic UI key agrees with what the service
// returns (no phantom duplicate row on rename).
function parseCommand(f: FormData) {
  const name = normName(String(f.get('name') ?? ''));

  // Alternate names arrive as repeated `aliases` fields. Trim, drop blanks, and
  // de-duplicate case-insensitively so the wire payload matches what the
  // commands service will accept.
  const seen = new Set<string>();
  const aliases: string[] = [];
  for (const raw of f.getAll('aliases')) {
    const a = normName(String(raw));
    if (!a) continue;
    if (seen.has(a)) continue;
    seen.add(a);
    aliases.push(a);
  }

  // The editor posts one LF-delimited response line per chat message. Preserve
  // those separators and canonicalize the value exactly as the commands
  // service does, so Sesame can fan the stored response out one line at a time.
  const response = normalizeCommandResponse(String(f.get('response') ?? ''));
  const permRaw = String(f.get('perm') ?? 'everyone');
  const perm: Perm = (PERMS as readonly string[]).includes(permRaw) ? (permRaw as Perm) : 'everyone';

  // Cooldown arrives as a string; clamp to a sane non-negative integer.
  const cooldown = Math.max(0, Math.floor(Number(f.get('cooldown') ?? 0) || 0));

  // Optional single-user lock; keep digits only so a stray "@name" can't slip through.
  const allowedUserId = String(f.get('allowed_user_id') ?? '').replace(/\D/g, '');

  const streamOnlineOnly = f.get('stream_online_only') === 'on';

  return { name, aliases, response, perm, cooldown, allowedUserId, streamOnlineOnly };
}

// Build the CommandView a DEMO action echoes back (mirrors upsertCommand's
// optimistic view construction).
function demoView(cmd: ReturnType<typeof parseCommand>, isActive: boolean): CommandView {
  return {
    name: cmd.name,
    aliases: cmd.aliases,
    response: cmd.response,
    is_active: isActive,
    stream_online_only: cmd.streamOnlineOnly,
    perm: cmd.perm,
    cooldown: cmd.cooldown,
    allowed_user_id: cmd.allowedUserId
  };
}

// actionContext runs the shared action prologue: section gate, effective
// dashboard id, auth check, and form parse. DEMO runs without a real session;
// the demo branches in each action short-circuit before any RPC, so only
// production requests need the auth gate — null means "respond 401".
async function actionContext({ request, locals }: { request: Request; locals: App.Locals }) {
  gateCommands(locals.session);
  if (!DEMO && !locals.session) return null;
  return {
    uid: effectiveId(locals.session),
    session: locals.session,
    form: await request.formData()
  };
}

const notSignedIn = () => fail(401, { ok: false, error: 'Not signed in.' });

// tryRpc runs one store RPC, logging the real failure server-side — RpcError /
// NATS timeout messages can carry internal service detail, so they go to the
// logs, never the dashboard. The caller returns a generic fail(); the client
// shows its own localized "…failed" copy.
async function tryRpc<T>(action: string, call: () => Promise<T>): Promise<{ ok: true; value: T } | { ok: false }> {
  try {
    return { ok: true, value: await call() };
  } catch (e) {
    logger.error({ err: e }, `[commands] ${action} failed`);
    return { ok: false };
  }
}

// builtinRow rebuilds one built-in's CommandView so the optimistic UI
// reconciles the same way it does for custom rows.
function builtinRow(def: NonNullable<ReturnType<typeof builtinDef>>, response: string, isActive: boolean): CommandView {
  return {
    name: def.id,
    response,
    is_active: isActive,
    perm: def.defaultPerm,
    cooldown: def.defaultCooldown,
    stream_online_only: def.liveOnly,
    builtin: true
  };
}

// parseSaveForm reads the editor's submission: the shared command fields plus
// the edit/rename bookkeeping. A rename passes original_name so the commands
// service updates the row's name field in place (single write) instead of
// delete-old + create-new.
function parseSaveForm(f: FormData) {
  const cmd = parseCommand(f);
  const isEdit = f.get('edit') === '1';
  const originalName = normName(String(f.get('original_name') ?? ''));
  return {
    cmd,
    isActive: f.get('is_active') === 'on',
    isEdit,
    originalName,
    renamed: isEdit && originalName !== '' && originalName !== cmd.name
  };
}

// saveResult shapes the save action's reply; applyResult only reads the
// affected row out of `commands`, so echoing just that row is enough in DEMO.
function saveResult(s: ReturnType<typeof parseSaveForm>, commands: CommandView[]) {
  return {
    ok: true,
    action: s.isEdit ? 'updated' : 'created',
    name: s.cmd.name,
    original: s.renamed ? s.originalName : undefined,
    commands
  };
}

// Each test run dials a third-party host for real, so it gets its own bucket
// rather than sharing a command-save allowance: 6 back-to-back attempts, then a
// refill of one per ten seconds. Same numbers the standalone fetches page used;
// moving the UI into the command editor does not make the upstream cheaper.
const fetchTestLimiter = new ValkeyRateLimiter({ name: 'fetchtest', capacity: 6, refillPerSec: 0.1 });

interface DefForm {
  name: string;
  url: string;
  kind: 'plain' | 'json';
  path: string[];
  keyLabel: string;
  isEdit: boolean;
  originalName: string;
  /** Edit + a distinct non-empty original slug: rename detection rides on the
   * parsed draft so validator, store call and reply all read one field. */
  renamed: boolean;
}

// parseDefForm reads the builder's submission; normalization mirrors the client
// (slugifyName) so the optimistic UI key agrees with what lands.
//
// No is_active is parsed: the pause toggle is gone from the UI, so every
// definition the builder writes is active. The field still exists in the store
// and the projection, which is why the write below hard-codes true instead of
// dropping it — a def saved without it would read back as paused and silently
// stop resolving.
function parseDefForm(f: FormData): DefForm {
  const kindRaw = String(f.get('kind') ?? 'plain');
  const pathRaw = String(f.get('path') ?? '');
  const name = slugifyName(String(f.get('name') ?? ''));
  const originalName = slugifyName(String(f.get('original_name') ?? ''));
  return {
    name,
    url: String(f.get('url') ?? '').trim(),
    kind: kindRaw === 'json' ? 'json' : 'plain',
    path: kindRaw === 'json' ? (parseJsonPath(pathRaw.trim()) ?? []) : [],
    keyLabel: slugifyName(String(f.get('key_label') ?? '')),
    isEdit: f.get('edit') === '1',
    originalName,
    renamed: f.get('edit') === '1' && originalName !== '' && originalName !== name
  };
}

// Courtesy pre-read ahead of the service's own synchronous enforcement (COUNT
// before insert, unique (user_id,name)): our list can be a beat stale, Go owns
// truth. Assigns onto errors.name so a collision message outranks a field error.
async function precheckFetchConflicts(uid: string, def: DefForm, errors: FetchDefErrors): Promise<void> {
  if (DEMO) return;
  const fresh = await tryRpc('fetch-pre-check', () => listFetches(uid));
  if (!fresh.ok) return;
  const existsElsewhere = fresh.value.defs.some((d) => d.name === def.name && d.name !== def.originalName);
  if (existsElsewhere) {
    errors.name = `A data source named "${def.name}" already exists.`;
  } else if (!fresh.value.defs.some((d) => d.name === def.name) && fresh.value.defs.length >= DEFS_PER_BROADCASTER) {
    errors.name = `At most ${DEFS_PER_BROADCASTER} data sources per channel.`;
  }
}

// testRunThrottle returns the refusal message, or null to proceed. Demo never
// spends the bucket: it dials nothing.
async function testRunThrottle(uid: string): Promise<string | null> {
  if (DEMO) return null;
  const decision = await fetchTestLimiter.check(`fetchtest:${uid}`);
  if (decision.allowed) return null;
  return 'Too many test runs — each one calls the real API. Wait about 10 seconds and try again.';
}

// Fields that must be sound before we dial a third-party host for real. The
// slug is not among them: the builder fetches a sample before the author has
// named anything, so an unnamed draft is expected here and only here.
const TEST_BLOCKING_FIELDS = ['url', 'path', 'kind', 'key_label'] as const;

function testDraftError(def: DefForm): string | null {
  const errors = validateFetchDef({
    name: def.name || 'draft',
    url: def.url,
    kind: def.kind,
    path: def.path,
    keyLabel: def.keyLabel
  });
  if (!TEST_BLOCKING_FIELDS.some((field) => errors[field])) return null;
  return firstError(errors) ?? 'Fix the highlighted fields first.';
}

// The token identity falls back to the key label, then to a placeholder, so an
// unnamed draft still rehearses.
function rehearsalName(def: DefForm): string {
  if (def.name) return def.name;
  const fromKey = normName(def.keyLabel);
  if (fromKey) return fromKey;
  return 'draft';
}

async function demoTestReply() {
  const { demoFetchTestRun } = await import('$lib/server/demo-data');
  const demo = demoFetchTestRun();
  return { ok: true, action: 'fetchtested', status: 'ok', values: demo.values, ms: demo.ms, sample: demo.sample };
}

// runRehearsal owns the dial and its error mapping. Returns null when gossip
// did not answer; the caller turns that into the 502 so ActionData still sees
// the fail() inside the action.
async function runRehearsal(uid: string, def: DefForm) {
  try {
    const reply = await rehearseFetch(uid, {
      name: rehearsalName(def),
      url: def.url,
      jsonPath: def.path,
      keyLabel: def.keyLabel
    });
    return {
      ok: true,
      action: 'fetchtested',
      status: reply.status,
      values: reply.values,
      ms: reply.ms,
      sample: reply.sample
    };
  } catch (e) {
    logger.error({ err: e }, '[commands] fetch testrun failed');
    return null;
  }
}

export const actions: Actions = {
  // Save a data source from the builder inside the command editor.
  savefetch: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn();
    const def = parseDefForm(ctx.form);

    // Shared validator: the client builder runs these exact checks, so this is
    // the authoritative re-check rather than a duplicate of a different shape.
    const errors: FetchDefErrors = validateFetchDef({
      name: def.name,
      url: def.url,
      kind: def.kind,
      path: def.path,
      keyLabel: def.keyLabel
    });
    await precheckFetchConflicts(ctx.uid, def, errors);
    if (Object.keys(errors).length) {
      return fail(400, { ok: false, errors, error: firstError(errors) });
    }

    if (DEMO) {
      const { demoFetches } = await import('$lib/server/demo-data');
      const current = demoFetches();
      const defs = current.defs.filter((d) => d.name !== def.originalName && d.name !== def.name);
      defs.push({ name: def.name, url: def.url, json_path: def.path, is_active: true, key_label: def.keyLabel });
      return { ok: true, action: 'fetchsaved', name: def.name, defs, keys: current.keys };
    }

    const res = await tryRpc('savefetch', () =>
      upsertFetchDef(ctx.uid, {
        name: def.name,
        url: def.url,
        jsonPath: def.path,
        isActive: true,
        keyLabel: def.keyLabel,
        originalName: def.renamed ? def.originalName : undefined
      })
    );
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, def.isEdit ? 'fetchdef:update' : 'fetchdef:create', def.name);
    return { ok: true, action: 'fetchsaved', name: def.name, defs: res.value.defs, keys: res.value.keys };
  },

  // The service refuses while any command response still references
  // `{urlfetch:<name>}`; the client only pre-warns.
  deletefetch: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn();
    const name = slugifyName(String(ctx.form.get('name') ?? ''));

    if (DEMO) {
      const { demoFetches } = await import('$lib/server/demo-data');
      const current = demoFetches();
      return { ok: true, action: 'fetchdeleted', name, defs: current.defs.filter((d) => d.name !== name), keys: current.keys };
    }

    const res = await tryRpc('deletefetch', () => deleteFetchDef({ userId: ctx.uid, name }));
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, 'fetchdef:delete', name);
    const fresh = await tryRpc('deletefetch-refresh', () => listFetches(ctx.uid));
    return {
      ok: true,
      action: 'fetchdeleted',
      name,
      defs: fresh.ok ? fresh.value.defs : [],
      keys: fresh.ok ? fresh.value.keys : []
    };
  },

  // Rehearsal dry-run: executes the REAL chat path (same gossip subject, SSRF
  // gate, buckets) with DryRun+Fresh and the posted draft inline as Def. Returns
  // the raw body as `sample` so the builder can render a clickable tree — that
  // is the whole point of the call for a non-technical author, who otherwise
  // has to paste a response by hand. Nothing is persisted.
  testfetch: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn();

    // Each step answers with a message or nothing, and the fail() stays here:
    // SvelteKit infers ActionData from the fail() calls written inside the
    // action, so the branching moves out but the refusals cannot.
    const throttled = await testRunThrottle(ctx.uid);
    if (throttled) return fail(429, { ok: false, error: throttled });

    const def = parseDefForm(ctx.form);
    const invalid = testDraftError(def);
    if (invalid) return fail(400, { ok: false, error: invalid });

    if (DEMO) return demoTestReply();

    const reply = await runRehearsal(ctx.uid, def);
    if (!reply) return fail(502, { ok: false, error: 'The fetch service did not answer. Try again in a moment.' });
    return reply;
  },

  save: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn();
    const s = parseSaveForm(ctx.form);

    // Shared validator: the client editor runs the exact same checks, so this
    // is the authoritative re-check. errors is a field -> message map for
    // inline display; error keeps the single-line toast fallback.
    const errors = validateCommand({
      name: s.cmd.name,
      aliases: s.cmd.aliases,
      response: s.cmd.response,
      cooldown: s.cmd.cooldown,
      allowedUserId: s.cmd.allowedUserId
    });
    if (Object.keys(errors).length) {
      return fail(400, { ok: false, errors, error: firstError(errors) });
    }

    // DEMO: echo the row back as a success so the demo console exercises the
    // full optimistic flow without NATS.
    if (DEMO) {
      return saveResult(s, [demoView(s.cmd, s.isActive)]);
    }

    const res = await tryRpc('save', () =>
      upsertCommand(ctx.uid, { ...s.cmd, isActive: s.isActive }, s.renamed ? s.originalName : undefined)
    );
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, s.isEdit ? 'command:update' : 'command:create', s.cmd.name);
    return saveResult(s, res.value.commands);
  },

  // Lightweight toggle: flips is_active without going through the full editor.
  toggle: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn();
    const { uid, form: f } = ctx;

    const cmd = parseCommand(f);
    const isActive = f.get('is_active') === 'on';

    if (DEMO) {
      return { ok: true, action: 'updated', name: cmd.name, commands: [demoView(cmd, isActive)], silent: true };
    }

    const res = await tryRpc('toggle', () => upsertCommand(uid, { ...cmd, isActive }));
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, 'command:toggle', `${cmd.name}=${isActive}`);

    return { ok: true, action: 'updated', name: cmd.name, commands: res.value.commands, silent: true };
  },

  delete: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn();
    const { uid, form: f } = ctx;

    const name = String(f.get('name') ?? '');

    if (DEMO) return { ok: true, action: 'deleted', name };

    const res = await tryRpc('delete', () => deleteCommand(uid, name));
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, 'command:delete', name);

    return { ok: true, action: 'deleted', name, commands: res.value.commands };
  },

  // Toggle a built-in command on/off. Built-in state lives in the modules
  // service (key = the built-in id), not the commands service, so this is a
  // separate path from the custom-command toggle.
  toggleBuiltin: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn();
    const { uid, form: f } = ctx;

    const name = normName(String(f.get('name') ?? ''));
    const def = builtinDef(name);
    if (!def) return fail(400, { ok: false, error: 'Unknown built-in command.' });
    const isActive = f.get('is_active') === 'on';
    const view = builtinRow(def, def.summary, isActive);

    if (DEMO) {
      return { ok: true, action: 'updated', name, commands: [view], silent: true };
    }

    const res = await tryRpc('toggleBuiltin', () => upsertModule(uid, def.id, isActive));
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, 'command:builtin_toggle', `${name}=${isActive}`);
    return { ok: true, action: 'updated', name, commands: [view], silent: true };
  },

  // Save an editable built-in's custom reply template. Like the toggle, the
  // value lives in the modules service (under the built-in id, config key
  // def.replyKey), so this writes there — not the commands service. An empty
  // reply clears the override (upsertModule omits empty config), so the bot
  // falls back to the default template. The current on/off state rides along so
  // the write preserves it.
  saveBuiltinReply: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn();
    const { uid, form: f } = ctx;

    const name = normName(String(f.get('name') ?? ''));
    const def = editableBuiltin(name);
    if (!def) {
      return fail(400, { ok: false, error: 'This command has no editable reply.' });
    }
    const reply = String(f.get('reply') ?? '').trim();
    if (reply.length > RESPONSE_MAX) {
      return fail(400, { ok: false, error: `Reply is too long (max ${RESPONSE_MAX}).` });
    }
    const isActive = f.get('is_active') === 'on';
    const view = builtinRow(def, reply || def.preview, isActive);

    if (DEMO) {
      return { ok: true, action: 'updated', name, commands: [view], silent: true };
    }

    const res = await tryRpc('saveBuiltinReply', () =>
      upsertModule(uid, def.id, isActive, reply ? { [def.replyKey!]: reply } : undefined)
    );
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, 'command:builtin_reply', name);
    return { ok: true, action: 'updated', name, commands: [view], silent: true };
  }
};

// editableBuiltin resolves a built-in that carries an editable reply template.
function editableBuiltin(name: string) {
  const def = builtinDef(name);
  if (!def?.editable || !def.replyKey) return undefined;
  return def;
}
