// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { BlockedViewer, TtsOverlayView, VoiceAllowance, VoiceSafety } from '#lib/server/services/voice';
import {
  demoVoiceAllowance,
  demoVoiceBlock,
  demoVoiceBlocks,
  demoVoiceOverlay,
  demoVoiceSettingsSave,
  demoVoiceUnblock
} from '../../fixtures';

export * from '#lib/server/services/voice';

export async function voiceGet(): Promise<VoiceAllowance> {
  return demoVoiceAllowance();
}

export async function voiceOverlayGet(): Promise<TtsOverlayView> {
  return demoVoiceOverlay();
}

export async function voiceSet(_session: unknown, save: Pick<TtsOverlayView, 'settings' | 'revision'>): Promise<TtsOverlayView> {
  return demoVoiceSettingsSave(save.settings);
}

export async function voiceOverlayRotate(): Promise<string> {
  return `https://overlay.itsbagelbot.com/tts#demo-${Date.now()}`;
}

export async function voiceBlocksGet(): Promise<BlockedViewer[]> {
  return demoVoiceBlocks();
}

export async function voiceBlock(_session: unknown, viewer: { viewer_id: string; viewer_login: string }): Promise<BlockedViewer[]> {
  return demoVoiceBlock(viewer);
}

export async function voiceUnblock(_session: unknown, viewerId: string): Promise<BlockedViewer[]> {
  return demoVoiceUnblock({ viewerId });
}

export async function voiceSafetyGet(): Promise<VoiceSafety> {
  return { paused_until: 0 };
}

export async function voiceTestAlert(): Promise<'queued'> {
  return 'queued';
}
