// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { OverlayDocument } from '@bagel/kit/overlay-document';
import type {
  AssetView,
  CustomOverlay,
  FeedAction,
  FeedControlArgs,
  FeedControlState,
  OverlayView,
  PoweredSettings
} from '#lib/server/services/overlays';
import {
  demoAssetDelete,
  demoAssets,
  demoFeedControl,
  demoOverlayList,
  demoOverlayRotate,
  demoPoweredSave,
  demoPoweredView
} from '../../fixtures';

export * from '#lib/server/services/overlays';

const customs = new Map<number, CustomOverlay>();
const CUSTOM_ID_BASE = 100;

function customUrl(id: number): string {
  return `https://overlay.itsbagelbot.com/o#demo-custom-${id}`;
}

export async function overlayList(): Promise<OverlayView[]> {
  const custom = [...customs.values()].map((c): OverlayView => ({ id: c.id, kind: 'custom', name: c.name, revision: c.revision }));
  return [...demoOverlayList(), ...custom];
}

export async function overlayAddTemplate(): Promise<OverlayView> {
  return demoOverlayList()[0];
}

export async function overlayGet(): Promise<PoweredSettings> {
  return demoPoweredView();
}

export async function overlaySave(_session: unknown, save: { config: PoweredSettings['config'] }): Promise<PoweredSettings> {
  demoPoweredSave(save.config);
  return demoPoweredView();
}

export async function overlayRotate(): Promise<OverlayView> {
  const rotated = demoOverlayRotate();
  return { ...demoOverlayList()[0], ...rotated };
}

export async function overlayDelete(): Promise<void> {}

export async function overlayGetCustom(_session: unknown, id: number): Promise<CustomOverlay | null> {
  return structuredClone(customs.get(id) ?? null);
}

export async function overlaySaveCustom(
  _session: unknown,
  save: { id: number; name: string; document: OverlayDocument; revision: number }
): Promise<CustomOverlay> {
  const saved: CustomOverlay = { id: save.id, name: save.name, revision: save.revision + 1, document: save.document as CustomOverlay['document'] };
  customs.set(save.id, saved);
  return structuredClone(saved);
}

export async function overlayCreateCustom(_session: unknown, create: { document: OverlayDocument }): Promise<CustomOverlay> {
  const id = CUSTOM_ID_BASE + customs.size + 1;
  const created: CustomOverlay = { id, name: create.document.name, revision: 1, document: create.document as CustomOverlay['document'], url: customUrl(id) };
  customs.set(id, created);
  return structuredClone(created);
}

export async function assetList(): Promise<AssetView[]> {
  return demoAssets();
}

export async function assetDelete(_session: unknown, assetId: string): Promise<void> {
  demoAssetDelete(assetId);
}

export async function feedControl(_session: unknown, control: { action: FeedAction; args: FeedControlArgs }): Promise<FeedControlState> {
  return demoFeedControl(control);
}
