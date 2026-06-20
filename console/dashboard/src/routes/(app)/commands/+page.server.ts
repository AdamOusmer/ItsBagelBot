import type { Actions, PageServerLoad } from './$types';
import type { CommandView, Perm } from '@bagel/shared';
import { PERMS } from '@bagel/shared';
import { listCommands, upsertCommand, deleteCommand, auditDashboardImpersonation } from '$lib/server/rpc';
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

const sample: CommandView[] = [
  { name: '!uptime', aliases: ['!live', '!up'], response: '@{user} the stream has been live for {uptime} 🥯', perm: 'everyone', cooldown: 5, uses: '412', is_active: true, stream_online_only: true },
  { name: '!socials', aliases: ['!social', '!links'], response: 'Follow along → twitch.tv/itsmavey · @itsmavey everywhere', perm: 'everyone', cooldown: 30, uses: '288', is_active: true },
  { name: '!bagel', response: '{user} tosses a warm bagel to {target}. Toasty.', perm: 'everyone', cooldown: 10, uses: '1.2k', is_active: true },
  { name: '!so', response: 'Go show some love to twitch.tv/{target} — absolute legend', perm: 'mod', cooldown: 0, uses: '96', is_active: true },
  { name: '!discord', response: 'Join the bakery → discord.gg/itsbagelbot', perm: 'everyone', cooldown: 60, uses: '203', is_active: true },
  { name: '!uptime-debug', response: 'node={node} replica={id} lag={ms}ms', perm: 'broadcaster', cooldown: 0, uses: '14', is_active: false },
  { name: '!lurk', response: '{user} fades into the shadows. Thanks for the lurk.', perm: 'everyone', cooldown: 5, uses: '521', is_active: true },
  { name: '!followage', response: "@{user} you've followed for {followage} 🥯", perm: 'sub', cooldown: 15, uses: '177', is_active: true }
];

export const load: PageServerLoad = async ({ locals }) => {
  gateCommands(locals.session);
  const uid = effectiveId(locals.session);
  if (env.DEMO === '1') return { commands: sample };
  try {
    return { commands: await listCommands(uid) };
  } catch {
    // Don't show fabricated rows in production; surface a degraded state.
    return { commands: [] as CommandView[], degraded: true };
  }
};

// Parses and normalizes the shared command fields out of a submitted form.
function parseCommand(f: FormData) {
  const name = String(f.get('name') ?? '').trim();

  // Alternate names arrive as repeated `aliases` fields. Trim, drop blanks, and
  // de-duplicate case-insensitively so the wire payload matches what the
  // commands service will accept.
  const seen = new Set<string>();
  const aliases: string[] = [];
  for (const raw of f.getAll('aliases')) {
    const a = String(raw).trim();
    if (!a) continue;
    const key = a.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
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

export const actions: Actions = {
  save: async ({ request, locals }) => {
    gateCommands(locals.session);
    const uid = effectiveId(locals.session);
    if (!locals.session) return fail(401, { ok: false, error: 'Not signed in.' });

    const f = await request.formData();
    const cmd = parseCommand(f);
    const isActive = f.get('is_active') === 'on';
    const isEdit = f.get('edit') === '1';
    const originalName = String(f.get('original_name') ?? '').trim();
    const renamed = isEdit && originalName !== '' && originalName !== cmd.name;

    if (!cmd.name) return fail(400, { ok: false, error: 'Command name is required.' });
    if (!cmd.response) return fail(400, { ok: false, error: 'Response is required.' });

    // A rename passes original_name so the commands service updates the row's
    // name field in place (single write) instead of delete-old + create-new.
    // The optimistic reply already drops the old key and lists the renamed
    // command, so one round trip covers it.
    const { commands, error } = await upsertCommand(
      uid,
      { ...cmd, isActive },
      renamed ? originalName : undefined
    );
    if (error) return fail(400, { ok: false, error, commands });

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
    if (!locals.session) return fail(401, { ok: false, error: 'Not signed in.' });

    const f = await request.formData();
    const cmd = parseCommand(f);
    const isActive = f.get('is_active') === 'on';

    const { commands, error } = await upsertCommand(uid, { ...cmd, isActive });
    if (error) return fail(400, { ok: false, error, commands });

    auditDashboardImpersonation(locals.session, 'command:toggle', `${cmd.name}=${isActive}`);

    return { ok: true, action: 'updated', name: cmd.name, commands, silent: true };
  },

  delete: async ({ request, locals }) => {
    gateCommands(locals.session);
    const uid = effectiveId(locals.session);
    if (!locals.session) return fail(401, { ok: false, error: 'Not signed in.' });

    const f = await request.formData();
    const name = String(f.get('name') ?? '');
    const { commands, error } = await deleteCommand(uid, name);
    if (error) return fail(400, { ok: false, error, commands });

    auditDashboardImpersonation(locals.session, 'command:delete', name);

    return { ok: true, action: 'deleted', name, commands };
  }
};
