// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { error } from '@sveltejs/kit';
import { errorCode } from '#lib/error-routes';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = ({ url }) => error(errorCode(url.searchParams.get('code')), 'Internal Error');
