// Height field, camera and visibility for the hero terrain.
// Ported from the design-time generator (mesh.py): same features, same camera,
// so the live page matches the Paper artboard.

export const A = 1.85; // ground aspect: world x spans [0, A], y spans [0, 1]
const NEG = 0.35; // basins are shallower than hills so floors stay visible at this camera

// amplitude, cx, cy, sx, sy — negative amplitude = basin
const FEATS: [number, number, number, number, number][] = [
  [-0.16, 0.26, 0.42, 0.13, 0.11],
  [-0.14, 0.7, 0.5, 0.14, 0.12],
  [-0.11, 0.86, 0.16, 0.1, 0.09],
  [0.15, 0.5, 0.22, 0.16, 0.12],
  [0.11, 0.1, 0.14, 0.13, 0.1],
  [0.09, 0.5, 0.8, 0.2, 0.1],
  [-0.1, 0.14, 0.8, 0.11, 0.09],
  [0.08, 0.92, 0.78, 0.12, 0.09],
  [0.06, 0.34, 0.66, 0.08, 0.07],
  [-0.07, 0.58, 0.66, 0.07, 0.06],
  [0.05, 0.75, 0.32, 0.06, 0.06],
  [0.07, 0.28, 0.1, 0.07, 0.06],
  [0.06, 0.62, 0.06, 0.09, 0.05],
  [-0.05, 0.44, 0.5, 0.06, 0.05],
  [0.05, 0.9, 0.5, 0.06, 0.08],
  [0.04, 0.05, 0.5, 0.05, 0.08],
];

export function height(x: number, y: number): number {
  let z = 0;
  for (const [a, cx, cy, sx, sy] of FEATS) {
    const amp = a > 0 ? a : a * NEG;
    const dx = (x - cx) / sx;
    const dy = (y - cy) / sy;
    z += amp * Math.exp(-(dx * dx + dy * dy));
  }
  z += 0.006 * Math.sin(x * 23 + 1.3) * Math.cos(y * 19 + 0.4) + 0.003 * Math.sin(x * 41 - y * 37);
  return z * 1.8 + 0.2;
}

export function grad(x: number, y: number, h = 0.002): [number, number] {
  return [
    (height(x + h, y) - height(x - h, y)) / (2 * h),
    (height(x, y + h) - height(x, y - h)) / (2 * h),
  ];
}

// ---- camera -----------------------------------------------------------------
type V3 = [number, number, number];
const C: V3 = [A / 2, -0.95, 1.45];
const T: V3 = [A / 2, 0.45, 0];

const norm = (v: V3): V3 => {
  const l = Math.hypot(v[0], v[1], v[2]);
  return [v[0] / l, v[1] / l, v[2] / l];
};
const cross = (a: V3, b: V3): V3 => [
  a[1] * b[2] - a[2] * b[1],
  a[2] * b[0] - a[0] * b[2],
  a[0] * b[1] - a[1] * b[0],
];
const dot = (a: V3, b: V3) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];

const FW = norm([T[0] - C[0], T[1] - C[1], T[2] - C[2]]);
const RT = norm(cross(FW, [0, 0, 1]));
const UP = cross(RT, FW);

/** Raw camera-plane projection of a world point (x in [0,1] before aspect). */
export function projRaw(x: number, y: number, z: number): [number, number] {
  const v: V3 = [x * A - C[0], y - C[1], z - C[2]];
  const d = dot(v, FW);
  return [dot(v, RT) / d, dot(v, UP) / d];
}

/** Bounding box of the projected terrain, computed once. */
export const BOUNDS = (() => {
  let minx = Infinity, maxx = -Infinity, miny = Infinity, maxy = -Infinity;
  for (let i = 0; i <= 20; i++) {
    for (let j = 0; j <= 20; j++) {
      const x = i / 20, y = j / 20;
      const [sx, sy] = projRaw(x, y, height(x, y));
      if (sx < minx) minx = sx;
      if (sx > maxx) maxx = sx;
      if (sy < miny) miny = sy;
      if (sy > maxy) maxy = sy;
    }
  }
  return { minx, maxx, miny, maxy, cx: (minx + maxx) / 2, cy: (miny + maxy) / 2 };
})();

/** Height/width ratio of the terrain's projected bounding box. */
export const ASPECT = (BOUNDS.maxy - BOUNDS.miny) / (BOUNDS.maxx - BOUNDS.minx);

export interface Screen {
  w: number;
  h: number;
  F: number;
}

export function makeScreen(w: number, h: number, margin: number): Screen {
  const F = Math.min(
    (w - 2 * margin) / (BOUNDS.maxx - BOUNDS.minx),
    (h - 2 * margin) / (BOUNDS.maxy - BOUNDS.miny),
  );
  return { w, h, F };
}

export function project(s: Screen, x: number, y: number, dz = 0): [number, number] {
  const [sx, sy] = projRaw(x, y, height(x, y) + dz);
  return [s.w / 2 + (sx - BOUNDS.cx) * s.F, s.h / 2 - (sy - BOUNDS.cy) * s.F];
}

/** True if the surface point is not hidden behind terrain from the camera. */
export function visible(x: number, y: number, dz = 0.004): boolean {
  const z = height(x, y) + dz;
  const px = x * A, py = y, pz = z;
  const N = 60;
  for (let k = 1; k < N; k++) {
    const t = k / N;
    const qx = px + (C[0] - px) * t;
    const qy = py + (C[1] - py) * t;
    const qz = pz + (C[2] - pz) * t;
    if (height(qx / A, qy) > qz) return false;
  }
  return true;
}
