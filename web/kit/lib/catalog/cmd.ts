// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef, ModuleField } from './module-def';

const ROLE_OPTIONS = [
  { value: 'everyone', label: 'Everyone' },
  { value: 'sub', label: 'Subscribers' },
  { value: 'vip', label: 'VIPs' },
  { value: 'mod', label: 'Moderators' },
  { value: 'lead_mod', label: 'Lead moderators' },
  { value: 'broadcaster', label: 'Broadcaster' }
];

function roleField(key: string, label: string): ModuleField {
  return { key, label, type: 'select', placeholder: 'mod', options: ROLE_OPTIONS };
}

export const CMD_MODULE: ModuleDef = {
  id: 'cmd',
  label: 'Command Editor',
  tagline: 'Manage custom commands from chat, and choose who can.',
  description:
    'Anyone can type !cmd for a link to your public command list. Your team can also manage custom commands from chat: !cmd add, !cmd edit and !cmd remove change the list, !cmd disable turns a command off and keeps its reply (handy for a speedrun or a focused stream), !cmd enable turns it back on, and !cmd perm sets who can use a command. Pick below who may run each one. All of them start at moderators. Switch the module off to stop !cmd entirely.',
  category: 'Chat',
  defaultEnabled: true,
  delegateSections: ['modules', 'commands'],
  replies: [],
  commands: [
    { trigger: '!cmd', aliases: ['!cmds', '!command', '!commands'], summary: 'Link to your public command list.' },
    { trigger: '!cmd add <name> <response>', summary: 'Create a custom command.' },
    { trigger: '!cmd edit <name> <response>', summary: 'Change a command reply; its other settings stay.' },
    { trigger: '!cmd remove <name>', summary: 'Delete a custom command (also !cmd delete).' },
    { trigger: '!cmd disable <name>', summary: 'Turn a command off without deleting it.' },
    { trigger: '!cmd enable <name>', summary: 'Turn a disabled command back on.' },
    { trigger: '!cmd perm <name> <level>', summary: 'Set who can use a command: everyone, sub, vip, mod, lead_mod or broadcaster.' }
  ],
  settings: [
    roleField('addPerm', 'Who can add'),
    roleField('editPerm', 'Who can edit'),
    roleField('removePerm', 'Who can remove'),
    roleField('disablePerm', 'Who can disable'),
    roleField('enablePerm', 'Who can enable'),
    roleField('permPerm', 'Who can change access')
  ]
};
