import type { LayoutServerLoad } from './$types';

// Root layout data is inherited by every route (including the pre-login
// login/goodbye pages), so the resolved locale reaches the i18n context no
// matter where the visitor lands.
export const load: LayoutServerLoad = ({ locals }) => ({
  locale: locals.locale,
  cursorEnabled: locals.cursorEnabled
});
