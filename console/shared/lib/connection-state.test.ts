import { describe, expect, test } from 'bun:test';
import { connectionUiState, type ConnSignals, type SubState } from './connection-state';

// Characterization + invariant tests for the honest connection mapping. These
// pin the P0 fixes: pending/failing/unknown can never say online, a down core
// read is `unavailable` (not "not connected" / "free"), and a failed account
// read never resolves to a plan.

const base: ConnSignals = { grant: true, active: true, status: 'vip', sub: 'ok' };

describe('connectionUiState', () => {
	test('online only when grant + active + sub ok', () => {
		expect(connectionUiState(base).kind).toBe('online');
		expect(connectionUiState(base).live).toBe(true);
	});

	test('pending / unenrolled / failing / unknown never read as online', () => {
		const notOnline: SubState[] = ['pending', 'unenrolled', 'failing', 'unknown'];
		for (const sub of notOnline) {
			const r = connectionUiState({ ...base, sub });
			expect(r.kind).not.toBe('online');
			expect(r.live).toBe(false);
		}
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
		// Unavailable offers a retry and no confident action.
		const u = connectionUiState({ ...base, active: 'unknown' });
		expect(u.canRetry).toBe(true);
		expect(u.live).toBe(false);
		expect(u.showEnable).toBe(false);
	});

	test('no grant → auth_required (connect via settings, not enable)', () => {
		const r = connectionUiState({ ...base, grant: false });
		expect(r.kind).toBe('auth_required');
		expect(r.showConnect).toBe(true);
		expect(r.showEnable).toBe(false);
	});

	test('grant present but inactive → disabled (enable form shown)', () => {
		const r = connectionUiState({ ...base, active: false });
		expect(r.kind).toBe('disabled');
		expect(r.showEnable).toBe(true);
		expect(r.canManage).toBe(false);
	});

	test('enable is never offered while a channel is active or in flight', () => {
		for (const sub of ['ok', 'pending', 'unenrolled', 'failing', 'unknown'] as SubState[]) {
			expect(connectionUiState({ ...base, sub }).showEnable).toBe(false);
		}
	});

	test('every signal permutation resolves to exactly one kind', () => {
		const grants: (boolean | 'unknown')[] = [true, false, 'unknown'];
		const actives: (boolean | 'unknown')[] = [true, false, 'unknown'];
		const subs: SubState[] = ['ok', 'pending', 'failing', 'unenrolled', 'unknown'];
		for (const grant of grants)
			for (const active of actives)
				for (const sub of subs) {
					const r = connectionUiState({ grant, active, status: 'unknown', sub });
					expect(typeof r.kind).toBe('string');
					// online is reachable only through the one true path.
					if (r.kind === 'online') {
						expect(grant).toBe(true);
						expect(active).toBe(true);
						expect(sub).toBe('ok');
					}
				}
	});
});
