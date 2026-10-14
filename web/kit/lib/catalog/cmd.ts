// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const CMD_MODULE: ModuleDef = {
  id: 'cmd',
  label: 'Command Editor',
  tagline: 'Manage custom commands from chat, and choose who can.',
  description:
    'Anyone can type !cmd for a link to your public command list. Your team can also manage custom commands from chat: !cmd add, !cmd edit and !cmd remove change the list, !cmd disable turns a command off and keeps its reply (handy for a speedrun or a focused stream), !cmd enable turns it back on, and !cmd perm sets who can use a command. Open a command below to rehearse it and pick who may run it. All of them start at moderators, and Everyone is never an option. Switch the module off to stop !cmd entirely.',
  category: 'Moderation',
  defaultEnabled: true,
  delegateSections: ['modules', 'commands'],
  replies: [],
  commands: [
    {
      trigger: '!cmd',
      aliases: ['!cmds', '!command', '!commands'],
      summary: 'Link to your public command list.',
      preview: "@{user} browse {channel}'s commands here: {url}"
    },
    {
      trigger: '!cmd add <name> <response>',
      summary: 'Create a custom command.',
      perm: 'mod',
      permKey: 'addPerm',
      previewArgs: "add hype Let's go!",
      preview: '@{user} the command {command} has been added'
    },
    {
      trigger: '!cmd edit <name> <response>',
      summary: 'Change a command reply; its other settings stay.',
      perm: 'mod',
      permKey: 'editPerm',
      previewArgs: 'edit hype Here we go!',
      preview: '@{user} the command {command} has been modified'
    },
    {
      trigger: '!cmd remove <name>',
      summary: 'Delete a custom command (also !cmd delete).',
      perm: 'mod',
      permKey: 'removePerm',
      previewArgs: 'remove hype',
      preview: '@{user} the command {command} has been removed'
    },
    {
      trigger: '!cmd disable <name>',
      summary: 'Turn a command off without deleting it.',
      perm: 'mod',
      permKey: 'disablePerm',
      previewArgs: 'disable hype',
      preview: '@{user} the command {command} is now disabled'
    },
    {
      trigger: '!cmd enable <name>',
      summary: 'Turn a disabled command back on.',
      perm: 'mod',
      permKey: 'enablePerm',
      previewArgs: 'enable hype',
      preview: '@{user} the command {command} is now enabled'
    },
    {
      trigger: '!cmd perm <name> <level>',
      summary: 'Set who can use a command: everyone, sub, vip, mod, lead_mod or broadcaster.',
      perm: 'mod',
      permKey: 'permPerm',
      previewArgs: 'perm hype vip',
      preview: '@{user} who can use {command} is now set to {role}'
    }
  ]
};
