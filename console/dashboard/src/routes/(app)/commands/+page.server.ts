import type { Actions, PageServerLoad } from './$types';
import type { CommandView, Perm } from '@bagel/shared';
import { PERMS } from '@bagel/shared';
import { listCommands, upsertCommand, deleteCommand } from '$lib/server/rpc';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';

const sample: CommandView[] = [
  { name: '!uptime', response: '@{user} the stream has been live for {uptime} 🥯', perm: 'everyone', cooldown: 5, uses: '412', is_active: true },
  { name: '!socials', response: 'Follow along → twitch.tv/itsmavey · @itsmavey everywhere', perm: 'everyone', cooldown: 30, uses: '288', is_active: true },
  { name: '!bagel', response: '{user} tosses a warm bagel to {target}. Toasty.', perm: 'everyone', cooldown: 10, uses: '1.2k', is_active: true },
  { name: '!so', response: 'Go show some love to twitch.tv/{target} — absolute legend', perm: 'mod', cooldown: 0, uses: '96', is_active: true },
  { name: '!discord', response: 'Join the bakery → discord.gg/itsbagelbot', perm: 'everyone', cooldown: 60, uses: '203', is_active: true },
  { name: '!uptime-debug', response: 'node={node} replica={id} lag={ms}ms', perm: 'broadcaster', cooldown: 0, uses: '14', is_active: false },
  { name: '!lurk', response: '{user} fades into the shadows. Thanks for the lurk.', perm: 'everyone', cooldown: 5, uses: '521', is_active: true },
  { name: '!followage', response: "@{user} you've followed for {followage} 🥯", perm: 'sub', cooldown: 15, uses: '177', is_active: true }
];

export const load: PageServerLoad = async ({ locals }) => {
  const uid = locals.session?.user_id ?? 'demo';
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
  const response = String(f.get('response') ?? '');
  const permRaw = String(f.get('perm') ?? 'everyone');
  const perm: Perm = (PERMS as readonly string[]).includes(permRaw) ? (permRaw as Perm) : 'everyone';

  // Cooldown arrives as a string; clamp to a sane non-negative integer.
  const cooldown = Math.max(0, Math.floor(Number(f.get('cooldown') ?? 0) || 0));

  // Optional single-user lock; keep digits only so a stray "@name" can't slip through.
  const allowedUserId = String(f.get('allowed_user_id') ?? '').replace(/\D/g, '');

  return { name, response, perm, cooldown, allowedUserId };
}

export const actions: Actions = {
  save: async ({ request, locals }) => {
    const uid = locals.session?.user_id;
    if (!uid) return fail(401, { ok: false, error: 'Not signed in.' });

    const f = await request.formData();
    const cmd = parseCommand(f);
    const isActive = f.get('is_active') === 'on';
    const isEdit = f.get('edit') === '1';
    const originalName = String(f.get('original_name') ?? '').trim();
    const renamed = isEdit && originalName !== '' && originalName !== cmd.name;

    if (!cmd.name) return fail(400, { ok: false, error: 'Command name is required.' });
    if (!cmd.response) return fail(400, { ok: false, error: 'Response is required.' });

    // Name is the row key (user_id, name), so a rename is delete-old + create-new.
    // Write the new row first; its optimistic reply already lists the renamed
    // command (the old delete is immediate but the new write is behind a ~2s
    // batch, so deleteCommand's fresh DB list wouldn't include it yet).
    const { commands, error } = await upsertCommand(uid, { ...cmd, isActive });
    if (error) return fail(400, { ok: false, error, commands });

    if (renamed) {
      const del = await deleteCommand(uid, originalName);
      if (del.error) return fail(400, { ok: false, error: del.error, commands });
      // Drop the old key from the optimistic list returned by the upsert.
      return {
        ok: true,
        action: 'updated',
        name: cmd.name,
        commands: commands.filter((c) => c.name !== originalName)
      };
    }

    return { ok: true, action: isEdit ? 'updated' : 'created', name: cmd.name, commands };
  },

  // Lightweight toggle: flips is_active without going through the full editor.
  toggle: async ({ request, locals }) => {
    const uid = locals.session?.user_id;
    if (!uid) return fail(401, { ok: false, error: 'Not signed in.' });

    const f = await request.formData();
    const cmd = parseCommand(f);
    const isActive = f.get('is_active') === 'on';

    const { commands, error } = await upsertCommand(uid, { ...cmd, isActive });
    if (error) return fail(400, { ok: false, error, commands });

    return { ok: true, action: 'updated', name: cmd.name, commands, silent: true };
  },

  delete: async ({ request, locals }) => {
    const uid = locals.session?.user_id;
    if (!uid) return fail(401, { ok: false, error: 'Not signed in.' });

    const f = await request.formData();
    const name = String(f.get('name') ?? '');
    const { commands, error } = await deleteCommand(uid, name);
    if (error) return fail(400, { ok: false, error, commands });

    return { ok: true, action: 'deleted', name, commands };
  }
};
