import type { Actions, PageServerLoad } from './$types';
import type { CommandView, Perm } from '@bagel/shared';
import { PERMS, RESPONSE_MAX, normName, validateCommand, firstError, BUILTIN_COMMANDS, BUILTIN_NAMES, builtinDef } from '@bagel/shared';
import { listCommands, upsertCommand, deleteCommand, listModules, upsertModule, type ModuleView } from '$lib/server/commands-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import type { Session } from '$lib/server/session';
import { env } from '$env/dynamic/private';
import { fail, redirect } from '@sveltejs/kit';

// The dashboard a write targets: for a delegate it is the owner's board, for a
// normal login it is the user's own. A delegate must also hold the 'commands'
// section, else they have no business here.
function effectiveId(session: Session | null | undefined): string {
  return session?.delegate_of ?? session?.user_id ?? 'demo';
}

function gateCommands(session: Session | null | undefined): void {
  if (session?.delegate_of && !(session.sections ?? []).includes('commands')) {
    throw redirect(302, '/');
  }
}

// Sample rows use the STORED key format (no leading "!" — chat adds it), same
// as what the projector serves; the UI renders the "!" itself.
const sample: CommandView[] = [
  { name: 'uptime', aliases: ['live', 'up'], response: '{user} the stream has been live for {uptime}', perm: 'everyone', cooldown: 5, uses: '412', is_active: true, stream_online_only: true },
  { name: 'socials', aliases: ['social', 'links'], response: 'Follow along → twitch.tv/itsmavey · @itsmavey everywhere', perm: 'everyone', cooldown: 30, uses: '288', is_active: true },
  { name: 'bagel', response: '{user} tosses a warm bagel to {target}. Toasty.', perm: 'everyone', cooldown: 10, uses: '1.2k', is_active: true },
  { name: 'so', response: 'Go show some love to twitch.tv/{target} — absolute legend', perm: 'mod', cooldown: 0, uses: '96', is_active: true },
  { name: 'discord', response: 'Join the bakery → discord.gg/itsbagelbot', perm: 'everyone', cooldown: 60, uses: '203', is_active: true },
  { name: 'uptime-debug', response: 'node={node} replica={id} lag={ms}ms', perm: 'broadcaster', cooldown: 0, uses: '14', is_active: false },
  { name: 'lurk', response: '{user} fades into the shadows. Thanks for the lurk.', perm: 'everyone', cooldown: 5, uses: '521', is_active: true },
  { name: 'followage', response: "{user} you've followed for {followage}", perm: 'sub', cooldown: 15, uses: '177', is_active: true }
];

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
  if (env.DEMO === '1') return { commands: mergeCommands(sample, []) };
  try {
    const [custom, modules] = await Promise.all([listCommands(uid), listModules(uid).catch(() => [])]);
    return { commands: mergeCommands(custom, modules) };
  } catch {
    // Don't show fabricated rows in production; surface a degraded state.
    // Built-ins still render (their defaults) so the list is never empty.
    return { commands: mergeCommands([], []), degraded: true };
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

  // Twitch chat is single-line: a textarea lets users press Enter, but the
  // shared validator rejects any control character (CR/LF/tab). Fold control
  // characters to spaces and trim so a pasted multi-line note saves cleanly
  // instead of failing validation.
  const response = String(f.get('response') ?? '').replace(/[\u0000-\u001F]+/g, ' ').trim();
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
  if (env.DEMO !== '1' && !locals.session) return null;
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
    console.error(`[commands] ${action} failed:`, e instanceof Error ? (e.stack ?? e.message) : e);
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

export const actions: Actions = {
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
    if (env.DEMO === '1') {
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

    if (env.DEMO === '1') {
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

    if (env.DEMO === '1') return { ok: true, action: 'deleted', name };

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

    if (env.DEMO === '1') {
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

    if (env.DEMO === '1') {
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
