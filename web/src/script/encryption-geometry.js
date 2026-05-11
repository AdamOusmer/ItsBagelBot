/**
 * Shared geometry data for the Encryption scene. Computed once during boot
 * idle time and cached on `window.__itsbagelbotPreload.encryptionData`, so
 * `initEncryption` consumes ready-made TypedArrays instead of regenerating
 * curve samples / Fibonacci sphere positions / particle fields at scroll-in.
 *
 * The numbers here are intentionally exported — the Encryption update loop
 * needs `FLOW_CURVE_SAMPLES` to interpret the sample arrays, and the
 * BufferAttribute count needs to match `N_NODES`.
 */

export const N_NODES = 14;
export const NODE_RADIUS = 4.2;
export const NODE_CURVE_SEGMENTS = 32;
export const INTER_CURVE_SEGMENTS = 24;
export const FLOW_CURVE_SAMPLES = 32;

/**
 * @param {typeof import('three')} THREE
 * @param {{ isCompact: boolean }} options
 */
export function buildEncryptionGeometryData(THREE, {isCompact}) {
    const perConn = isCompact ? 3 : 4;
    const bgCount = isCompact ? 320 : 600;
    const {Vector3, CatmullRomCurve3} = THREE;

    const nodePositions = new Float32Array(N_NODES * 3);
    for (let i = 0; i < N_NODES; i++) {
        const phi = Math.acos(1 - 2 * (i + 0.5) / N_NODES);
        const theta = Math.PI * (1 + Math.sqrt(5)) * i;
        nodePositions[i * 3] = NODE_RADIUS * Math.cos(theta) * Math.sin(phi);
        nodePositions[i * 3 + 1] = NODE_RADIUS * Math.sin(theta) * Math.sin(phi);
        nodePositions[i * 3 + 2] = NODE_RADIUS * Math.cos(phi);
    }

    const curves = [];
    const nodeSegments = [];
    const interSegments = [];

    const pushSegments = (curve, segs, target) => {
        const points = curve.getPoints(segs);
        for (let i = 0; i < points.length - 1; i++) {
            const a = points[i];
            const b = points[i + 1];
            target.push(a.x, a.y, a.z, b.x, b.y, b.z);
        }
    };

    for (let i = 0; i < N_NODES; i++) {
        const px = nodePositions[i * 3];
        const py = nodePositions[i * 3 + 1];
        const pz = nodePositions[i * 3 + 2];
        const a = (i / N_NODES) * Math.PI * 2;
        const mx = px * 0.5 + Math.cos(a) * 0.6;
        const my = py * 0.5 + Math.sin(a) * 0.6;
        const mz = pz * 0.5 + Math.cos(a + 1.3) * 0.6;
        const curve = new CatmullRomCurve3([
            new Vector3(px, py, pz),
            new Vector3(mx, my, mz),
            new Vector3(),
        ], false, 'catmullrom', 0.5);
        curves.push(curve);
        pushSegments(curve, NODE_CURVE_SEGMENTS, nodeSegments);
    }

    for (const step of [3, 7]) {
        for (let i = 0; i < N_NODES; i += 3) {
            const j = (i + step) % N_NODES;
            const ax = nodePositions[i * 3];
            const ay = nodePositions[i * 3 + 1];
            const az = nodePositions[i * 3 + 2];
            const bx = nodePositions[j * 3];
            const by = nodePositions[j * 3 + 1];
            const bz = nodePositions[j * 3 + 2];
            const mx = (ax + bx) * 0.35;
            const my = (ay + by) * 0.35;
            const mz = (az + bz) * 0.35;
            const curve = new CatmullRomCurve3([
                new Vector3(ax, ay, az),
                new Vector3(mx, my, mz),
                new Vector3(bx, by, bz),
            ]);
            curves.push(curve);
            pushSegments(curve, INTER_CURVE_SEGMENTS, interSegments);
        }
    }

    const sampleTmp = new Vector3();
    const flowSampleSets = curves.map((curve) => {
        const samples = new Float32Array((FLOW_CURVE_SAMPLES + 1) * 3);
        for (let i = 0; i <= FLOW_CURVE_SAMPLES; i++) {
            curve.getPoint(i / FLOW_CURVE_SAMPLES, sampleTmp);
            const offset = i * 3;
            samples[offset] = sampleTmp.x;
            samples[offset + 1] = sampleTmp.y;
            samples[offset + 2] = sampleTmp.z;
        }
        return samples;
    });

    const flowCount = curves.length * perConn;
    const flowSamples = new Array(flowCount);
    const flowT = new Float32Array(flowCount);
    const flowSpeed = new Float32Array(flowCount);
    let pi = 0;
    for (let ci = 0; ci < curves.length; ci++) {
        for (let i = 0; i < perConn; i++) {
            flowSamples[pi] = flowSampleSets[ci];
            flowT[pi] = i / perConn;
            flowSpeed[pi] = 0.06 + ((ci + i) % 4) * 0.022;
            pi++;
        }
    }

    const bgPositions = new Float32Array(bgCount * 3);
    const bgVelocities = new Float32Array(bgCount * 3);
    for (let i = 0; i < bgCount; i++) {
        bgPositions[i * 3] = (Math.random() - 0.5) * 28;
        bgPositions[i * 3 + 1] = (Math.random() - 0.5) * 28;
        bgPositions[i * 3 + 2] = (Math.random() - 0.5) * 28;
        bgVelocities[i * 3] = (Math.random() - 0.5) * 0.004;
        bgVelocities[i * 3 + 1] = (Math.random() - 0.5) * 0.004;
        bgVelocities[i * 3 + 2] = (Math.random() - 0.5) * 0.004;
    }

    return {
        isCompact,
        perConn,
        bgCount,
        nodePositions,
        nodeConnectionPoints: new Float32Array(nodeSegments),
        interConnectionPoints: new Float32Array(interSegments),
        flowSampleSets,
        flowCount,
        flowSamples,
        flowT,
        flowSpeed,
        bgPositions,
        bgVelocities,
    };
}
