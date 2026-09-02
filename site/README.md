# iris — landing page

Static Next.js site for iris. Implements the "LANDING v3" Paper artboard.

```
npm install
npm run dev      # http://localhost:3000
npm run build    # static export to ./out
```

- `app/` — layout (fonts, theme bootstrap), page copy, global styles and tokens.
- `components/terrain/` — the hero: `field.ts` (height field, camera, visibility), `sim.ts` (mesh render, gradient-descent agents, sandboxes), `Terrain.tsx` (canvas + controls).
- `components/illustrations/` — the four use-case illustrations. Greyed by default; they activate and animate on hover or when scrolled into view (`components/Illustration.tsx`).
- Theme follows the system setting, with a toggle in the nav (stored in `localStorage`). Append `?theme=dark` or `?theme=light` to force one.
