import React from 'react';
import { AbsoluteFill, useVideoConfig } from 'remotion';
import { theme } from './theme';
import { Box } from './components/Box';
import { Chip } from './components/Chip';
import { Arrow } from './components/Arrow';
import { Caption, Beat } from './components/Caption';

// Vertical pipeline: a new tag on a dependency repo travels down through
// renovate-trigger (batch → gate → resolve) into a one-off Renovate Job that
// opens a PR in the Argo CD (dependent) repo. Loops.
export const Showcase: React.FC = () => {
  const { width, height } = useVideoConfig();

  const COL_X = 260;
  const COL_W = 760;
  const labelX = COL_X + COL_W + 16; // 1036, labels sit to the right of the column

  // Row geometry (y, height)
  const r1 = { y: 64, h: 88 };
  const r2 = { y: 212, h: 140 };
  const r3 = { y: 410, h: 88 };
  const r4 = { y: 556, h: 88 };
  const cx = COL_X + COL_W / 2;

  const beats: Beat[] = [
    { from: 0, text: 'A new tag is pushed on a dependency repo' },
    { from: 55, text: 'GitHub App delivers the webhook (HMAC-verified)' },
    { from: 100, text: 'renovate-trigger batches it over a tumbling window' },
    { from: 160, text: 'Gate: never overlap an active Renovate run' },
    { from: 220, text: 'Resolve dependents from renovate.trigger.json' },
    { from: 285, text: 'Clone the Renovate CronJob into a one-off Job' },
    { from: 360, text: 'Renovate opens a PR in the Argo CD repo' },
  ];

  return (
    <AbsoluteFill style={{ background: theme.bg }}>
      {/* Title */}
      <div
        style={{
          position: 'absolute',
          top: 20,
          width,
          textAlign: 'center',
          color: theme.text,
          fontFamily: theme.mono,
          fontSize: 24,
          letterSpacing: 1,
        }}
      >
        renovate-trigger — how it works
      </div>

      {/* Arrows (drawn under the boxes' text but over the bg) */}
      <Arrow
        id="a1"
        x1={cx}
        y1={r1.y + r1.h}
        x2={cx}
        y2={r2.y}
        label="GitHub App webhook"
        labelX={labelX}
        labelY={(r1.y + r1.h + r2.y) / 2 + 5}
        appearAt={50}
        flowFrom={55}
        flowTo={95}
        width={width}
        height={height}
      />
      <Arrow
        id="a2"
        x1={cx}
        y1={r2.y + r2.h}
        x2={cx}
        y2={r3.y}
        label="clone CronJob → Job"
        labelX={labelX}
        labelY={(r2.y + r2.h + r3.y) / 2 + 5}
        appearAt={270}
        flowFrom={275}
        flowTo={315}
        width={width}
        height={height}
      />
      <Arrow
        id="a3"
        x1={cx}
        y1={r3.y + r3.h}
        x2={cx}
        y2={r4.y}
        label="opens PR"
        labelX={labelX}
        labelY={(r3.y + r3.h + r4.y) / 2 + 5}
        appearAt={345}
        flowFrom={350}
        flowTo={390}
        width={width}
        height={height}
      />

      {/* R1 — Dependency repo */}
      <Box
        x={COL_X}
        y={r1.y}
        w={COL_W}
        h={r1.h}
        title="Dependency repo"
        subtitle="org/lib-foo"
        appearAt={0}
        activeFrom={0}
        activeTo={54}
      >
        <div style={{ position: 'absolute', right: 18, top: 22 }}>
          <Chip label="new tag: v1.4.0" appearAt={14} tone="accept" mono />
        </div>
      </Box>

      {/* R2 — renovate-trigger (internal steps) */}
      <Box
        x={COL_X}
        y={r2.y}
        w={COL_W}
        h={r2.h}
        title="renovate-trigger"
        subtitle="single replica"
        appearAt={55}
        activeFrom={95}
        activeTo={265}
      >
        <div style={{ display: 'flex', flexDirection: 'row', gap: 10, marginTop: 6, flexWrap: 'wrap' }}>
          <Chip label="Batch · 30s window" appearAt={105} tone="accent" />
          <Chip label="RunGate · no active run" appearAt={165} tone="accept" />
          <Chip label="Resolve trigger file" appearAt={225} tone="accent" />
        </div>
      </Box>

      {/* R3 — Renovate run */}
      <Box
        x={COL_X}
        y={r3.y}
        w={COL_W}
        h={r3.h}
        title="Renovate run"
        subtitle="Kubernetes Job (cloned from CronJob)"
        appearAt={285}
        activeFrom={300}
        activeTo={349}
      >
        <div style={{ position: 'absolute', right: 18, top: 26 }}>
          <Chip label={'RENOVATE_REPOSITORIES=["org/argocd-config"]'} appearAt={302} tone="accent" mono />
        </div>
      </Box>

      {/* R4 — Argo CD (dependent) repo */}
      <Box
        x={COL_X}
        y={r4.y}
        w={COL_W}
        h={r4.h}
        title="Argo CD repo (Dependent)"
        subtitle="org/argocd-config"
        appearAt={360}
        activeFrom={380}
        activeTo={450}
      >
        <div style={{ position: 'absolute', right: 18, top: 26 }}>
          <Chip label="PR opened · bump image → v1.4.0" appearAt={382} tone="accept" />
        </div>
      </Box>

      <Caption beats={beats} width={width} y={664} />
    </AbsoluteFill>
  );
};
