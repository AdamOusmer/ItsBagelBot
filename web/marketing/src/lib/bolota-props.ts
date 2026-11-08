// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { readClientProps } from '@bagel/kit/client-props';
interface BolotaProps {
    name: string;
    size: number;
    palette: { head: string; eye: string };
    expression?: string;
    follow?: boolean;
}
export function bolotaProps(svg: SVGSVGElement): BolotaProps {
    const payload = svg.nextElementSibling;
    if (!payload?.matches('script.bb-client-props')) throw new Error('Missing bolota props');
    return readClientProps<BolotaProps>(payload);
}
