// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { CLOUD_VOICE_CATALOG, VOICE_GROUPS, VOICE_STYLES } from '@bagel/kit/voice-plan';
import type { SelectOption } from '@bagel/ui/lib/select';

type Translate = (key: string, params?: Record<string, string | number>) => string;

const GROUPS = ['energetic', 'warm', 'calm', 'authoritative'] as const;

export function voiceOptions(t: Translate): SelectOption[] {
  return GROUPS.flatMap((group) => CLOUD_VOICE_CATALOG
    .filter(([, character]) => VOICE_GROUPS[character] === group)
    .map(([name, character]) => ({
      value: name,
      label: name,
      description: t(`voice.character.${character.toLowerCase()}`),
      group: t(`voice.group.${group}`),
      searchText: character
    })));
}

export function styleOptions(t: Translate): SelectOption[] {
  return Object.keys(VOICE_STYLES).map((value) => ({
    value,
    label: t(`voice.style.${value}`),
    description: t(`voice.styleDescription.${value}`)
  }));
}
