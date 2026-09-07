// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { HubContent } from '../../lib/guides/types';

const hub: HubContent = {
  meta: {
    title: 'Guides - ItsBagelBot',
    description:
      'Visual guides for ItsBagelBot: set up the bot, learn the dashboard, and write custom commands with variables and live data.',
    eyebrow: 'Guides',
    heading: 'Learn the bot.',
    lead: 'Every guide here works on the free plan, and each one runs ten minutes or less.',
  },
  readCta: 'Read the guide',
  tool: {
    href: '/command-builder',
    eyebrow: 'Interactive tool',
    name: 'Command builder',
    description:
      'Write the message like a sentence, click to drop in the smart parts, watch a live chat rehearsal, then send the finished command to your dashboard.',
    cta: 'Open the builder',
    demoIn: 'Welcome in, <i>&lbrace;user&rbrace;</i>! You are visitor <i>&lbrace;counter:visits&rbrace;</i> 🥯',
    demoOut: '<b>ItsBagelBot:</b> Welcome in, maya_live! You are visitor 128 🥯',
  },
  help: {
    prompt: "Stuck on something a guide doesn't cover?",
    links: [
      { href: 'https://discord.gg/SZ2remwSDv', label: 'Ask on Discord', external: true },
      { href: '/contact', label: 'Contact support' },
      { href: 'https://dashboard.itsbagelbot.com', label: 'Open the dashboard', external: true },
    ],
  },
};

export default hub;
