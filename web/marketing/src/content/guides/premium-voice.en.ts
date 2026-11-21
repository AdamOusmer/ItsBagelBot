// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dashboardHref } from '@bagel/kit/site-links';
import { MONTHLY_VOICE_MINUTES } from '@bagel/kit/voice-plan';
import type { GuideContent } from '../../lib/guides/types';

const guide: GuideContent = {
  slug: 'premium-voice',
  meta: {
    aiDoc: 'https://ai.itsbagelbot.com/features/premium-voice.md',
    title: 'Premium Voice Alerts in OBS (Beta) | ItsBagelBot',
    description: 'Set up spoken Twitch alerts in OBS with Premium voice. Create a browser source link, choose voices and styles, and check your monthly minutes.',
    eyebrow: 'Guide',
    heading: 'Premium voice alerts in OBS',
    lead: 'Use cloud voices to read Twitch alerts aloud in OBS with Premium voice (Beta).',
    minutes: '5 min read',
    card: {
      title: 'Premium voice alerts in OBS',
      description: 'Add spoken alerts to OBS and choose which Twitch events your viewers hear.',
      meta: '5 min · 5 steps',
      chips: ['OBS', 'Premium', 'Beta']
    }
  },
  sections: [
    {
      id: 'allowance',
      heading: 'Who can use Premium voice',
      blocks: [
        { kind: 'prose', html: '<p>Cloud voices are available to channel owners with Premium access, including VIP. The Premium voice page shows how much speech you have used and when your allowance resets.</p>' },
        {
          kind: 'table',
          head: ['Access', 'Minutes per month'],
          rows: [
            ['Premium', MONTHLY_VOICE_MINUTES.premium],
            ['Giveaway Premium', MONTHLY_VOICE_MINUTES.giveaway],
            ['Admin grant', MONTHLY_VOICE_MINUTES.admin_grant],
            ['VIP', MONTHLY_VOICE_MINUTES.vip]
          ]
        }
      ]
    },
    {
      id: 'setup',
      heading: 'Set up spoken alerts',
      blocks: [
        {
          kind: 'steps',
          items: [
            {
              title: 'Open Premium voice in the dashboard',
              html: `<p>Sign in as the channel owner and open <a href="${dashboardHref('/voice', '')}">Premium voice</a>. Turn on <b>Use premium voice for alerts</b> and save.</p>`
            },
            {
              title: 'Create your OBS link',
              html: '<p>Under <b>OBS browser source</b>, choose <b>Create link</b> and copy the full URL, including the part after <code>#</code>. The dashboard shows it once.</p>'
            },
            {
              title: 'Add a Browser Source in OBS',
              html: '<p>In your scene, add a <b>Browser</b> source and paste the link into the <b>URL</b> field. Set the width and height to a small size such as 320 × 80. Tick <b>Control audio via OBS</b>, then save the source.</p>'
            },
            {
              title: 'Choose what the voice reads',
              html: '<p>Pick a default voice. Enable the alerts you want spoken: follows, subscriptions, gifted subs, Cheers, or raids. For each alert, you can edit the spoken message, pick a style, and keep the default voice or choose another. Set the minimum Bits for Cheers and decide whether to read messages from Cheers and resubs. Keep live-only on to speak alerts only while streaming, then save.</p>'
            },
            {
              title: 'Test one alert',
              html: '<p>Activate the scene in OBS and trigger one of the alert types you enabled. If live-only is on, your channel needs to be live for the test. Check that you hear the voice and see the source’s level move in the OBS Audio Mixer.</p>'
            }
          ]
        },
        {
          kind: 'callout',
          tone: 'warn',
          html: '<b>Keep the OBS link private.</b> If it leaks, choose <b>Make a new link</b> in the dashboard. That revokes the old link. Paste the replacement into each OBS source that used it.'
        }
      ]
    },
    {
      id: 'troubleshooting',
      heading: 'Troubleshoot spoken alerts',
      blocks: [
        {
          kind: 'table',
          head: ['What you see', 'What to do'],
          rows: [
            ['OBS is silent', 'Confirm that Control audio via OBS is checked and the source is unmuted in the Audio Mixer. If autoplay is blocked, right-click the source, choose Interact, and select Click to enable sound when it appears.'],
            ['Alerts stopped after you made a new link', 'Replace the URL in OBS with the full new link. The previous link has been revoked.'],
            ['You have used all your minutes', 'Check the reset date on Premium voice. Cloud speech resumes when your monthly allowance resets.'],
            ['An alert ends early', 'Shorten its spoken message or raise Max length per alert. You can set the limit from 5 to 120 seconds. A low remaining monthly balance can also shorten a clip.']
          ]
        }
      ]
    }
  ]
};

export default guide;
