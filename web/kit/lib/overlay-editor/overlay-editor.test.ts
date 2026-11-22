// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { validateDocument, type OverlayAsset } from '../overlay-document';
import { staticText } from '../i18n/static';
import { ALERT_KINDS, EDITABLE_KINDS, newWidget } from './defaults';
import { DEFAULT_CANVAS, blankDocument, toWireDocument } from './document';
import {
  alertFields, alertLook, creditHeaders, geometryFields, labelOf, listRule, tierWeights, typography, variationFields, widgetFields,
  type SettingField
} from './fields';
import { IMPORT_ERRORS, IMPORT_REASONS, importError, importReason, reportCounts } from './import-report';
import { MediaFailure, UPLOAD_ERRORS, acceptsType, describeUpload, slotAccept, slotMaxBytes, uploadError } from './media';
import { copyOf, reorderWidget } from './ops';
import { EditorSession } from './session';
import { createOverlay, templateNames } from './templates';

const png: OverlayAsset = { type: 'png', bytes: 1000, w: 100, h: 100, duration_ms: 0, sha256: 'a'.repeat(64), url: 'https://cdn.itsbagelbot.com/u/1/a' };
const HANDLES = ['nw', 'ne', 'sw', 'se'];

test.each(templateNames)('the %s template is a valid custom document', (template) => {
  expect(validateDocument(createOverlay(template, 'Starter', (kind) => kind)).ok).toBe(true);
});

test.each(EDITABLE_KINDS)('a new %s widget passes document validation', (kind) => {
  const doc = { ...blankDocument('Doc'), widgets: [newWidget(kind, DEFAULT_CANVAS, kind)] };
  expect(validateDocument(doc).ok).toBe(true);
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

const fieldGroups: SettingField[][] = [
  typography, alertLook, alertFields, listRule, variationFields, creditHeaders, tierWeights, geometryFields, ...Object.values(widgetFields)
];

test('every field label and option has an English catalog entry', () => {
  const fields = fieldGroups.flat();
  const keys = [
    ...fields.map((item) => `overlayEditor.field.${labelOf(item)}`),
    ...fields.filter((item) => !item.raw).flatMap((item) => (item.options ?? []).map((option) => `overlayEditor.option.${option}`)),
    ...[...EDITABLE_KINDS, 'custom'].map((kind) => `overlayEditor.kind.${kind}`),
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
