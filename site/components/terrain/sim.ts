import { ASPECT, Screen, grad, height, makeScreen, project, visible } from "./field";

export interface Palette {
  meshLine: string;
  meshFill: string;
  ink: string;
  muted: string;
  dash: string;
  agents: string[];
  font: string;
}

interface Pt { x: number; y: number; vis: boolean }

interface Agent {
  ci: number; // colour index into palette.agents
  x: number; y: number;
  vx: number; vy: number;
  trail: Pt[];
  stillFrames: number;
  settledAt: number | null;
  bornAt: number;
  frame: number;
}

interface Sandbox {
  rect: [number, number, number, number]; // x0,y0,x1,y1 (world)
  open: boolean;
  openedAt: number | null;
  outline: Pt[]; // draped, world-space with visibility
  polygon: [number, number][]; // projected, unclipped (hit testing)
}

export const MAX_AGENTS = 5;
const SANDBOX_RECTS: [number, number, number, number][] = [
  [0.1, 0.36, 0.36, 0.58],
  [0.72, 0.02, 0.98, 0.3],
  [0.02, 0.66, 0.28, 0.94],
];
const START_POINTS: [number, number][] = [
  [0.44, 0.22],
  [0.6, 0.8],
];

const LR = 0.0026;
const MOM = 0.8;
const MARGIN = 20;

export class TerrainSim {
  private canvas: HTMLCanvasElement;
  private ctx: CanvasRenderingContext2D;
  private mesh: HTMLCanvasElement | null = null;
  private screen: Screen = { w: 0, h: 0, F: 1 };
  private dpr = 1;
  private palette: Palette;
  private agents: Agent[] = [];
  private sandboxes: Sandbox[];
  private raf = 0;
  private last = 0;
  onChange?: () => void;

  constructor(canvas: HTMLCanvasElement, palette: Palette) {
    this.canvas = canvas;
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("no 2d context");
    this.ctx = ctx;
    this.palette = palette;
    this.sandboxes = SANDBOX_RECTS.map((rect, i) => ({
      rect,
      open: i === 0,
      openedAt: null,
      outline: drapedOutline(rect),
      polygon: [],
    }));
    for (const [x, y] of START_POINTS) this.spawn(x, y);
  }

  get agentCount() { return this.agents.length; }
  get openCount() { return this.sandboxes.filter((s) => s.open).length; }
  get sandboxCount() { return this.sandboxes.length; }

  static heightFor(width: number) {
    return Math.round(width * ASPECT + 2 * MARGIN);
  }

  setPalette(p: Palette) {
    this.palette = p;
    this.renderMesh();
  }

  resize(width: number, height: number) {
    this.dpr = Math.min(window.devicePixelRatio || 1, 2);
    this.canvas.width = Math.round(width * this.dpr);
    this.canvas.height = Math.round(height * this.dpr);
    this.canvas.style.width = `${width}px`;
    this.canvas.style.height = `${height}px`;
    this.screen = makeScreen(width, height, MARGIN);
    for (const s of this.sandboxes) s.polygon = this.projectPolygon(s.rect);
    this.renderMesh();
  }

  start() {
    const loop = (t: number) => {
      this.tick(t);
      this.raf = requestAnimationFrame(loop);
    };
    this.raf = requestAnimationFrame(loop);
  }

  stop() { cancelAnimationFrame(this.raf); }

  addAgent(): boolean {
    if (this.agents.length >= MAX_AGENTS) return false;
    const p = this.randomStart();
    this.spawn(p[0], p[1]);
    this.onChange?.();
    return true;
  }

  unlockNext(): boolean {
    const s = this.sandboxes.find((b) => !b.open);
    if (!s) return false;
    s.open = true;
    s.openedAt = performance.now();
    this.renderMesh();
    this.onChange?.();
    return true;
  }

  /** Returns the index of a locked sandbox under the CSS-pixel point, or -1. */
  lockedAt(px: number, py: number): number {
    return this.sandboxes.findIndex((s) => !s.open && pointInPolygon(px, py, s.polygon));
  }

  unlockAt(px: number, py: number): boolean {
    const i = this.lockedAt(px, py);
    if (i < 0) return false;
    this.sandboxes[i].open = true;
    this.sandboxes[i].openedAt = performance.now();
    this.renderMesh();
    this.onChange?.();
    return true;
  }

  // ---- internals -------------------------------------------------------------

  private spawn(x: number, y: number) {
    this.agents.push({
      ci: this.agents.length,
      x, y, vx: 0, vy: 0,
      trail: [{ x, y, vis: visible(x, y) }],
      stillFrames: 0,
      settledAt: null,
      bornAt: performance.now(),
      frame: 0,
    });
  }

  private randomStart(): [number, number] {
    for (let i = 0; i < 80; i++) {
      const x = 0.06 + Math.random() * 0.88;
      const y = 0.08 + Math.random() * 0.84;
      if (this.inLocked(x, y)) continue;
      if (!visible(x, y)) continue;
      const [gx, gy] = grad(x, y);
      if (Math.hypot(gx, gy) < 0.08) continue; // flat spots make dull descents
      return [x, y];
    }
    return [0.5, 0.5];
  }

  private inLocked(x: number, y: number) {
    return this.sandboxes.some((s) => !s.open && inRect(x, y, s.rect));
  }

  private projectPolygon(r: [number, number, number, number]): [number, number][] {
    const [x0, y0, x1, y1] = r;
    const pts: [number, number][] = [];
    const n = 12;
    for (let k = 0; k < n; k++) pts.push(project(this.screen, x0 + ((x1 - x0) * k) / n, y0));
    for (let k = 0; k < n; k++) pts.push(project(this.screen, x1, y0 + ((y1 - y0) * k) / n));
    for (let k = 0; k < n; k++) pts.push(project(this.screen, x1 - ((x1 - x0) * k) / n, y1));
    for (let k = 0; k < n; k++) pts.push(project(this.screen, x0, y1 - ((y1 - y0) * k) / n));
    return pts;
  }

  private renderMesh() {
    const { w, h } = this.screen;
    if (!w || !h) return;
    const mesh = document.createElement("canvas");
    mesh.width = this.canvas.width;
    mesh.height = this.canvas.height;
    const ctx = mesh.getContext("2d")!;
    ctx.setTransform(this.dpr, 0, 0, this.dpr, 0, 0);
    const nx = Math.max(28, Math.min(60, Math.round(w / 18)));
    const ny = Math.round(nx * 0.6);
    ctx.fillStyle = this.palette.meshFill;
    ctx.strokeStyle = this.palette.meshLine;
    ctx.lineWidth = 0.8;
    ctx.lineJoin = "round";
    // strips back to front so nearer terrain paints over farther terrain.
    // Quads inside a locked environment are filled but not stroked: the grid
    // underneath is hidden until it is unlocked.
    for (let j = ny; j > 0; j--) {
      const yb = j / ny, yf = (j - 1) / ny;
      const open = new Path2D();
      const locked = new Path2D();
      for (let i = 0; i < nx; i++) {
        const x0 = i / nx, x1 = (i + 1) / nx;
        const target = this.inLocked((x0 + x1) / 2, (yb + yf) / 2) ? locked : open;
        const a = project(this.screen, x0, yb);
        const b = project(this.screen, x1, yb);
        const c = project(this.screen, x1, yf);
        const d = project(this.screen, x0, yf);
        target.moveTo(a[0], a[1]);
        target.lineTo(b[0], b[1]);
        target.lineTo(c[0], c[1]);
        target.lineTo(d[0], d[1]);
        target.closePath();
      }
      ctx.fill(locked);
      ctx.fill(open);
      ctx.stroke(open);
    }
    this.mesh = mesh;
  }

  private step(a: Agent, now: number) {
    if (a.settledAt !== null) {
      if (now - a.settledAt > 2800) this.respawn(a, now);
      return;
    }
    const [gx, gy] = grad(a.x, a.y);
    a.vx = MOM * a.vx - LR * gx;
    a.vy = MOM * a.vy - LR * gy;
    const nx = clamp(a.x + a.vx, 0.02, 0.98);
    const ny = clamp(a.y + a.vy, 0.02, 0.98);
    if (this.inLocked(nx, ny)) {
      // locked sandboxes are walls: stop at the boundary and settle there
      a.vx = a.vy = 0;
      a.settledAt = now;
      return;
    }
    const moved = Math.hypot(nx - a.x, ny - a.y);
    a.x = nx; a.y = ny;
    a.frame++;
    if (moved < 1.5e-4) a.stillFrames++; else a.stillFrames = 0;
    if (a.frame % 3 === 0 && moved > 1e-4) a.trail.push({ x: a.x, y: a.y, vis: visible(a.x, a.y) });
    if (a.stillFrames > 40 && a.frame > 30) a.settledAt = now;
  }

  private respawn(a: Agent, now: number) {
    const [x, y] = this.randomStart();
    a.x = x; a.y = y; a.vx = a.vy = 0;
    a.trail = [{ x, y, vis: visible(x, y) }];
    a.stillFrames = 0; a.settledAt = null; a.bornAt = now; a.frame = 0;
  }

  private tick(now: number) {
    if (!this.mesh) return;
    const ctx = this.ctx;
    // fixed-rate simulation regardless of display refresh
    if (!this.last) this.last = now;
    let steps = Math.min(4, Math.floor((now - this.last) / (1000 / 60)));
    if (steps > 0) this.last = now;
    while (steps-- > 0) for (const a of this.agents) this.step(a, now);

    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.clearRect(0, 0, this.canvas.width, this.canvas.height);
    ctx.drawImage(this.mesh, 0, 0);
    ctx.setTransform(this.dpr, 0, 0, this.dpr, 0, 0);

    for (const s of this.sandboxes) this.drawSandbox(ctx, s, now);
    for (const a of this.agents) this.drawAgent(ctx, a, now);
  }

  private drawSandbox(ctx: CanvasRenderingContext2D, s: Sandbox, now: number) {
    const p = this.palette;
    const t = s.openedAt === null ? 1 : clamp((now - s.openedAt) / 450, 0, 1);
    const openness = s.open ? t : 0;
    const runs = visibleRuns(this.screen, s.outline, true);
    // dashed locked outline fades out, solid open outline fades in
    if (openness < 1) {
      ctx.save();
      ctx.globalAlpha = 1 - openness;
      ctx.strokeStyle = p.dash;
      ctx.lineWidth = 1;
      ctx.setLineDash([4, 3]);
      strokeRuns(ctx, runs);
      ctx.restore();
    }
    if (openness > 0) {
      ctx.save();
      ctx.globalAlpha = openness;
      ctx.strokeStyle = p.ink;
      ctx.lineWidth = 1.2;
      ctx.setLineDash([]);
      strokeRuns(ctx, runs);
      ctx.restore();
    }
    // label at the back-left corner
    const [x0, , , y1] = s.rect;
    const [lx, ly] = project(this.screen, x0, y1, 0.004);
    const scale = Math.max(0.75, Math.min(1, this.screen.w / 1120));
    ctx.save();
    ctx.font = `500 ${10 * scale}px ${p.font}`;
    ctx.textBaseline = "top";
    if (s.open) {
      ctx.fillStyle = p.ink;
      ctx.globalAlpha = openness;
      fillSpaced(ctx, "ENVIRONMENT", lx + 2, ly + 4 * scale, 1.2 * scale);
    } else {
      ctx.fillStyle = p.muted;
      ctx.strokeStyle = p.muted;
      ctx.lineWidth = 1.1;
      const gx = lx + 2, gy = ly + 4 * scale;
      // lock glyph
      ctx.strokeRect(gx + 0.5, gy + 4.5, 7, 5);
      ctx.beginPath();
      ctx.arc(gx + 4, gy + 4.2, 2.3, Math.PI, 0);
      ctx.stroke();
      fillSpaced(ctx, "LOCKED", gx + 12, gy, 1.2 * scale);
    }
    ctx.restore();
  }

  private drawAgent(ctx: CanvasRenderingContext2D, a: Agent, now: number) {
    const born = clamp((now - a.bornAt) / 500, 0, 1);
    const color = this.palette.agents[a.ci % this.palette.agents.length];
    const runs = visibleRuns(this.screen, a.trail, false);
    ctx.save();
    ctx.globalAlpha = born;
    ctx.strokeStyle = color;
    ctx.fillStyle = color;
    ctx.lineWidth = 1.5;
    ctx.lineCap = "round";
    ctx.setLineDash([3, 4]);
    strokeRuns(ctx, runs);
    ctx.setLineDash([]);
    // step markers
    for (let i = 3; i < a.trail.length - 1; i += 4) {
      const t = a.trail[i];
      if (!t.vis) continue;
      const [px, py] = project(this.screen, t.x, t.y, 0.004);
      ctx.beginPath();
      ctx.arc(px, py, 2, 0, Math.PI * 2);
      ctx.fill();
    }
    // start ring
    const s0 = a.trail[0];
    if (s0.vis) {
      const [sx, sy] = project(this.screen, s0.x, s0.y, 0.004);
      ctx.beginPath();
      ctx.arc(sx, sy, 3.5, 0, Math.PI * 2);
      ctx.fillStyle = this.palette.meshFill;
      ctx.fill();
      ctx.stroke();
      ctx.fillStyle = color;
    }
    // current position
    const vis = visible(a.x, a.y);
    const [cx, cy] = project(this.screen, a.x, a.y, 0.004);
    ctx.globalAlpha = born * (vis ? 1 : 0.3);
    ctx.beginPath();
    ctx.arc(cx, cy, 6, 0, Math.PI * 2);
    ctx.fill();
    const pulse = a.settledAt === null ? 0.45 : 0.25 + 0.25 * Math.sin(now / 350);
    ctx.globalAlpha = born * pulse * (vis ? 1 : 0.3);
    ctx.lineWidth = 1;
    ctx.beginPath();
    ctx.arc(cx, cy, 12, 0, Math.PI * 2);
    ctx.stroke();
    ctx.restore();
  }
}

// ---- helpers ----------------------------------------------------------------

function clamp(v: number, lo: number, hi: number) { return v < lo ? lo : v > hi ? hi : v; }

function inRect(x: number, y: number, r: [number, number, number, number]) {
  return x >= r[0] && x <= r[2] && y >= r[1] && y <= r[3];
}

function drapedOutline(r: [number, number, number, number]): Pt[] {
  const [x0, y0, x1, y1] = r;
  const n = 30;
  const pts: Pt[] = [];
  const push = (x: number, y: number) => pts.push({ x, y, vis: visible(x, y) });
  for (let k = 0; k < n; k++) push(x0 + ((x1 - x0) * k) / n, y0);
  for (let k = 0; k < n; k++) push(x1, y0 + ((y1 - y0) * k) / n);
  for (let k = 0; k < n; k++) push(x1 - ((x1 - x0) * k) / n, y1);
  for (let k = 0; k <= n; k++) push(x0, y1 - ((y1 - y0) * k) / n);
  return pts;
}

function visibleRuns(s: Screen, pts: Pt[], closed: boolean): [number, number][][] {
  const runs: [number, number][][] = [];
  let cur: [number, number][] = [];
  const seq = closed ? [...pts, pts[0]] : pts;
  for (const p of seq) {
    if (p.vis) cur.push(project(s, p.x, p.y, 0.004));
    else { if (cur.length > 1) runs.push(cur); cur = []; }
  }
  if (cur.length > 1) runs.push(cur);
  return runs;
}

function strokeRuns(ctx: CanvasRenderingContext2D, runs: [number, number][][]) {
  ctx.beginPath();
  for (const run of runs) {
    ctx.moveTo(run[0][0], run[0][1]);
    for (let i = 1; i < run.length; i++) ctx.lineTo(run[i][0], run[i][1]);
  }
  ctx.stroke();
}

function fillSpaced(ctx: CanvasRenderingContext2D, text: string, x: number, y: number, tracking: number) {
  let cx = x;
  for (const ch of text) {
    ctx.fillText(ch, cx, y);
    cx += ctx.measureText(ch).width + tracking;
  }
}

function pointInPolygon(px: number, py: number, poly: [number, number][]) {
  let inside = false;
  for (let i = 0, j = poly.length - 1; i < poly.length; j = i++) {
    const [xi, yi] = poly[i], [xj, yj] = poly[j];
    if (yi > py !== yj > py && px < ((xj - xi) * (py - yi)) / (yj - yi) + xi) inside = !inside;
  }
  return inside;
}

export { height };
