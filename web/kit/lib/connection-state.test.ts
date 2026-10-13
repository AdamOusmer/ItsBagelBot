// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { connectionUiState, mergeConnectionSignals, type ConnSignals, type SubState } from './connection-state';

const base: ConnSignals = { grant: true, active: true, status: 'vip', sub: 'ok' };

type Row = [name: string, patch: Partial<ConnSignals>, want: Partial<ReturnType<typeof connectionUiState>>];

const ROWS: Row[] = [
	['online only when grant + active + sub ok', {}, { kind: 'online', live: true, showEnable: false }],
	['pending reads as connecting, never online, and cannot be managed mid-enrollment', { sub: 'pending' }, { kind: 'connecting', live: false, showEnable: false, canManage: false }],
	['unenrolled reads as connecting, never online, and cannot be managed mid-enrollment', { sub: 'unenrolled' }, { kind: 'connecting', live: false, showEnable: false, canManage: false }],
	['failing reads as degraded, never online', { sub: 'failing' }, { kind: 'degraded', live: false, showEnable: false }],
	['an unknown sub reads as sub_unknown, never online', { sub: 'unknown' }, { kind: 'sub_unknown', live: false, showEnable: false }],
	['revoked → reauth_required (reconnect via settings, no enable, no retry)', { sub: 'revoked' }, { kind: 'reauth_required', showConnect: true, showEnable: false, canRetry: false, live: false }],
	['chat_banned → bot_banned (enable after the unban, no reconnect, no restart)', { sub: 'chat_banned' }, { kind: 'bot_banned', showEnable: false, showConnect: false, canManage: false, canRetry: false, live: false }],
	['a revoked sub outranks the active flag outgress cleared', { active: false, sub: 'revoked' }, { kind: 'reauth_required' }],
	['a chat ban outranks the active flag outgress cleared', { active: false, sub: 'chat_banned' }, { kind: 'bot_banned' }],
	['an unenrolled sub outranks the active flag outgress cleared', { active: false, sub: 'unenrolled' }, { kind: 'disabled' }],
	['no grant → auth_required (connect via settings, not enable)', { grant: false }, { kind: 'auth_required', showConnect: true, showEnable: false }],
	['grant present but inactive → disabled (enable form shown)', { active: false }, { kind: 'disabled', showEnable: true, canManage: false }]
];

describe('connectionUiState', () => {
	test.each(ROWS)('%s', (_name, patch, want) => {
		expect(connectionUiState({ ...base, ...patch })).toMatchObject(want);
	});

	test('pending / unenrolled map to connecting, failing to degraded', () => {
		expect(connectionUiState({ ...base, sub: 'pending' }).kind).toBe('connecting');
		expect(connectionUiState({ ...base, sub: 'unenrolled' }).kind).toBe('connecting');
		expect(connectionUiState({ ...base, sub: 'failing' }).kind).toBe('degraded');
		expect(connectionUiState({ ...base, sub: 'unknown' }).kind).toBe('sub_unknown');
	});

	test('a down core read is unavailable, not a definite state', () => {
		expect(connectionUiState({ ...base, grant: 'unknown' }).kind).toBe('unavailable');
		expect(connectionUiState({ ...base, active: 'unknown' }).kind).toBe('unavailable');
		const u = connectionUiState({ ...base, active: 'unknown' });
		expect(u.canRetry).toBe(true);
		expect(u.live).toBe(false);
		expect(u.showEnable).toBe(false);
	});

	test('enable is never offered while a channel is active or in flight', () => {
		for (const sub of ['ok', 'pending', 'unenrolled', 'failing', 'revoked', 'unknown'] as SubState[]) {
			expect(connectionUiState({ ...base, sub }).showEnable).toBe(false);
		}
	});

	test('management actions are unavailable while an enrollment is in flight', () => {
		expect(connectionUiState({ ...base, sub: 'pending' }).canManage).toBe(false);
		expect(connectionUiState({ ...base, sub: 'unenrolled' }).canManage).toBe(false);
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

test('durable lifecycle outranks missing grants and preserves owner intent', () => {
 const lifecycle = { version: 1, desired_enabled: true, runtime_state: 'blocked', chat_restriction: 'banned', grant_health: 'missing' };
 expect(connectionUiState({ ...base, grant: false, active: false, lifecycle })).toMatchObject({kind:'bot_banned', showEnable:false, verifyRepair:true});
 expect(connectionUiState({ ...base, lifecycle:{...lifecycle, desired_enabled:false} })).toMatchObject({kind:'disabled', retainedBan:true});
 expect(connectionUiState({ ...base, lifecycle:{...lifecycle, chat_restriction:'none', grant_health:'healthy', runtime_state:'dormant'} }).kind).toBe('dormant');
 expect(connectionUiState({ ...base, lifecycle:{...lifecycle, chat_restriction:'none', runtime_state:'recovering'} }).kind).toBe('recovering');
 expect(connectionUiState({ ...base, lifecycle:{...lifecycle, version:2} }).kind).toBe('unavailable');
});

test('new loader bans outrank old poll state and new repair polls override old inactive flags',()=>{
 const active={version:1,desired_enabled:true,runtime_state:'active',chat_restriction:'none',grant_health:'healthy'};
 const blocked={...active,runtime_state:'blocked',chat_restriction:'banned'};
 const ban={...base,accountCreatedAt:101,lifecycleRevision:3,lifecycle:blocked};
 const oldPoll={...base,accountCreatedAt:101,lifecycleRevision:2,lifecycle:active};
 expect(connectionUiState(mergeConnectionSignals(ban,oldPoll)).kind).toBe('bot_banned');
 const oldLoader={...base,active:false,accountCreatedAt:101,lifecycleRevision:3,lifecycle:blocked};
 const repaired={...base,accountCreatedAt:101,lifecycleRevision:4,lifecycle:active};
 expect(connectionUiState(mergeConnectionSignals(oldLoader,repaired)).kind).toBe('online');
 expect(connectionUiState({...base,active:false,lifecycle:active}).kind).toBe('online');
 expect(connectionUiState({...base,lifecycle:{...blocked,desired_enabled:false}})).toMatchObject({kind:'disabled',showEnable:true,retainedBan:true});
});

test('automatic unban promise requires healthy recovery listeners',()=>{
 const lifecycle={version:1,desired_enabled:true,runtime_state:'blocked',chat_restriction:'banned',grant_health:'healthy'};
 expect(connectionUiState({...base,lifecycle}).automaticRecovery).toBe(false);
 expect(connectionUiState({...base,lifecycle:{...lifecycle,control_listeners_healthy:false}}).automaticRecovery).toBe(false);
 expect(connectionUiState({...base,lifecycle:{...lifecycle,control_listeners_healthy:true}}).automaticRecovery).toBe(true);
 expect(connectionUiState({...base,lifecycle:{...lifecycle,control_listeners_healthy:true,desired_enabled:false}}).automaticRecovery).toBe(false);
});
