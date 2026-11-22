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
type Assets = Record<string, OverlayAsset>;
interface Range {
  min: number;
  max: number;
}
interface Bound {
  key: string;
  value: number;
  range: Range;
}

const ASSET_PREFIX = 'asset:';
const NAME_LENGTH: Range = { min: 1, max: 120 };
const CANVAS: Range = { min: 320, max: 7680 };
const POSITION: Range = { min: 0, max: 7680 };
const EXTENT: Range = { min: 1, max: 7680 };
const ROTATION: Range = { min: -360, max: 360 };
const UNIT: Range = { min: 0, max: 1 };
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

class Scope {
  constructor(readonly path = '') {}

  field(key: string): Scope {
    return new Scope(this.path ? `${this.path}.${key}` : key);
  }

  at(position: number): Scope {
    return new Scope(`${this.path}[${position}]`);
  }

  fail(code: OverlayErrorCode): never {
    throw new Failure({ code, path: this.path });
  }
}

const ROOT = new Scope();
const WIDGETS = ROOT.field('widgets');
const ASSETS = ROOT.field('assets');

const within = (range: Range, value: number): boolean => value >= range.min && value <= range.max;
const byteLength = (text: string): number => new TextEncoder().encode(text).length;
const isObject = (value: unknown): value is object => typeof value === 'object' && value !== null;
const isRecord = (value: unknown): value is Obj => isObject(value) && !Array.isArray(value);
const isString = (value: unknown): boolean => typeof value === 'string';
const isMember = (options: readonly string[], value: string): boolean => options.includes(value);

class Reader {
  private constructor(
    private readonly obj: Obj,
    readonly scope: Scope
  ) {}

  static of(value: unknown, scope: Scope, keys?: readonly string[]): Reader {
    if (!isRecord(value)) return scope.fail('invalid_document');
    if (keys && Object.keys(value).some((key) => !keys.includes(key))) scope.fail('invalid_document');
    return new Reader(value, scope);
  }

  toObject(): Obj {
    return this.obj;
  }

  raw(key: string): unknown {
    return this.obj[key];
  }

  private read(key: string, type: Primitive): unknown {
    const value = this.obj[key];
    if (value == null) return ZERO[type];
    if (typeof value !== type) this.scope.field(key).fail('invalid_document');
    return value;
  }

  text(key: string): string {
    return this.read(key, 'string') as string;
  }

  decimal(key: string): number {
    return this.read(key, 'number') as number;
  }

  flag(key: string): boolean {
    return this.read(key, 'boolean') as boolean;
  }

  integer(key: string): number {
    const value = this.decimal(key);
    if (!Number.isInteger(value)) this.scope.field(key).fail('invalid_document');
    return value;
  }

  list(key: string): unknown[] {
    const value = this.obj[key];
    if (value == null) return [];
    return Array.isArray(value) ? value : this.scope.field(key).fail('invalid_document');
  }
}

function decodeConfig(value: unknown): PoweredConfig {
  const reader = Reader.of(value, ROOT.field('config'), CONFIG_KEYS);
  const directions = reader.list('directions');
  if (!directions.every(isString)) reader.scope.field('directions').fail('invalid_document');
  return {
    every_minutes: reader.integer('every_minutes'),
    show_seconds: reader.integer('show_seconds'),
    directions: directions as PoweredConfig['directions'],
    align: reader.text('align') as PoweredConfig['align']
  };
}

function decodeWidget(value: unknown, scope: Scope): OverlayWidget {
  const reader = Reader.of(value, scope, WIDGET_KEYS);
  const settings = reader.raw('settings');
  return {
    id: reader.text('id'),
    name: reader.text('name'),
    kind: reader.text('kind') as WidgetKind,
    x: reader.decimal('x'),
    y: reader.decimal('y'),
    w: reader.decimal('w'),
    h: reader.decimal('h'),
    rotation: reader.decimal('rotation'),
    opacity: reader.decimal('opacity'),
    hidden: reader.flag('hidden'),
    locked: reader.flag('locked'),
    settings: settings == null ? null : Reader.of(settings, scope.field('settings')).toObject()
  };
}

function decodeAsset(value: unknown, scope: Scope): OverlayAsset {
  const reader = Reader.of(value, scope, ASSET_KEYS);
  return {
    type: reader.text('type') as AssetType,
    bytes: reader.integer('bytes'),
    w: reader.integer('w'),
    h: reader.integer('h'),
    duration_ms: reader.integer('duration_ms'),
    sha256: reader.text('sha256')
  };
}

function decodeAssets(value: unknown): Assets {
  if (value == null) return {};
  const entries = Object.entries(Reader.of(value, ASSETS).toObject());
  return Object.fromEntries(entries.map(([id, asset]) => [id, decodeAsset(asset, ASSETS.field(id))]));
}

function decodeDocument(input: unknown): OverlayDocument {
  const reader = Reader.of(input, ROOT, DOCUMENT_KEYS);
  const config = reader.raw('config');
  return {
    version: reader.integer('version'),
    kind: reader.text('kind') as DocumentKind,
    id: reader.text('id'),
    name: reader.text('name'),
    width: reader.integer('width'),
    height: reader.integer('height'),
    rev: reader.integer('rev'),
    template: reader.text('template'),
    config: config == null ? null : decodeConfig(config),
    widgets: reader.list('widgets').map((widget, position) => decodeWidget(widget, WIDGETS.at(position))),
    assets: decodeAssets(reader.raw('assets'))
  };
}

function checkIdentity(doc: OverlayDocument): void {
  if (doc.version !== OVERLAY_VERSION) ROOT.field('version').fail('unsupported_version');
  if (!isMember(DOCUMENT_KINDS, doc.kind)) ROOT.field('kind').fail('unknown_kind');
  if (!ID_PATTERN.test(doc.id)) ROOT.field('id').fail('invalid_id');
  if (!within(NAME_LENGTH, Array.from(doc.name.trim()).length)) ROOT.field('name').fail('invalid_name');
  if (doc.rev < 0) ROOT.field('rev').fail('invalid_revision');
}

function checkSystem(doc: OverlayDocument): void {
  const content = [doc.width !== 0, doc.height !== 0, doc.widgets.length > 0, Object.keys(doc.assets).length > 0];
  if (content.includes(true)) ROOT.fail('system_has_content');
  if (doc.template !== 'powered') ROOT.field('template').fail('unknown_template');
  if (doc.config === null || poweredConfigError(doc.config) !== null) ROOT.field('config').fail('invalid_config');
}

function checkWidgetBox(widget: OverlayWidget, scope: Scope): void {
  const bounds: Bound[] = [
    { key: 'x', value: widget.x, range: POSITION },
    { key: 'y', value: widget.y, range: POSITION },
    { key: 'w', value: widget.w, range: EXTENT },
    { key: 'h', value: widget.h, range: EXTENT },
    { key: 'rotation', value: widget.rotation, range: ROTATION },
    { key: 'opacity', value: widget.opacity, range: UNIT },
    { key: 'name', value: Array.from(widget.name).length, range: NAME_LENGTH }
  ];
  for (const bound of bounds) {
    if (!within(bound.range, bound.value)) scope.field(bound.key).fail('invalid_widget');
  }
}

function checkWidget(widget: OverlayWidget, scope: Scope): void {
  if (!ID_PATTERN.test(widget.id)) scope.field('id').fail('invalid_id');
  if (!isMember(WIDGET_KINDS, widget.kind)) scope.field('kind').fail('unknown_widget_kind');
  checkWidgetBox(widget, scope);
  if (widget.settings === null) scope.field('settings').fail('invalid_widget');
}

function checkWidgets(widgets: OverlayWidget[]): void {
  if (widgets.length > overlayLimits.maxWidgets) WIDGETS.fail('too_many_widgets');
  const seen = new Set<string>();
  widgets.forEach((widget, position) => {
    const scope = WIDGETS.at(position);
    checkWidget(widget, scope);
    if (seen.has(widget.id)) scope.field('id').fail('duplicate_widget_id');
    seen.add(widget.id);
  });
}

function fitsPixels(limit: AssetLimit, asset: OverlayAsset): boolean {
  return limit.maxPixels === 0 || asset.w * asset.h <= limit.maxPixels;
}

function fitsFrame(limit: AssetLimit, asset: OverlayAsset): boolean {
  if (limit.maxWidth === 0) return true;
  const inFrame = within({ min: 1, max: limit.maxWidth }, asset.w) && within({ min: 1, max: limit.maxHeight }, asset.h);
  return inFrame && fitsPixels(limit, asset);
}

function hasSizes(asset: OverlayAsset): boolean {
  return asset.bytes > 0 && Math.min(asset.w, asset.h, asset.duration_ms) >= 0;
}

function wellFormed(asset: OverlayAsset): boolean {
  return HASH_PATTERN.test(asset.sha256) && hasSizes(asset);
}

function checkAsset(asset: OverlayAsset, scope: Scope): void {
  const limit = Object.hasOwn(assetLimits, asset.type) ? assetLimits[asset.type] : undefined;
  if (!limit || !wellFormed(asset)) scope.fail('invalid_asset');
  if (asset.bytes > limit.maxBytes) scope.field('bytes').fail('asset_too_large');
  if (!fitsFrame(limit, asset)) scope.fail('asset_dimensions');
  if (limit.maxDurationMs !== 0 && asset.duration_ms > limit.maxDurationMs) scope.field('duration_ms').fail('asset_duration');
}

function checkTotals(assets: OverlayAsset[]): void {
  const total = assets.reduce((sum, asset) => sum + asset.bytes, 0);
  if (total > overlayLimits.overlayAssetBytes) ASSETS.fail('overlay_assets_too_large');
  for (const type of ASSET_TYPES) {
    const cap = assetLimits[type].maxPerOverlay;
    if (cap > 0 && assets.filter((asset) => asset.type === type).length > cap) ASSETS.fail('too_many_of_type');
  }
}

function checkAssets(assets: Assets): void {
  const ids = Object.keys(assets).sort();
  if (ids.length > overlayLimits.overlayAssets) ASSETS.fail('too_many_assets');
  for (const id of ids) {
    const scope = ASSETS.field(id);
    if (!ID_PATTERN.test(id)) scope.fail('invalid_id');
    checkAsset(assets[id]!, scope);
  }
  checkTotals(ids.map((id) => assets[id]!));
}

function stringRefError(value: string, assets: Assets): OverlayErrorCode | null {
  if (LEGACY_MEDIA.test(value)) return 'legacy_media_ref';
  const isRef = value.startsWith(ASSET_PREFIX);
  return isRef && !Object.hasOwn(assets, value.slice(ASSET_PREFIX.length)) ? 'unknown_asset' : null;
}

function refError(value: unknown, assets: Assets): OverlayErrorCode | null {
  if (typeof value === 'string') return stringRefError(value, assets);
  if (!isObject(value)) return null;
  const children = Array.isArray(value) ? value : Object.values(value);
  for (const child of children) {
    const code = refError(child, assets);
    if (code !== null) return code;
  }
  return null;
}

function checkBundle(widget: OverlayWidget, assets: Assets, scope: Scope): void {
  if (widget.kind !== 'custom') return;
  const ref = widget.settings?.bundle;
  if (typeof ref !== 'string' || !ref.startsWith(ASSET_PREFIX)) scope.fail('invalid_bundle');
  const id = ref.slice(ASSET_PREFIX.length);
  if (!Object.hasOwn(assets, id)) scope.fail('unknown_asset');
  if (assets[id]!.type !== 'widget') scope.fail('invalid_bundle');
}

function checkRefs(doc: OverlayDocument): void {
  doc.widgets.forEach((widget, position) => {
    const scope = WIDGETS.at(position).field('settings');
    checkBundle(widget, doc.assets, scope);
    const root = widget.kind === 'custom' ? widget.settings?.values : widget.settings;
    const code = refError(root, doc.assets);
    if (code !== null) scope.fail(code);
  });
}

function checkCustom(doc: OverlayDocument): void {
  if (doc.template !== '' || doc.config !== null) ROOT.field('template').fail('invalid_document');
  if (!within(CANVAS, doc.width) || !within(CANVAS, doc.height)) ROOT.field('width').fail('invalid_size');
  checkWidgets(doc.widgets);
  checkAssets(doc.assets);
  checkRefs(doc);
}

function checkDocument(doc: OverlayDocument, input: unknown): void {
  if (byteLength(JSON.stringify(input)) > overlayLimits.documentBytes) ROOT.fail('document_too_large');
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
