import type { PageServerLoad } from './$types';
import {
  serviceHealth,
  userEnrollment,
  type EnrollmentWire,
  type ServiceHealth
} from '$lib/server/services';
import { dev } from '$app/environment';
import { emptyEnrollment } from '$lib/server/fallback';

// Window sizes the page offers; the users service clamps to 90 anyway.
const WINDOWS = new Set([7, 30, 90]);
const DEMO = dev && process.env.DEMO === '1';

function parseWindow(raw: string | null): number {
  const days = Number(raw ?? '30');
  return WINDOWS.has(days) ? days : 30;
}

export type AnalyticsBundle = { enrollment: EnrollmentWire; degraded: boolean };

// Streamed: the shell renders immediately; the enrollment series hydrates in.
export const load: PageServerLoad = ({ url }) => {
  const days = parseWindow(url.searchParams.get('window'));

  const bundle: Promise<AnalyticsBundle> = DEMO
    ? import('$lib/server/demo-data').then(({ demoEnrollment }) => ({
        enrollment: demoEnrollment(days),
        degraded: false
      }))
    : userEnrollment(days)
        .then((enrollment) => ({ enrollment, degraded: false }))
        .catch(() => ({ enrollment: emptyEnrollment(days), degraded: true }));

  // RPC timing is diagnostic data, not an analytics-page dependency. Stream it
  // independently so a missing responder can only hold its own cells until the
  // probe timeout; enrollment still renders as soon as the users read lands.
  const health: Promise<ServiceHealth[]> = DEMO
    ? import('$lib/server/demo-data').then(({ sampleHealth }) => sampleHealth)
    : serviceHealth();

  return { bundle, health, days };
};
