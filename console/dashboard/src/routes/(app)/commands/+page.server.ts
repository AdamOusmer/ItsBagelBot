import type { Actions, PageServerLoad } from './$types';
import type { CommandView } from '@bagel/shared';
import { listCommands, upsertCommand, deleteCommand } from '$lib/server/rpc';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';

const sample: CommandView[] = [
  { name: '!uptime', response: '@{user} the stream has been live for {uptime} 🥯', perm: 'everyone', cooldown: '5s', uses: '412', is_active: true },
  { name: '!socials', response: 'Follow along → twitch.tv/itsmavey · @itsmavey everywhere', perm: 'everyone', cooldown: '30s', uses: '288', is_active: true },
  { name: '!bagel', response: '{user} tosses a warm bagel to {target}. Toasty.', perm: 'everyone', cooldown: '10s', uses: '1.2k', is_active: true },
  { name: '!so', response: 'Go show some love to twitch.tv/{target} — absolute legend', perm: 'mod', cooldown: '0s', uses: '96', is_active: true },
  { name: '!discord', response: 'Join the bakery → discord.gg/itsbagelbot', perm: 'everyone', cooldown: '60s', uses: '203', is_active: true },
  { name: '!uptime-debug', response: 'node={node} replica={id} lag={ms}ms', perm: 'broadcaster', cooldown: '0s', uses: '14', is_active: false },
  { name: '!lurk', response: '{user} fades into the shadows. Thanks for the lurk.', perm: 'everyone', cooldown: '5s', uses: '521', is_active: true },
  { name: '!followage', response: "@{user} you've followed for {followage} 🥯", perm: 'sub', cooldown: '15s', uses: '177', is_active: true }
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

export const actions: Actions = {
  save: async ({ request, locals }) => {
    const uid = locals.session?.user_id;
    if (!uid) return fail(401);
    const f = await request.formData();
    const name = String(f.get('name') ?? '').trim();
    const response = String(f.get('response') ?? '');
    if (!name) return fail(400, { error: 'name required' });
    const commands = await upsertCommand(uid, name, response, f.get('is_active') === 'on');
    return { commands };
  },
  delete: async ({ request, locals }) => {
    const uid = locals.session?.user_id;
    if (!uid) return fail(401);
    const f = await request.formData();
    const commands = await deleteCommand(uid, String(f.get('name') ?? ''));
    return { commands };
  }
};
