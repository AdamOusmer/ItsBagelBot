// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { poweredConfigError, type PoweredConfig } from './overlays';

export const OVERLAY_VERSION = 2;

export const overlayLimits = {
  documentBytes: 256 * 1024,
  maxWidgets: 100,
  overlayAssetBytes: 250 * 1024 * 1024,
  overlayAssets: 200,
  accountBytes: 1024 * 1024 * 1024,
  accountOverlays: 20,
  accountAssets: 500,
  accountUploadsPerHour: 60,
  importUnpackedBytes: 500 * 1024 * 1024,
  importEntries: 300,
  importRatio: 50
} as const;

export const ASSET_TYPES = ['png', 'webp', 'jpeg', 'gif', 'webm', 'mp3', 'ogg', 'wav', 'woff2', 'widget'] as const;
export type AssetType = (typeof ASSET_TYPES)[number];

export interface AssetLimit {
  maxBytes: number;
  maxWidth: number;
  maxHeight: number;
  maxPixels: number;
  maxDurationMs: number;
  maxFrames: number;
  maxPerOverlay: number;
}

const MIB = 1024 * 1024;
const NONE: AssetLimit = { maxBytes: 0, maxWidth: 0, maxHeight: 0, maxPixels: 0, maxDurationMs: 0, maxFrames: 0, maxPerOverlay: 0 };
const IMAGE: AssetLimit = { ...NONE, maxBytes: 10 * MIB, maxWidth: 4096, maxHeight: 4096, maxPixels: 1 << 24 };
const AUDIO: AssetLimit = { ...NONE, maxBytes: 5 * MIB, maxDurationMs: 30_000 };

export const assetLimits: Record<AssetType, AssetLimit> = {
  png: IMAGE,
  webp: IMAGE,
  jpeg: IMAGE,
  gif: { ...NONE, maxBytes: 10 * MIB, maxWidth: 1920, maxHeight: 1080, maxFrames: 600 },
  webm: { ...NONE, maxBytes: 50 * MIB, maxWidth: 1920, maxHeight: 1920, maxDurationMs: 120_000 },
  mp3: AUDIO,
  ogg: AUDIO,
  wav: AUDIO,
  woff2: { ...NONE, maxBytes: MIB, maxPerOverlay: 4 },
  widget: { ...NONE, maxBytes: 512 * 1024 }
};

export const DOCUMENT_KINDS = ['system', 'custom'] as const;
export type DocumentKind = (typeof DOCUMENT_KINDS)[number];
export const OVERLAY_TEMPLATES = ['powered'] as const;
export const WIDGET_KINDS = [
  'alertbox', 'eventlist', 'goal', 'label', 'chatbox', 'emotewall', 'timer',
  'credits', 'text', 'image', 'video', 'slideshow', 'custom'
] as const;
export type WidgetKind = (typeof WIDGET_KINDS)[number];

export function allowed(kind: DocumentKind, premium: boolean): boolean {
  return kind === 'system' || premium;
}

export const OVERLAY_ERROR_CODES = [
  'invalid_document', 'unsupported_version', 'unknown_kind', 'invalid_id', 'invalid_name', 'invalid_revision',
  'invalid_size', 'unknown_template', 'invalid_config', 'system_has_content', 'too_many_widgets',
  'duplicate_widget_id', 'unknown_widget_kind', 'invalid_widget', 'invalid_bundle',
  'unknown_asset', 'legacy_media_ref', 'unsupported_media', 'invalid_asset', 'asset_too_large', 'asset_dimensions',
  'asset_duration', 'too_many_assets', 'too_many_of_type', 'overlay_assets_too_large', 'document_too_large'
] as const;
export type OverlayErrorCode = (typeof OVERLAY_ERROR_CODES)[number];
export interface OverlayError {
  code: OverlayErrorCode;
  path: string;
}

export interface OverlayAsset {
  type: AssetType;
  bytes: number;
  w: number;
  h: number;
  duration_ms: number;
  sha256: string;
}

export interface OverlayWidget {
  id: string;
  name: string;
  kind: WidgetKind;
  x: number;
  y: number;
  w: number;
  h: number;
  rotation: number;
  opacity: number;
  hidden: boolean;
  locked: boolean;
  settings: Record<string, unknown> | null;
}

export interface OverlayDocument {
  version: number;
  kind: DocumentKind;
  id: string;
  name: string;
  width: number;
  height: number;
  rev: number;
  template: string;
  config: PoweredConfig | null;
  widgets: OverlayWidget[];
  assets: Record<string, OverlayAsset>;
}

export type ValidationResult = { ok: true; doc: OverlayDocument } | { ok: false; error: OverlayError };

type Obj = Record<string, unknown>;
type Primitive = 'string' | 'number' | 'boolean';

const ASSET_PREFIX = 'asset:';
const MAX_NAME = 120;
const MIN_CANVAS = 320;
const MAX_CANVAS = 7680;
const MAX_COORD = 7680;
const MAX_ROTATION = 360;
const ID_PATTERN = /^[a-zA-Z0-9_-]{1,96}$/;
const HASH_PATTERN = /^[a-f0-9]{64}$/;
const LEGACY_MEDIA = /^\/media\/[a-f0-9]{64}\.(?:png|jpe?g|gif|webp|mp4|webm|mp3|ogg|wav)$/;

const DOCUMENT_KEYS = ['version', 'kind', 'id', 'name', 'width', 'height', 'rev', 'template', 'config', 'widgets', 'assets'];
const WIDGET_KEYS = ['id', 'name', 'kind', 'x', 'y', 'w', 'h', 'rotation', 'opacity', 'hidden', 'locked', 'settings'];
const ASSET_KEYS = ['type', 'bytes', 'w', 'h', 'duration_ms', 'sha256'];
const CONFIG_KEYS = ['every_minutes', 'show_seconds', 'directions', 'align'];
const ZERO: Record<Primitive, string | number | boolean> = { string: '', number: 0, boolean: false };

class Failure {
  constructor(readonly error: OverlayError) {}
}

function fail(code: OverlayErrorCode, path = ''): never {
  throw new Failure({ code, path });
}

const field = (base: string, key: string): string => (base ? `${base}.${key}` : key);
const index = (base: string, position: number): string => `${base}[${position}]`;
const within = (value: number, low: number, high: number): boolean => value >= low && value <= high;
const byteLength = (text: string): number => new TextEncoder().encode(text).length;

function record(value: unknown, path: string, keys?: readonly string[]): Obj {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) fail('invalid_document', path);
  const obj = value as Obj;
  if (keys && Object.keys(obj).some((key) => !keys.includes(key))) fail('invalid_document', path);
  return obj;
}

function read(obj: Obj, key: string, type: Primitive, path: string): unknown {
  const value = obj[key];
  if (value == null) return ZERO[type];
  if (typeof value !== type) fail('invalid_document', field(path, key));
  return value;
}

const text = (obj: Obj, key: string, path: string): string => read(obj, key, 'string', path) as string;
const decimal = (obj: Obj, key: string, path: string): number => read(obj, key, 'number', path) as number;
const flag = (obj: Obj, key: string, path: string): boolean => read(obj, key, 'boolean', path) as boolean;

function integer(obj: Obj, key: string, path: string): number {
  const value = decimal(obj, key, path);
  if (!Number.isInteger(value)) fail('invalid_document', field(path, key));
  return value;
}

function list(obj: Obj, key: string, path: string): unknown[] {
  const value = obj[key];
  if (value == null) return [];
  if (!Array.isArray(value)) fail('invalid_document', field(path, key));
  return value;
}

function decodeConfig(value: unknown): PoweredConfig {
  const path = 'config';
  const obj = record(value, path, CONFIG_KEYS);
  const directions = list(obj, 'directions', path);
  if (directions.some((entry) => typeof entry !== 'string')) fail('invalid_document', field(path, 'directions'));
  return {
    every_minutes: integer(obj, 'every_minutes', path),
    show_seconds: integer(obj, 'show_seconds', path),
    directions: directions as PoweredConfig['directions'],
    align: text(obj, 'align', path) as PoweredConfig['align']
  };
}

function decodeWidget(value: unknown, path: string): OverlayWidget {
  const obj = record(value, path, WIDGET_KEYS);
  return {
    id: text(obj, 'id', path),
    name: text(obj, 'name', path),
    kind: text(obj, 'kind', path) as WidgetKind,
    x: decimal(obj, 'x', path),
    y: decimal(obj, 'y', path),
    w: decimal(obj, 'w', path),
    h: decimal(obj, 'h', path),
    rotation: decimal(obj, 'rotation', path),
    opacity: decimal(obj, 'opacity', path),
    hidden: flag(obj, 'hidden', path),
    locked: flag(obj, 'locked', path),
    settings: obj.settings == null ? null : record(obj.settings, field(path, 'settings'))
  };
}

function decodeAsset(value: unknown, path: string): OverlayAsset {
  const obj = record(value, path, ASSET_KEYS);
  return {
    type: text(obj, 'type', path) as AssetType,
    bytes: integer(obj, 'bytes', path),
    w: integer(obj, 'w', path),
    h: integer(obj, 'h', path),
    duration_ms: integer(obj, 'duration_ms', path),
    sha256: text(obj, 'sha256', path)
  };
}

function decodeAssets(value: unknown): Record<string, OverlayAsset> {
  if (value == null) return {};
  const obj = record(value, 'assets');
  return Object.fromEntries(Object.entries(obj).map(([id, asset]) => [id, decodeAsset(asset, field('assets', id))]));
}

function decodeDocument(input: unknown): OverlayDocument {
  const obj = record(input, '', DOCUMENT_KEYS);
  return {
    version: integer(obj, 'version', ''),
    kind: text(obj, 'kind', '') as DocumentKind,
    id: text(obj, 'id', ''),
    name: text(obj, 'name', ''),
    width: integer(obj, 'width', ''),
    height: integer(obj, 'height', ''),
    rev: integer(obj, 'rev', ''),
    template: text(obj, 'template', ''),
    config: obj.config == null ? null : decodeConfig(obj.config),
    widgets: list(obj, 'widgets', '').map((widget, position) => decodeWidget(widget, index('widgets', position))),
    assets: decodeAssets(obj.assets)
  };
}

function checkIdentity(doc: OverlayDocument): void {
  if (doc.version !== OVERLAY_VERSION) fail('unsupported_version', 'version');
  if (!(DOCUMENT_KINDS as readonly string[]).includes(doc.kind)) fail('unknown_kind', 'kind');
  if (!ID_PATTERN.test(doc.id)) fail('invalid_id', 'id');
  if (!within(Array.from(doc.name.trim()).length, 1, MAX_NAME)) fail('invalid_name', 'name');
  if (doc.rev < 0) fail('invalid_revision', 'rev');
}

function checkSystem(doc: OverlayDocument): void {
  const content = [doc.width !== 0, doc.height !== 0, doc.widgets.length > 0, Object.keys(doc.assets).length > 0];
  if (content.includes(true)) fail('system_has_content');
  if (doc.template !== 'powered') fail('unknown_template', 'template');
  if (doc.config === null || poweredConfigError(doc.config) !== null) fail('invalid_config', 'config');
}

function checkWidgetBox(widget: OverlayWidget, path: string): void {
  const bounds: [string, number, number, number][] = [
    ['x', widget.x, 0, MAX_COORD], ['y', widget.y, 0, MAX_COORD],
    ['w', widget.w, 1, MAX_COORD], ['h', widget.h, 1, MAX_COORD],
    ['rotation', widget.rotation, -MAX_ROTATION, MAX_ROTATION], ['opacity', widget.opacity, 0, 1]
  ];
  for (const [name, value, low, high] of bounds) {
    if (!within(value, low, high)) fail('invalid_widget', field(path, name));
  }
  if (!within(Array.from(widget.name).length, 1, MAX_NAME)) fail('invalid_widget', field(path, 'name'));
}

function checkWidget(widget: OverlayWidget, path: string): void {
  if (!ID_PATTERN.test(widget.id)) fail('invalid_id', field(path, 'id'));
  if (!(WIDGET_KINDS as readonly string[]).includes(widget.kind)) fail('unknown_widget_kind', field(path, 'kind'));
  checkWidgetBox(widget, path);
  if (widget.settings === null) fail('invalid_widget', field(path, 'settings'));
}

function checkWidgets(widgets: OverlayWidget[]): void {
  if (widgets.length > overlayLimits.maxWidgets) fail('too_many_widgets', 'widgets');
  const seen = new Set<string>();
  widgets.forEach((widget, position) => {
    const path = index('widgets', position);
    checkWidget(widget, path);
    if (seen.has(widget.id)) fail('duplicate_widget_id', field(path, 'id'));
    seen.add(widget.id);
  });
}

function fitsFrame(limit: AssetLimit, asset: OverlayAsset): boolean {
  if (limit.maxWidth === 0) return true;
  const inFrame = within(asset.w, 1, limit.maxWidth) && within(asset.h, 1, limit.maxHeight);
  return inFrame && (limit.maxPixels === 0 || asset.w * asset.h <= limit.maxPixels);
}

function wellFormed(asset: OverlayAsset): boolean {
  return HASH_PATTERN.test(asset.sha256) && asset.bytes > 0 && Math.min(asset.w, asset.h, asset.duration_ms) >= 0;
}

function checkAsset(asset: OverlayAsset, path: string): void {
  const limit = Object.hasOwn(assetLimits, asset.type) ? assetLimits[asset.type] : undefined;
  if (!limit || !wellFormed(asset)) fail('invalid_asset', path);
  if (asset.bytes > limit.maxBytes) fail('asset_too_large', field(path, 'bytes'));
  if (!fitsFrame(limit, asset)) fail('asset_dimensions', path);
  if (limit.maxDurationMs !== 0 && asset.duration_ms > limit.maxDurationMs) fail('asset_duration', field(path, 'duration_ms'));
}

function checkTotals(assets: OverlayAsset[]): void {
  const total = assets.reduce((sum, asset) => sum + asset.bytes, 0);
  if (total > overlayLimits.overlayAssetBytes) fail('overlay_assets_too_large', 'assets');
  for (const type of ASSET_TYPES) {
    const cap = assetLimits[type].maxPerOverlay;
    if (cap > 0 && assets.filter((asset) => asset.type === type).length > cap) fail('too_many_of_type', 'assets');
  }
}

function checkAssets(assets: Record<string, OverlayAsset>): void {
  const ids = Object.keys(assets).sort();
  if (ids.length > overlayLimits.overlayAssets) fail('too_many_assets', 'assets');
  for (const id of ids) {
    const path = field('assets', id);
    if (!ID_PATTERN.test(id)) fail('invalid_id', path);
    checkAsset(assets[id]!, path);
  }
  checkTotals(ids.map((id) => assets[id]!));
}

function refError(value: unknown, assets: Record<string, OverlayAsset>): OverlayErrorCode | null {
  if (typeof value === 'string') return stringRefError(value, assets);
  if (value === null || typeof value !== 'object') return null;
  const children = Array.isArray(value) ? value : Object.values(value);
  for (const child of children) {
    const code = refError(child, assets);
    if (code !== null) return code;
  }
  return null;
}

function stringRefError(value: string, assets: Record<string, OverlayAsset>): OverlayErrorCode | null {
  if (LEGACY_MEDIA.test(value)) return 'legacy_media_ref';
  const isRef = value.startsWith(ASSET_PREFIX);
  return isRef && !Object.hasOwn(assets, value.slice(ASSET_PREFIX.length)) ? 'unknown_asset' : null;
}

function checkBundle(widget: OverlayWidget, assets: Record<string, OverlayAsset>, path: string): void {
  if (widget.kind !== 'custom') return;
  const ref = widget.settings?.bundle;
  if (typeof ref !== 'string' || !ref.startsWith(ASSET_PREFIX)) fail('invalid_bundle', path);
  const id = ref.slice(ASSET_PREFIX.length);
  if (!Object.hasOwn(assets, id)) fail('unknown_asset', path);
  if (assets[id]!.type !== 'widget') fail('invalid_bundle', path);
}

function checkRefs(doc: OverlayDocument): void {
  doc.widgets.forEach((widget, position) => {
    const path = field(index('widgets', position), 'settings');
    checkBundle(widget, doc.assets, path);
    const root = widget.kind === 'custom' ? widget.settings?.values : widget.settings;
    const code = refError(root, doc.assets);
    if (code !== null) fail(code, path);
  });
}

function checkCustom(doc: OverlayDocument): void {
  if (doc.template !== '' || doc.config !== null) fail('invalid_document', 'template');
  if (!within(doc.width, MIN_CANVAS, MAX_CANVAS) || !within(doc.height, MIN_CANVAS, MAX_CANVAS)) fail('invalid_size', 'width');
  checkWidgets(doc.widgets);
  checkAssets(doc.assets);
  checkRefs(doc);
}

function checkDocument(doc: OverlayDocument, input: unknown): void {
  if (byteLength(JSON.stringify(input)) > overlayLimits.documentBytes) fail('document_too_large');
  checkIdentity(doc);
  if (doc.kind === 'system') checkSystem(doc);
  else checkCustom(doc);
}

export function validateDocument(input: unknown): ValidationResult {
  try {
    const doc = decodeDocument(input);
    checkDocument(doc, input);
    return { ok: true, doc };
  } catch (failure) {
    if (failure instanceof Failure) return { ok: false, error: failure.error };
    throw failure;
  }
}
