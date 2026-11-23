// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { validateDocument, type OverlayAsset } from '../overlay-document';
import { staticText } from '../i18n/static';
import { ALERT_KINDS, EDITABLE_KINDS, applySetting, newWidget } from './defaults';
import { DEFAULT_CANVAS, blankDocument, toWireDocument } from './document';
import { EmulationBus, sampleEvent } from './emulate';
import {
  alertFields, alertLook, creditHeaders, geometryFields, labelOf, listRule, tierWeights, typography, variationFields, widgetFields,
  type SettingField
} from './fields';
import { IMPORT_ERRORS, IMPORT_REASONS, importError, importReason, reportCounts } from './import-report';
import { MediaFailure, UPLOAD_ERRORS, acceptsType, describeUpload, slotAccept, slotMaxBytes, uploadError } from './media';
import { KIND_GROUPS, filterKinds, kindIcon, type KindLabel } from './kinds';
import { centerPosition, copyOf, moveWidget, reorderWidget } from './ops';
import { parseSize } from './resolutions';
import { EditorSession } from './session';
import { createOverlay, templateNames } from './templates';
import { stepZoom } from './zoom';

const png: OverlayAsset = { type: 'png', bytes: 1000, w: 100, h: 100, duration_ms: 0, sha256: 'a'.repeat(64), url: 'https://cdn.itsbagelbot.com/u/1/a' };
const HANDLES = ['nw', 'ne', 'sw', 'se'];

test.each(templateNames)('the %s template is a valid custom document', (template) => {
  expect(validateDocument(createOverlay(template, 'Starter', (kind) => kind)).ok).toBe(true);
});

test('every editable kind sits in exactly one add-widget group and has an icon', () => {
  const grouped = KIND_GROUPS.flatMap((group) => [...group.kinds]).sort();
  expect({ grouped, icons: EDITABLE_KINDS.every((kind) => kindIcon(kind) !== 'overlay' || kind === 'image') }).toEqual({
    grouped: [...EDITABLE_KINDS].sort(),
    icons: true
  });
});

test.each(EDITABLE_KINDS)('a new %s widget passes document validation', (kind) => {
  const doc = { ...blankDocument('Doc'), widgets: [newWidget(kind, DEFAULT_CANVAS, kind)] };
  expect(validateDocument(doc).ok).toBe(true);
});

test.each([
  { name: 'switching a timer to count up stamps the start time', kind: 'timer' as const, key: 'mode', value: 'countup', stamped: true },
  { name: 'switching a timer to count down', kind: 'timer' as const, key: 'mode', value: 'countdown', stamped: false },
  { name: 'another timer setting', kind: 'timer' as const, key: 'format', value: 'countup', stamped: false },
  { name: 'a mode on another widget', kind: 'goal' as const, key: 'mode', value: 'countup', stamped: false }
])('$name', ({ kind, key, value, stamped }) => {
  const next = applySetting(kind, { at: '' }, key, value);
  expect({ value: next[key], stamped: next.at !== '' }).toEqual({ value, stamped });
});

test('saving keeps referenced assets only, with their urls', () => {
  const widget = { ...newWidget('image', DEFAULT_CANVAS, 'Logo'), settings: { media: 'asset:used', fit: 'contain' } };
  const served = {
    ...blankDocument('Doc'),
    widgets: [widget],
    assets: { used: png, spare: png }
  };
  const wire = toWireDocument(served);
  expect({ assets: Object.keys(wire.assets), url: wire.assets.used?.url, valid: validateDocument(wire).ok }).toEqual({
    assets: ['used'],
    url: png.url,
    valid: true
  });
});

test('the session steps through commits, keeps the saved revision and reverts a drag', () => {
  const first = blankDocument('First');
  const second = { ...first, name: 'Second' };
  const session = new EditorSession(first);
  session.commit(second);
  session.markSaved({ ...second, rev: 4 });
  const undone = session.step(-1);
  const redone = session.step(1);
  expect([undone?.name, undone?.rev, redone?.name, redone?.rev, session.revert().name]).toEqual(['First', 4, 'Second', 4, 'Second']);
});

test('the session has nothing to step before the first commit', () => {
  const session = new EditorSession(blankDocument('Only'));
  expect([session.canUndo, session.canRedo, session.step(-1), session.step(1)]).toEqual([false, false, undefined, undefined]);
});

test('duplicating offsets inside the canvas and reordering stops at the ends', () => {
  const doc = createOverlay('justChatting', 'Chat', (kind) => kind);
  const [bottom, , , top] = doc.widgets;
  const copy = copyOf(doc, top!);
  const lowered = reorderWidget(doc, bottom!.id, -1);
  const raised = reorderWidget(doc, top!.id, 1);
  expect({ id: copy.id !== top!.id, name: copy.name, x: copy.x, edge: lowered === doc && raised === doc }).toEqual({
    id: true, name: 'eventlist copy', x: top!.x + 18, edge: true
  });
});

test.each(ALERT_KINDS)('the %s sample is a test event the runtime can read', (kind) => {
  const event = sampleEvent(kind, 7, 0);
  expect({ id: event.id, kind: event.kind, seq: event.seq, test: event.test, at: event.at, user: event.user?.name }).toEqual({
    id: 'test-7', kind, seq: 7, test: true, at: '1970-01-01T00:00:00.000Z', user: 'BagelFan'
  });
});

test('samples carry the details their alerts print', () => {
  expect({
    months: sampleEvent('resub', 1).months,
    recipient: sampleEvent('gift_sub', 1).recipient?.name,
    bits: sampleEvent('cheer', 1).amount,
    viewers: sampleEvent('raid', 1).amount,
    reward: sampleEvent('redemption', 1).reward?.title
  }).toEqual({ months: 12, recipient: 'Toasty', bits: 500, viewers: 42, reward: 'Hydrate' });
});

test('the emulation bus numbers events and stops after unsubscribe', () => {
  const bus = new EmulationBus();
  const seen: number[] = [];
  const stop = bus.subscribe((event) => seen.push(event.seq));
  bus.emit('follow');
  bus.emit('raid');
  stop();
  bus.emit('cheer');
  expect(seen).toEqual([1, 2]);
});

test.each([
  { name: 'in from fit', scale: 0.37, direction: 1 as const, want: 0.5 },
  { name: 'out from fit', scale: 0.37, direction: -1 as const, want: 0.25 },
  { name: 'in from 100 percent', scale: 1, direction: 1 as const, want: 1.5 },
  { name: 'out from 100 percent', scale: 1, direction: -1 as const, want: 0.75 },
  { name: 'in just under a step', scale: 0.499, direction: 1 as const, want: 0.75 },
  { name: 'in at the top', scale: 2, direction: 1 as const, want: 2 },
  { name: 'out at the bottom', scale: 0.25, direction: -1 as const, want: 0.25 }
])('zooming $name', ({ scale, direction, want }) => {
  expect(stepZoom(scale, direction)).toBe(want);
});

test.each([
  { value: '1280x720', want: { width: 1280, height: 720 } },
  { value: '1920x1080', want: { width: 1920, height: 1080 } },
  { value: '1080x1920', want: { width: 1080, height: 1920 } },
  { value: '999x1', want: undefined },
  { value: '1280X720', want: undefined },
  { value: 'big', want: undefined },
  { value: '', want: undefined },
  { value: null, want: undefined }
])('the size $value parses to $want', ({ value, want }) => {
  expect(parseSize(value)).toEqual(want);
});

test.each([
  { name: 'to the front', from: 0, to: 3, order: [1, 2, 3, 0] },
  { name: 'to the back', from: 3, to: 0, order: [3, 0, 1, 2] },
  { name: 'in place', from: 1, to: 1, order: [0, 1, 2, 3] },
  { name: 'past the end', from: 1, to: 4, order: [0, 1, 2, 3] },
  { name: 'before the start', from: 1, to: -1, order: [0, 1, 2, 3] }
])('moving a layer $name', ({ from, to, order }) => {
  const doc = createOverlay('justChatting', 'Chat', (kind) => kind);
  const ids = doc.widgets.map((widget) => widget.id);
  expect(moveWidget(doc, ids[from]!, to).widgets.map((widget) => ids.indexOf(widget.id))).toEqual(order);
});

test('moving an unknown layer changes nothing', () => {
  const doc = createOverlay('justChatting', 'Chat', (kind) => kind);
  expect(moveWidget(doc, 'missing', 0)).toBe(doc);
});

test.each([
  { canvas: DEFAULT_CANVAS, want: { x: 760, y: 440 } },
  { canvas: { width: 1280, height: 720 }, want: { x: 440, y: 260 } }
])('centering a 400 x 200 box on $canvas', ({ canvas, want }) => {
  const widget = { ...newWidget('text', canvas, 'Title'), w: 400, h: 200 };
  expect(centerPosition(widget, canvas)).toEqual(want);
});

const labels: KindLabel[] = [
  { kind: 'alertbox', name: 'Alert box', hint: 'Pops up for follows.' },
  { kind: 'goal', name: 'Goal', hint: 'Tracks a follower target.' },
  { kind: 'timer', name: 'Timer', hint: 'Counts down.' }
];

test.each([
  { query: '', want: ['alertbox', 'goal', 'timer'] },
  { query: '   ', want: ['alertbox', 'goal', 'timer'] },
  { query: 'ALERT', want: ['alertbox'] },
  { query: 'follow', want: ['alertbox', 'goal'] },
  { query: 'zzz', want: [] }
])('searching widgets for "$query"', ({ query, want }) => {
  expect(filterKinds(labels, query).map((item) => item.kind)).toEqual(want);
});

const fieldGroups: SettingField[][] = [
  typography, alertLook, alertFields, listRule, variationFields, creditHeaders, tierWeights, geometryFields, ...Object.values(widgetFields)
];

test('every field label and option has an English catalog entry', () => {
  const fields = fieldGroups.flat();
  const keys = [
    ...fields.map((item) => `overlayEditor.field.${labelOf(item)}`),
    ...fields.filter((item) => !item.raw).flatMap((item) => (item.options ?? []).map((option) => `overlayEditor.option.${option}`)),
    ...[...EDITABLE_KINDS, 'custom'].map((kind) => `overlayEditor.kind.${kind}`),
    ...EDITABLE_KINDS.map((kind) => `overlayEditor.kindHint.${kind}`),
    ...KIND_GROUPS.map((group) => `overlayEditor.group.${group.key}`),
    ...ALERT_KINDS.map((kind) => `overlayEditor.alert.${kind}`),
    ...templateNames.map((name) => `overlayEditor.template.${name}`),
    ...HANDLES.map((handle) => `overlayEditor.handle.${handle}`),
    ...UPLOAD_ERRORS.map((code) => `overlayEditor.uploadError.${code}`),
    ...[...IMPORT_REASONS, 'unknown'].map((code) => `overlayEditor.reason.${code}`),
    ...IMPORT_ERRORS.map((code) => `overlayEditor.importError.${code}`)
  ];
  expect(keys.filter((key) => staticText('en', key) === key)).toEqual([]);
});

test.each([
  { name: 'a known reason', run: () => importReason('remote_font'), want: 'remote_font' },
  { name: 'an unknown reason', run: () => importReason('made_up'), want: undefined },
  { name: 'no reason', run: () => importReason(undefined), want: undefined },
  { name: 'a known error', run: () => importError('bomb_ratio'), want: 'bomb_ratio' },
  { name: 'an unknown error', run: () => importError('made_up'), want: 'failed' },
  { name: 'an unknown upload error', run: () => uploadError('made_up'), want: 'failed' },
  { name: 'a known upload error', run: () => uploadError('rate_limited'), want: 'rate_limited' },
  { name: 'a server size code', run: () => uploadError('too_large'), want: 'asset_too_large' },
  { name: 'a server codec code', run: () => uploadError('unsupported_codec'), want: 'unsupported_media' }
])('normalizes $name', ({ run, want }) => {
  expect(run()).toBe(want);
});

test('the report counts each group', () => {
  const item = { subject: 'a.png' };
  expect(reportCounts({ mapped: [item, item], approximated: [item], unsupported: [] })).toEqual({ mapped: 2, approximated: 1, unsupported: 0 });
});

test('media slots follow the shared limits table', () => {
  expect({
    video: slotAccept('video'),
    soundBytes: slotMaxBytes('sound'),
    visualBytes: slotMaxBytes('visual'),
    gifAsSound: acceptsType('sound', 'gif'),
    webmAsVisual: acceptsType('visual', 'webm')
  }).toEqual({ video: 'video/webm', soundBytes: 5 * 1024 * 1024, visualBytes: 50 * 1024 * 1024, gifAsSound: false, webmAsVisual: true });
});

test('an upload reply becomes a document asset with the server hash and url', () => {
  const reply = { ...png, asset_id: 'abc', display_name: 'logo.png' };
  expect(describeUpload(reply)).toEqual(png);
});

test('a media failure carries the server code', () => {
  expect(new MediaFailure('rate_limited').code).toBe('rate_limited');
});
