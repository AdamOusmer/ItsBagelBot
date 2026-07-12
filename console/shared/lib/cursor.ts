import { writable } from 'svelte/store';

// Client-side source of truth for the custom-cursor preference. Kept out of the
// i18n/context path on purpose: the toggle (settings + onboarding) lives in a
// different render subtree than the Cursor component (RootShell), so a store is
// the natural bridge for an instant live flip.
//
// SSR safety: never mutate this during server render (it is a module singleton
// shared across concurrent requests). RootShell seeds it client-only from the
// per-user preference, and Cursor only reads it inside an effect, which never
// runs on the server. Default `true` preserves the current behaviour.
export const customCursor = writable(true);
