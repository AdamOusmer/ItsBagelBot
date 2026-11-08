// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { Call } from '@wailsio/runtime';
import type { OverlayScene } from './types';

// Desktop-only write boundary. The OBS bundle never imports this module.
export function readScene(): Promise<OverlayScene> {
  return Call.ByName('main.OverlayService.Current') as Promise<OverlayScene>;
}

export function sourceURL(): Promise<string> {
  return Call.ByName('main.OverlayService.SourceURL') as Promise<string>;
}

export function publishScene(scene: OverlayScene): Promise<void> {
  return Call.ByName('main.OverlayService.Publish', JSON.stringify(scene)) as Promise<void>;
}

export function resetSourceURL(): Promise<string> {
  return Call.ByName('main.OverlayService.ResetSourceURL') as Promise<string>;
}
