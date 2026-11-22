// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { readdirSync, readFileSync } from 'node:fs';
import {
  ASSET_TYPES, WIDGET_KINDS, allowed, assetLimits, overlayLimits, validateDocument,
  type OverlayErrorCode
} from './overlay-document';

const root = new URL('../../../internal/domain/overlays/testdata/', import.meta.url);
const fixture = (name: string): any => JSON.parse(readFileSync(new URL(name, root), 'utf8'));
const codeOf = (input: unknown) => {
  const result = validateDocument(input);
  return result.ok ? null : result.error.code;
};

const widget = (id: string, kind = 'text', settings: object = {}) => ({
  id, name: 'Widget', kind, x: 0, y: 0, w: 100, h: 100, rotation: 0, opacity: 1, hidden: false, locked: false, settings
});
const customDocument = (over: object = {}) => ({
  version: 2, kind: 'custom', id: 'overlay-1', name: 'Main', width: 1920, height: 1080, rev: 0, ...over
});

describe('overlay document fixtures', () => {
  test.each(['valid/system.json', 'valid/custom.json'])('%s is valid', (name) => {
    expect(codeOf(fixture(name))).toBeNull();
  });

  const invalid = readdirSync(new URL('invalid/', root)).filter((name) => name.endsWith('.json'));

  test('invalid fixtures exist', () => {
    expect(invalid.length).toBeGreaterThan(0);
  });

  test.each(invalid)('%s reports its own code', (name) => {
    expect(codeOf(fixture(`invalid/${name}`))).toBe(name.replace(/\.json$/, '') as OverlayErrorCode);
  });
});

describe('overlay document limits', () => {
  const cases: [string, object, OverlayErrorCode][] = [
    ['widget count', customDocument({ widgets: Array.from({ length: overlayLimits.maxWidgets + 1 }, (_, i) => widget(`w${i}`)) }), 'too_many_widgets'],
    ['asset count', customDocument({ assets: Object.fromEntries(Array.from({ length: overlayLimits.overlayAssets + 1 }, (_, i) => [`a${i}`, {}])) }), 'too_many_assets'],
    ['document size', customDocument({ widgets: [widget('w1', 'text', { text: 'a'.repeat(overlayLimits.documentBytes) })] }), 'document_too_large']
  ];

  test.each(cases)('%s', (_name, document, code) => {
    expect(codeOf(document)).toBe(code);
  });
});

describe('premium rule', () => {
  test.each([
    ['system', false, true],
    ['system', true, true],
    ['custom', false, false],
    ['custom', true, true]
  ] as const)('%s premium=%s', (kind, premium, want) => {
    expect(allowed(kind, premium)).toBe(want);
  });
});

describe('pinned limits', () => {
  test('match the table shared with Go', () => {
    const pinned = fixture('limits.json');
    const current = {
      limits: {
        document_bytes: overlayLimits.documentBytes,
        max_widgets: overlayLimits.maxWidgets,
        overlay_asset_bytes: overlayLimits.overlayAssetBytes,
        overlay_assets: overlayLimits.overlayAssets,
        account_bytes: overlayLimits.accountBytes,
        account_overlays: overlayLimits.accountOverlays,
        account_assets: overlayLimits.accountAssets,
        account_uploads_per_hour: overlayLimits.accountUploadsPerHour,
        import_unpacked_bytes: overlayLimits.importUnpackedBytes,
        import_entries: overlayLimits.importEntries,
        import_ratio: overlayLimits.importRatio
      },
      assets: Object.fromEntries(
        ASSET_TYPES.map((type) => [type, {
          max_bytes: assetLimits[type].maxBytes,
          max_width: assetLimits[type].maxWidth,
          max_height: assetLimits[type].maxHeight,
          max_pixels: assetLimits[type].maxPixels,
          max_duration_ms: assetLimits[type].maxDurationMs,
          max_frames: assetLimits[type].maxFrames,
          max_per_overlay: assetLimits[type].maxPerOverlay
        }])
      ),
      widget_kinds: [...WIDGET_KINDS]
    };
    expect(current).toEqual(pinned);
  });
});
