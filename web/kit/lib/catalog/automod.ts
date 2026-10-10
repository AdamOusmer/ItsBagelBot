// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const AUTOMOD_MODULE: ModuleDef = 
{
  id: 'automod',
  beta: true,
  label: 'AutoMod',
  tagline: 'Your blocklists first, then a panel of AI models on anything uncertain.',
  description:
    'AutoMod checks every chat message against the rules below before anything else: your blocked terms, adult, slur and hate lists, spam phrases, blocked or IP-grabber sites, blocked accounts and the all-links switch. A message that matches one is removed. Anything those rules do not settle goes to a panel of AI models that score toxicity and hate in context. That panel is still learning on live chat: it records what it would do but never removes, times out or bans on its own, so borderline calls stay with your human mods.',
  category: 'Moderation',
  defaultEnabled: true,
  replies: [],
  settings: [
    {
      key: 'block_terms',
      label: 'Blocked terms',
      type: 'textarea',
      placeholder: 'term one, term two',
      help: 'Words or phrases to remove in your channel. Separate with commas or new lines. Matched as whole words, ignoring case.'
    },
    {
      key: 'nsfw_terms',
      label: 'Adult terms',
      type: 'textarea',
      placeholder: 'term one, term two',
      help: 'Sexual or adult words to remove on sight. Separate with commas or new lines.'
    },
    {
      key: 'slur_terms',
      label: 'Slurs',
      type: 'textarea',
      placeholder: 'term one, term two',
      help: 'Slurs your channel never wants to see. Separate with commas or new lines.'
    },
    {
      key: 'racism_terms',
      label: 'Hate terms',
      type: 'textarea',
      placeholder: 'term one, term two',
      help: 'Racist or hateful words and phrases to remove on sight. Separate with commas or new lines.'
    },
    {
      key: 'spam_phrases',
      label: 'Spam phrases',
      type: 'textarea',
      placeholder: 'buy followers, cheap viewers',
      help: 'Phrases only spammers post, like follower or viewer sales pitches. Separate with commas or new lines.'
    },
    {
      key: 'block_links',
      label: 'Block all links',
      type: 'toggle',
      help: 'Removes every message that contains a link, including bare domains like example.com. Off by default.'
    },
    {
      key: 'blocked_domains',
      label: 'Blocked sites',
      type: 'textarea',
      placeholder: 'example.com',
      help: 'Removes links to these sites and all their subdomains. Enter bare domains like example.com, without https:// or a path.'
    },
    {
      key: 'ip_logger_domains',
      label: 'IP-grabber sites',
      type: 'textarea',
      placeholder: 'grabify.link',
      help: 'IP-logger or tracking sites to remove. Enter bare domains like grabify.link.'
    },
    {
      key: 'blocked_accounts',
      label: 'Blocked accounts',
      type: 'textarea',
      placeholder: '123456789',
      help: 'Twitch user IDs (numbers, not usernames) whose messages are always removed. Separate with commas or new lines.'
    }
  ]
};
