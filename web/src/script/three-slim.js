// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * Tree-shakeable facade over `three` for the Encryption scene.
 *
 * `import('three')` pulls the whole namespace, which defeats Rollup's
 * tree-shaking and shipped the full 724 kB (183 kB gzip) build to every
 * homepage visitor. Re-exporting only the names the scene actually touches
 * lets the bundler drop the rest (loaders, animation, audio, shadows, …).
 *
 * If the scene starts using a new THREE export, add it here — a missing name
 * fails loudly at init with `X is not a constructor` / `undefined`, and the
 * Playwright test "encryption scene boots" catches it.
 */
export {
    WebGLRenderer, Scene, Group, PerspectiveCamera,
    LineBasicMaterial, MeshBasicMaterial, PointsMaterial,
    LineSegments, Mesh, Points,
    IcosahedronGeometry, SphereGeometry, OctahedronGeometry,
    TetrahedronGeometry, DodecahedronGeometry, BufferGeometry,
    WireframeGeometry, EdgesGeometry,
    Vector3, Vector2, BufferAttribute, Timer,
    CatmullRomCurve3,
    ACESFilmicToneMapping, SRGBColorSpace, AdditiveBlending, HalfFloatType,
} from 'three';
