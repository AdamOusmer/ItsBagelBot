import type { Actions, PageServerLoad } from './$types';
import type { CommandView, Perm } from '@bagel/shared';
import { PERMS, normName, validateCommand, firstError, BUILTIN_COMMANDS, BUILTIN_NAMES, builtinDef } from '@bagel/shared';
import { listCommands, upsertCommand, deleteCommand, listModules, upsertModule } from '$lib/server/commands-store';
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

// builtinViews turns the built-in catalog into command rows, reading each
// built-in's on/off state from the modules service (key = the built-in id). A
// missing module row means the catalog default. These render read-only on the
// commands page with a toggle + preview.
function builtinViews(modules: { name: string; is_enabled: boolean }[]): CommandView[] {
  const byName = new Map(modules.map((m) => [m.name, m]));
  return BUILTIN_COMMANDS.map((def) => {
    const row = byName.get(def.id);
    return {
      name: def.id,
      response: def.summary,
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
function mergeCommands(custom: CommandView[], modules: { name: string; is_enabled: boolean }[]): CommandView[] {
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

  const response = String(f.get('response') ?? '');
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

export const actions: Actions = {
  save: async ({ request, locals }) => {
    gateCommands(locals.session);
    const uid = effectiveId(locals.session);
    // DEMO runs without a real session; the demo branches below short-circuit
    // before any RPC, so only production requests need the auth gate.
    if (env.DEMO !== '1' && !locals.session) {
      return fail(401, { ok: false, error: 'Not signed in.' });
    }

    const f = await request.formData();
    const cmd = parseCommand(f);
    const isActive = f.get('is_active') === 'on';
    const isEdit = f.get('edit') === '1';
    const originalName = normName(String(f.get('original_name') ?? ''));
    const renamed = isEdit && originalName !== '' && originalName !== cmd.name;

    // Shared validator: the client editor runs the exact same checks, so this
    // is the authoritative re-check. errors is a field -> message map for
    // inline display; error keeps the single-line toast fallback.
    const errors = validateCommand({
      name: cmd.name,
      aliases: cmd.aliases,
      response: cmd.response,
      cooldown: cmd.cooldown,
      allowedUserId: cmd.allowedUserId
    });
    if (Object.keys(errors).length) {
      return fail(400, { ok: false, errors, error: firstError(errors) });
    }

    // DEMO: echo the row back as a success so the demo console exercises the
    // full optimistic flow without NATS. applyResult only reads the affected
    // row out of `commands`, so echoing just that row is enough.
    if (env.DEMO === '1') {
      return {
        ok: true,
        action: isEdit ? 'updated' : 'created',
        name: cmd.name,
        original: renamed ? originalName : undefined,
        commands: [demoView(cmd, isActive)]
      };
    }

    // A rename passes original_name so the commands service updates the row's
    // name field in place (single write) instead of delete-old + create-new.
    // The optimistic reply already drops the old key and lists the renamed
    // command, so one round trip covers it. A write failure throws the service's
    // real error, returned as a fail() so the toast shows it (not a bare "failed").
    let commands: CommandView[];
    try {
      ({ commands } = await upsertCommand(uid, { ...cmd, isActive }, renamed ? originalName : undefined));
    } catch (e) {
      logRpcFailure('save', e);
      return fail(400, { ok: false });
    }

    auditDashboardImpersonation(locals.session, isEdit ? 'command:update' : 'command:create', cmd.name);

    return {
      ok: true,
      action: isEdit ? 'updated' : 'created',
      name: cmd.name,
      original: renamed ? originalName : undefined,
      commands
    };
  },

  // Lightweight toggle: flips is_active without going through the full editor.
  toggle: async ({ request, locals }) => {
    gateCommands(locals.session);
    const uid = effectiveId(locals.session);
    // DEMO runs without a real session; the demo branches below short-circuit
    // before any RPC, so only production requests need the auth gate.
    if (env.DEMO !== '1' && !locals.session) {
      return fail(401, { ok: false, error: 'Not signed in.' });
    }

    const f = await request.formData();
    const cmd = parseCommand(f);
    const isActive = f.get('is_active') === 'on';

    if (env.DEMO === '1') {
      return { ok: true, action: 'updated', name: cmd.name, commands: [demoView(cmd, isActive)], silent: true };
    }

    let commands: CommandView[];
    try {
      ({ commands } = await upsertCommand(uid, { ...cmd, isActive }));
    } catch (e) {
      logRpcFailure('toggle', e);
      return fail(400, { ok: false });
    }

    auditDashboardImpersonation(locals.session, 'command:toggle', `${cmd.name}=${isActive}`);

    return { ok: true, action: 'updated', name: cmd.name, commands, silent: true };
  },

  delete: async ({ request, locals }) => {
    gateCommands(locals.session);
    const uid = effectiveId(locals.session);
    // DEMO runs without a real session; the demo branches below short-circuit
    // before any RPC, so only production requests need the auth gate.
    if (env.DEMO !== '1' && !locals.session) {
      return fail(401, { ok: false, error: 'Not signed in.' });
    }

    const f = await request.formData();
    const name = String(f.get('name') ?? '');

    if (env.DEMO === '1') return { ok: true, action: 'deleted', name };

    let commands: CommandView[];
    try {
      ({ commands } = await deleteCommand(uid, name));
    } catch (e) {
      logRpcFailure('delete', e);
      return fail(400, { ok: false });
    }

    auditDashboardImpersonation(locals.session, 'command:delete', name);

    return { ok: true, action: 'deleted', name, commands };
  },

  // Toggle a built-in command on/off. Built-in state lives in the modules
  // service (key = the built-in id), not the commands service, so this is a
  // separate path from the custom-command toggle. Returns the rebuilt built-in
  // row so the optimistic UI reconciles the same way it does for custom rows.
  toggleBuiltin: async ({ request, locals }) => {
    gateCommands(locals.session);
    const uid = effectiveId(locals.session);
    if (env.DEMO !== '1' && !locals.session) {
      return fail(401, { ok: false, error: 'Not signed in.' });
    }

    const f = await request.formData();
    const name = normName(String(f.get('name') ?? ''));
    const def = builtinDef(name);
    if (!def) return fail(400, { ok: false, error: 'Unknown built-in command.' });
    const isActive = f.get('is_active') === 'on';

    const view: CommandView = {
      name: def.id,
      response: def.summary,
      is_active: isActive,
      perm: def.defaultPerm,
      cooldown: def.defaultCooldown,
      stream_online_only: def.liveOnly,
      builtin: true
    };

    if (env.DEMO === '1') {
      return { ok: true, action: 'updated', name, commands: [view], silent: true };
    }

    try {
      await upsertModule(uid, def.id, isActive);
    } catch (e) {
      logRpcFailure('toggleBuiltin', e);
      return fail(400, { ok: false });
    }

    auditDashboardImpersonation(locals.session, 'command:builtin_toggle', `${name}=${isActive}`);
    return { ok: true, action: 'updated', name, commands: [view], silent: true };
  }
};

// Log the real RPC failure server-side — RpcError / NATS timeout messages can
// carry internal service detail, so they go to the logs, never the dashboard.
// The action returns a generic fail() instead; the client shows its own
// localized "…failed" copy.
function logRpcFailure(action: string, e: unknown): void {
  console.error(`[commands] ${action} failed:`, e instanceof Error ? (e.stack ?? e.message) : e);
}
