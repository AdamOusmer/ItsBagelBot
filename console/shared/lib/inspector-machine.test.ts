import { describe, expect, test } from 'bun:test';
import {
	initial,
	openClean,
	edit,
	requestSave,
	resolveSave,
	requestClose,
	requestSelect,
	confirmDiscard,
	cancelIntent,
	externalUpdate,
	resolveConflict,
	type InspectorState
} from './inspector-machine';

type Cmd = { name: string; response: string };

const A: Cmd = { name: 'a', response: 'hi' };
const B: Cmd = { name: 'b', response: 'yo' };

describe('inspector-machine', () => {
	test('openClean starts clean with an independent draft copy', () => {
		const s = openClean('a', A);
		expect(s.selectedId).toBe('a');
		expect(s.dirty).toBe(false);
		expect(s.draft).toEqual(A);
		expect(s.draft).not.toBe(A); // cloned, not aliased
	});

	test('editing toward and back from committed toggles dirty', () => {
		let s = openClean('a', A);
		s = edit(s, { ...A, response: 'changed' });
		expect(s.dirty).toBe(true);
		s = edit(s, { ...A }); // back to original value
		expect(s.dirty).toBe(false);
	});

	test('requestClose on a dirty draft parks a discard intent instead of losing it', () => {
		let s = openClean('a', A);
		s = edit(s, { ...A, response: 'x' });
		s = requestClose(s);
		expect(s.pendingIntent).toEqual({ kind: 'close' });
		expect(s.draft?.response).toBe('x'); // draft preserved
	});

	test('requestClose on a clean draft closes immediately', () => {
		const s = requestClose(openClean('a', A));
		expect(s.selectedId).toBeNull();
	});

	test('dirty row switch is guarded; cancel keeps editing, confirm resumes intent', () => {
		let s = openClean('a', A);
		s = edit(s, { ...A, response: 'x' });
		s = requestSelect(s, 'b');
		expect(s.pendingIntent).toEqual({ kind: 'select', id: 'b' });
		// keep editing
		const kept = cancelIntent(s);
		expect(kept.pendingIntent).toBeUndefined();
		expect(kept.draft?.response).toBe('x');
		// or discard and resume
		const { state, intent } = confirmDiscard(s);
		expect(intent).toEqual({ kind: 'select', id: 'b' });
		expect(state.selectedId).toBeNull(); // caller now loads 'b'
	});

	// THE core guard: a delayed response for A must not touch selected B.
	test('a stale save response for A cannot affect B', () => {
		// Edit + submit A.
		let s = openClean('a', A);
		s = edit(s, { ...A, response: 'A-edited' });
		s = requestSave(s, 'req-A');
		expect(s.status).toBe('saving');

		// Before A resolves, the user abandons it and opens B, edits B.
		s = openClean('b', B);
		s = edit(s, { ...B, response: 'B-edited' });

		// A's response finally lands. It must be ignored entirely.
		const after = resolveSave(s, 'req-A', { type: 'success' });
		expect(after).toBe(s); // no-op, same reference
		expect(after.selectedId).toBe('b');
		expect(after.draft?.response).toBe('B-edited');
		expect(after.status).not.toBe('saved');
	});

	test('a matching save response settles the SAME selection to saved+clean', () => {
		let s = openClean('a', A);
		s = edit(s, { ...A, response: 'A-edited' });
		s = requestSave(s, 'req-A');
		s = resolveSave(s, 'req-A', { type: 'success' });
		expect(s.status).toBe('saved');
		expect(s.dirty).toBe(false);
		expect(s.selectedId).toBe('a'); // Save leaves the inspector open
		expect(s.submitted).toBeUndefined();
	});

	test('editing again after submit keeps the later edit dirty when the response lands', () => {
		let s = openClean('a', A);
		s = edit(s, { ...A, response: 'first' });
		s = requestSave(s, 'req-1');
		s = edit(s, { ...A, response: 'second' }); // user keeps typing during save
		s = resolveSave(s, 'req-1', { type: 'success' });
		expect(s.draft?.response).toBe('second');
		expect(s.dirty).toBe(true); // the newer edit is not falsely marked saved
		expect(s.status).toBe('idle');
	});

	test('a failed save keeps the draft and surfaces error', () => {
		let s = openClean('a', A);
		s = edit(s, { ...A, response: 'x' });
		s = requestSave(s, 'r');
		s = resolveSave(s, 'r', { type: 'error' });
		expect(s.status).toBe('error');
		expect(s.dirty).toBe(true);
		expect(s.draft?.response).toBe('x');
	});

	test('external update rebases a clean selection, conflicts a dirty one', () => {
		// clean → rebase
		let clean = openClean('a', A);
		clean = externalUpdate(clean, 'a', { ...A, response: 'server' });
		expect(clean.draft?.response).toBe('server');
		expect(clean.status).toBe('idle');

		// dirty → conflict, draft untouched
		let dirty = edit(openClean('a', A), { ...A, response: 'mine' });
		dirty = externalUpdate(dirty, 'a', { ...A, response: 'server' });
		expect(dirty.status).toBe('conflict');
		expect(dirty.draft?.response).toBe('mine');
	});

	test('external update for a different item is ignored', () => {
		const s = edit(openClean('a', A), { ...A, response: 'mine' });
		const after = externalUpdate(s, 'b', B);
		expect(after).toBe(s);
	});

	test('conflict resolution: take adopts server, keep stays dirty on new base', () => {
		let s = edit(openClean('a', A), { ...A, response: 'mine' });
		s = externalUpdate(s, 'a', { ...A, response: 'server' });
		const took = resolveConflict(s, 'take');
		expect(took.draft?.response).toBe('server');
		expect(took.dirty).toBe(false);
		const kept = resolveConflict(s, 'keep');
		expect(kept.draft?.response).toBe('mine');
		expect(kept.dirty).toBe(true);
		expect(kept.committed?.response).toBe('server');
	});

	test('requestSave is a no-op when nothing is dirty', () => {
		const s: InspectorState<Cmd> = openClean('a', A);
		expect(requestSave(s, 'r')).toBe(s);
	});

	test('requestSave snapshots a proxied draft (structuredClone rejects proxies)', () => {
		// At runtime the draft arrives wrapped in a Svelte 5 $state proxy, which
		// structuredClone throws DataCloneError on; the clone must fall back.
		let s = openClean('a', A);
		s = edit(s, new Proxy({ ...A, response: 'changed' }, {}));
		s = requestSave(s, 'req-p');
		expect(s.status).toBe('saving');
		expect(s.submitted?.snapshot).toEqual({ ...A, response: 'changed' });
	});
});
