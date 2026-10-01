// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { connectionUiState, type ConnSignals, type SubState } from './connection-state';

const base: ConnSignals = { grant: true, active: true, status: 'vip', sub: 'ok' };

type Row = [name: string, patch: Partial<ConnSignals>, want: Partial<ReturnType<typeof connectionUiState>>];

const ROWS: Row[] = [
	['online only when grant + active + sub ok', {}, { kind: 'online', live: true, showEnable: false }],
	['pending reads as connecting, never online, and cannot be managed mid-enrollment', { sub: 'pending' }, { kind: 'connecting', live: false, showEnable: false, canManage: false }],
	['unenrolled reads as connecting, never online, and cannot be managed mid-enrollment', { sub: 'unenrolled' }, { kind: 'connecting', live: false, showEnable: false, canManage: false }],
	['failing reads as degraded, never online', { sub: 'failing' }, { kind: 'degraded', live: false, showEnable: false }],
	['an unknown sub reads as sub_unknown, never online', { sub: 'unknown' }, { kind: 'sub_unknown', live: false, showEnable: false }],
	['revoked → reauth_required (reconnect via settings, no enable, no retry)', { sub: 'revoked' }, { kind: 'reauth_required', showConnect: true, showEnable: false, canRetry: false, live: false }],
	['chat_banned → bot_banned (enable after the unban, no reconnect, no restart)', { sub: 'chat_banned' }, { kind: 'bot_banned', showEnable: true, showConnect: false, canManage: false, canRetry: false, live: false }],
	['a revoked sub outranks the active flag outgress cleared', { active: false, sub: 'revoked' }, { kind: 'reauth_required' }],
	['a chat ban outranks the active flag outgress cleared', { active: false, sub: 'chat_banned' }, { kind: 'bot_banned' }],
	['an unenrolled sub outranks the active flag outgress cleared', { active: false, sub: 'unenrolled' }, { kind: 'disabled' }],
	['a down grant read is unavailable, not a definite state', { grant: 'unknown' }, { kind: 'unavailable' }],
	['a down active read is unavailable and retryable, never live or enable-able', { active: 'unknown' }, { kind: 'unavailable', canRetry: true, live: false, showEnable: false }],
	['no grant → auth_required (connect via settings, not enable)', { grant: false }, { kind: 'auth_required', showConnect: true, showEnable: false }],
	['grant present but inactive → disabled (enable form shown)', { active: false }, { kind: 'disabled', showEnable: true, canManage: false }]
];

describe('connectionUiState', () => {
	test.each(ROWS)('%s', (_name, patch, want) => {
		expect(connectionUiState({ ...base, ...patch })).toMatchObject(want);
	});

	test('every signal permutation resolves to exactly one kind', () => {
		const grants: (boolean | 'unknown')[] = [true, false, 'unknown'];
		const actives: (boolean | 'unknown')[] = [true, false, 'unknown'];
		const subs: SubState[] = ['ok', 'pending', 'failing', 'revoked', 'unenrolled', 'unknown'];
		for (const grant of grants)
			for (const active of actives)
				for (const sub of subs) {
					const r = connectionUiState({ grant, active, status: 'unknown', sub });
					expect(typeof r.kind).toBe('string');
					if (r.kind === 'online') {
						expect(grant).toBe(true);
						expect(active).toBe(true);
						expect(sub).toBe('ok');
					}
				}
	});
});
