import type { PageServerLoad } from './$types';
import { userEnrollment, type EnrollmentWire } from '$lib/server/services';
import { isDemo } from '$lib/server/access';
import { demoEnrollment, sampleEnrollment } from '$lib/server/sample';

// Window sizes the page offers; the users service clamps to 90 anyway.
const WINDOWS = new Set([7, 30, 90]);

function parseWindow(raw: string | null): number {
  const days = Number(raw ?? '30');
  return WINDOWS.has(days) ? days : 30;
}

export type AnalyticsBundle = { enrollment: EnrollmentWire; degraded: boolean };

// Streamed: the shell renders immediately; the enrollment series hydrates in.
export const load: PageServerLoad = ({ url }) => {
  const days = parseWindow(url.searchParams.get('window'));

  const bundle: Promise<AnalyticsBundle> = isDemo()
    ? Promise.resolve({ enrollment: demoEnrollment(days), degraded: false })
    : userEnrollment(days)
        .then((enrollment) => ({ enrollment, degraded: false }))
        .catch(() => ({ enrollment: sampleEnrollment, degraded: true }));

  return { bundle, days };
};
