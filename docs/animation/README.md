# Showcase animation

Source for the looping showcase GIF in the root README
([`renovate-trigger.gif`](./renovate-trigger.gif)) — the end-to-end pipeline:
tag on a dependency repo → GitHub App webhook → renovate-trigger (batch → gate →
resolve) → a one-off Renovate Job → a PR in the Argo CD (dependent) repo.

Built with [Remotion](https://www.remotion.dev/) (code-first, React/TSX). Remotion
is free for individuals and small companies; this internal-docs use is within its
free tier.

## Re-render

```sh
cd docs/animation
npm install
npm run render            # → renovate-trigger.gif
```

`npm run render` runs:

```sh
remotion render Showcase renovate-trigger.gif --codec=gif --every-nth-frame=2 --number-of-gif-loops=0
```

Preview/iterate live with `npm run studio`.

## Layout

- `src/Root.tsx` — the single `Showcase` composition (1280×720, 30fps, ~15s).
- `src/Showcase.tsx` — the vertical pipeline, beat timings, and captions.
- `src/components/` — `Box`, `Chip`, `Arrow`, `Caption`.
- `src/theme.ts` — flat, GIF-friendly palette (accept=green / ignore=yellow /
  reject=red, matching `WORKFLOWS.md` §3).

Wording mirrors `CONTEXT.md` and `WORKFLOWS.md`; keep them in sync if the flow
changes.
