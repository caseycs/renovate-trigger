import React from 'react';
import { AbsoluteFill, useVideoConfig } from 'remotion';
import { theme } from './theme';
import { Box } from './components/Box';
import { Chip } from './components/Chip';
import { Arrow } from './components/Arrow';
import { Caption, Beat } from './components/Caption';

// A labelled container that groups nodes (GitHub vs the Kubernetes cluster).
const GroupBox: React.FC<{
  x: number;
  y: number;
  w: number;
  h: number;
  label: string;
}> = ({ x, y, w, h, label }) => (
  <div
    style={{
      position: 'absolute',
      left: x,
      top: y,
      width: w,
      height: h,
      border: `2px dashed ${theme.stroke}`,
      borderRadius: 20,
      background: 'rgba(255,255,255,0.02)',
    }}
  >
    <div
      style={{
        position: 'absolute',
        left: 18,
        top: 12,
        color: theme.subtle,
        fontFamily: theme.mono,
        fontSize: 20,
        letterSpacing: 1,
      }}
    >
      {label}
    </div>
  </div>
);

// Two dependency repos (GitHub) get tagged → batched in the cluster
// (renovate-trigger: batch → gate → resolve) → a one-off Renovate Job → a PR
// back in the Argo CD (dependent) repo on GitHub. Loops.
export const Showcase: React.FC = () => {
  const { width, height } = useVideoConfig();

  // Groups
  const gh = { x: 40, y: 60, w: 560, h: 604 }; // GitHub
  const k8s = { x: 680, y: 60, w: 560, h: 604 }; // Kubernetes cluster

  // Nodes inside GitHub
  const repo1 = { x: 60, y: 120, w: 252, h: 150 };
  const repo2 = { x: 328, y: 120, w: 252, h: 150 };
  const argocd = { x: 60, y: 474, w: 520, h: 150 };
  // Nodes inside the cluster
  const trigger = { x: 700, y: 120, w: 520, h: 230 };
  const run = { x: 700, y: 474, w: 520, h: 150 };

  const beats: Beat[] = [
    { from: 0, text: 'Two tags land on dependency repos' },
    { from: 60, text: 'GitHub App delivers the webhooks (HMAC-verified)' },
    { from: 100, text: 'Batched together over a tumbling window' },
    { from: 165, text: 'Gate: never overlap an active Renovate run' },
    { from: 220, text: 'Resolve dependents from renovate.trigger.json (deduped)' },
    { from: 280, text: 'Clone the Renovate CronJob into a one-off Job' },
    { from: 355, text: 'Renovate opens a PR in the Argo CD repo' },
  ];

  return (
    <AbsoluteFill style={{ background: theme.bg }}>
      <div
        style={{
          position: 'absolute',
          top: 16,
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

      {/* Group containers (behind everything) */}
      <GroupBox x={gh.x} y={gh.y} w={gh.w} h={gh.h} label="GitHub" />
      <GroupBox x={k8s.x} y={k8s.y} w={k8s.w} h={k8s.h} label="Kubernetes cluster" />

      {/* Arrows */}
      <Arrow
        id="wh1"
        x1={repo1.x + repo1.w}
        y1={repo1.y + repo1.h / 2}
        x2={trigger.x}
        y2={trigger.y + 70}
        label="webhook"
        labelX={640}
        labelY={150}
        labelAnchor="middle"
        appearAt={56}
        flowFrom={60}
        flowTo={100}
        width={width}
        height={height}
      />
      <Arrow
        id="wh2"
        x1={repo2.x + repo2.w}
        y1={repo2.y + repo2.h / 2}
        x2={trigger.x}
        y2={trigger.y + 110}
        label=""
        labelX={0}
        labelY={0}
        appearAt={56}
        flowFrom={78}
        flowTo={118}
        width={width}
        height={height}
      />
      <Arrow
        id="clone"
        x1={trigger.x + trigger.w / 2}
        y1={trigger.y + trigger.h}
        x2={trigger.x + trigger.w / 2}
        y2={run.y}
        label="clone CronJob → Job"
        labelX={trigger.x + trigger.w / 2 + 20}
        labelY={(trigger.y + trigger.h + run.y) / 2 + 5}
        labelAnchor="start"
        appearAt={272}
        flowFrom={276}
        flowTo={316}
        width={width}
        height={height}
      />
      <Arrow
        id="pr"
        x1={run.x}
        y1={run.y + run.h / 2}
        x2={argocd.x + argocd.w}
        y2={argocd.y + argocd.h / 2}
        label="opens PR"
        labelX={640}
        labelY={run.y + run.h / 2 - 14}
        labelAnchor="middle"
        appearAt={348}
        flowFrom={350}
        flowTo={394}
        width={width}
        height={height}
      />

      {/* GitHub: two dependency repos + the Argo CD repo */}
      <Box
        x={repo1.x}
        y={repo1.y}
        w={repo1.w}
        h={repo1.h}
        title="Dependency repo"
        titleSize={22}
        subtitle="org/lib-foo"
        appearAt={0}
        activeFrom={0}
        activeTo={54}
      >
        <Chip label="tag: v1.4.0" appearAt={14} tone="accept" mono />
      </Box>

      <Box
        x={repo2.x}
        y={repo2.y}
        w={repo2.w}
        h={repo2.h}
        title="Dependency repo"
        titleSize={22}
        subtitle="org/lib-bar"
        appearAt={22}
        activeFrom={22}
        activeTo={54}
      >
        <Chip label="tag: v2.1.0" appearAt={36} tone="accept" mono />
      </Box>

      <Box
        x={argocd.x}
        y={argocd.y}
        w={argocd.w}
        h={argocd.h}
        title="Argo CD repo (Dependent)"
        subtitle="org/argocd-config"
        appearAt={344}
        activeFrom={380}
        activeTo={460}
      >
        <Chip label="PR opened · bump image → v1.4.0" appearAt={382} tone="accept" />
      </Box>

      {/* Cluster: renovate-trigger + the Renovate run */}
      <Box
        x={trigger.x}
        y={trigger.y}
        w={trigger.w}
        h={trigger.h}
        title="renovate-trigger"
        appearAt={55}
        activeFrom={95}
        activeTo={270}
      >
        <div style={{ display: 'flex', flexDirection: 'row', gap: 10, marginTop: 10, flexWrap: 'wrap' }}>
          <Chip label="Batch · 2 repos · 30s window" appearAt={115} tone="accent" />
          <Chip label="RunGate · no active run" appearAt={170} tone="accept" />
          <Chip label="Resolve · dedup → 1 dependent" appearAt={225} tone="accent" />
        </div>
      </Box>

      <Box
        x={run.x}
        y={run.y}
        w={run.w}
        h={run.h}
        title="Renovate run"
        subtitle="Kubernetes Job (cloned from CronJob)"
        appearAt={285}
        activeFrom={300}
        activeTo={349}
      >
        <Chip label={'RENOVATE_REPOSITORIES=["org/argocd-config"]'} appearAt={302} tone="accent" mono />
      </Box>

      <Caption beats={beats} width={width} y={678} />
    </AbsoluteFill>
  );
};
