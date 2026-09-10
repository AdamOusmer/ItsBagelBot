// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import {
	initial,
	openClean,
	edit,
	requestSave,
	resolveSave,
	type InspectorState
} from '../lib/inspector-machine';

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
