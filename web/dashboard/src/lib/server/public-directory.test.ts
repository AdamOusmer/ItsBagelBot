import { describe, expect, mock, test } from 'bun:test';

mock.module('@bagel/kit', () => {
  const module = {
    id: 'queue',
    label: 'Queue',
    tagline: 'Manage the queue.',
    defaultEnabled: true,
    toggleable: true,
    hidden: false,
    replies: [
      { key: 'join', command: 'join', label: 'Join', tagline: 'Join the queue.', event: 'on join' }
    ]
  };
  const fr: Record<string, string> = {
    'modules.catalog.queue.label': 'File d’attente',
    'modules.catalog.queue.tagline': 'Gérez la file.',
    'modules.catalog.queue.replies.join.tagline': 'Rejoindre la file.',
    'perm.sub': 'Abonnés'
  };
  return {
    BUILTIN_COMMANDS: [],
    MODULE_CATALOG: [module],
    PERM_LABELS: { everyone: 'Everyone', sub: 'Subs' },
    translate: (locale: string, key: string) => locale === 'fr' ? (fr[key] ?? key) : key,
    translateList: (locale: string, key: string) => locale === 'fr' && key === 'builtinDirectory.clip.usage' ? ['!clip', '!clip <titre>'] : [],
    tModuleLabel: (t: (key: string) => string, def: { id: string; label: string }) => t(`modules.catalog.${def.id}.label`) || def.label,
    tModuleTagline: (t: (key: string) => string, def: { id: string; tagline: string }) => t(`modules.catalog.${def.id}.tagline`) || def.tagline,
    tModuleReplyPart: (t: (key: string) => string, id: string, reply: { key: string; tagline: string; label: string; event: string }, part: string) => t(`modules.catalog.${id}.replies.${reply.key}.${part}`) || reply[part as keyof typeof reply]
  };
});

const { publicCommands, publicModules } = await import('./public-directory');

describe('public directory locale shaping', () => {
  test('localizes catalog module metadata and reply event copy', () => {
    const module = publicModules([], 'fr').find((entry) => entry.id === 'queue');
    expect(module?.label).toBe('File d’attente');
    expect(module?.tagline).toContain('file');
    expect(module?.commands.find((command) => command.label === '!join')?.meta).toContain('file');
  });

  test('localizes finite permission labels while preserving custom response text', () => {
    const rows = publicCommands([
      {
        name: 'hello',
        aliases: [],
        response: 'Welcome to the channel!',
        perm: 'sub',
        cooldown: 0,
        stream_online_only: false,
        uses: 0,
        is_active: true
      }
    ], 'fr');
    expect(rows[0]?.perm).toBe('Abonnés');
    expect(rows[0]?.response).toBe('Welcome to the channel!');
  });
});
