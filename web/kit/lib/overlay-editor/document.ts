// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { OVERLAY_VERSION, overlayLimits, type OverlayAsset, type OverlayDocument, type OverlayWidget } from '../overlay-document';
import type { Canvas } from './defaults';

export const ASSET_PREFIX = 'asset:';
export const DEFAULT_CANVAS: Canvas = { width: 1920, height: 1080 };

export type ServedDocument = Omit<OverlayDocument, 'assets'> & { assets?: Record<string, OverlayAsset> | null };

export const assetRef = (id: string): string => `${ASSET_PREFIX}${id}`;

export function refAssetId(value: unknown): string | undefined {
  return typeof value === 'string' && value.startsWith(ASSET_PREFIX) ? value.slice(ASSET_PREFIX.length) : undefined;
}

function collectRefs(value: unknown, found: Set<string>): Set<string> {
  const id = refAssetId(value);
  if (id !== undefined) found.add(id);
  if (typeof value !== 'object' || value === null) return found;
  for (const child of Object.values(value)) collectRefs(child, found);
  return found;
}

export function referencedAssets(widgets: readonly OverlayWidget[]): Set<string> {
  return widgets.reduce((found, widget) => collectRefs(widget.settings, found), new Set<string>());
}

export function pruneAssets(doc: OverlayDocument): OverlayDocument {
  const used = referencedAssets(doc.widgets);
  const assets = Object.fromEntries(Object.entries(doc.assets).filter(([id]) => used.has(id)));
  return { ...doc, assets };
}

export const toWireDocument = pruneAssets;

export const fromServed = (served: ServedDocument): OverlayDocument => ({ ...served, assets: served.assets ?? {} });

export function blankDocument(name: string, canvas: Canvas = DEFAULT_CANVAS): OverlayDocument {
  return {
    version: OVERLAY_VERSION, kind: 'custom', id: crypto.randomUUID(), name, rev: 0,
    width: canvas.width, height: canvas.height, template: '', config: null, widgets: [], assets: {}
  };
}

export const widgetLimitReached = (doc: OverlayDocument): boolean => doc.widgets.length >= overlayLimits.maxWidgets;
