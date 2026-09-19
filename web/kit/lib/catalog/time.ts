// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { replyTokens, type ModuleDef } from './module-def';

export const TIME_MODULE: ModuleDef = 
{
  id: 'time',
  label: 'Local Time',
  tagline: 'Viewers ask what time it is for you with !time.',
  description:
    'Viewers type !time and the bot answers with your current local time. They can also ask about another place: !time Tokyo, !time EST, !time UTC+2. Pick your timezone below. The page suggests the one your browser reports, computed on your device only (nothing is read or stored until you save it). Choose a 12- or 24-hour clock and customize both replies.',
  category: 'Chat',
  defaultEnabled: false,
  replies: [
    {
      key: 'time',
      label: '!time',
      tagline: 'Your current local time.',
      event: '!time',
      command: 'time',
      messageKey: 'message',
      defaultMessage: 'It is currently {time} for the streamer.',
      tokens: replyTokens(['time', 'date', 'timezone', 'user'], {
        time: '2:30 PM',
        date: 'Monday, July 13',
        timezone: 'America/Toronto',
        user: 'Viewer'
      })
    },
    {
      key: 'lookup',
      label: '!time <place>',
      tagline: 'Time somewhere else.',
      event: '!time Tokyo',
      command: 'time',
      previewArgs: 'Tokyo',
      messageKey: 'lookupMessage',
      defaultMessage: 'It is currently {time} in {place}.',
      tokens: replyTokens(['time', 'date', 'place', 'timezone', 'user'], {
        time: '3:30 AM',
        date: 'Tuesday, July 14',
        place: 'Tokyo',
        timezone: 'Asia/Tokyo',
        user: 'Viewer'
      })
    }
  ],
  settings: [
    {
      key: 'timezone',
      label: 'Timezone',
      type: 'timezone',
      placeholder: 'America/Toronto',
      help: 'IANA timezone name used to render {time} and {date}. The suggestion comes from your browser and is never stored until you save it.'
    },
    {
      key: 'format',
      label: 'Clock format',
      type: 'select',
      placeholder: '12',
      options: [
        { value: '12', label: '12-hour (2:30 PM)' },
        { value: '24', label: '24-hour (14:30)' }
      ]
    }
  ]
};
