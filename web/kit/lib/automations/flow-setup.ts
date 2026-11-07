// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const SETUP_REWARD_ID = 'setup-required';
export const SETUP_FILTER_VALUE = 'setup-required';
export const SETUP_RUN_TARGET = 'setup-required';
// A leading underscore is not a legal Twitch login, so this can never match a real account.
export const SETUP_LOGIN = '_setup_required';
export const SETUP_URL = 'https://example.com/setup-required';
export const SETUP_TEXT = 'setup-required';
export const SETUP_SCENE_VALUE = '"setup-required"';

const SETUP_VALUES: ReadonlySet<unknown> = new Set([SETUP_REWARD_ID, SETUP_LOGIN, SETUP_URL, SETUP_SCENE_VALUE]);

export const isSetupValue = (value: unknown): boolean => SETUP_VALUES.has(value);
