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

// Two dependency repos (GitHub) get tagged → batched in the cluster. Once the
// window closes and the gate is clear, renovate-trigger reaches back into *each*
// source repo to read its renovate.trigger.json (the dependents declaration),
// dedups them, and clones the Renovate CronJob into a one-off Job → a PR back in
// the Argo CD (dependent) repo on GitHub. Loops.
export const Showcase: React.FC = () => {
  const { width, height } = useVideoConfig();

  // Groups
  const gh = { x: 40, y: 60, w: 560, h: 604 }; // GitHub
  const k8s = { x: 680, y: 60, w: 560, h: 604 }; // Kubernetes cluster

  // Nodes inside GitHub
  const repo1 = { x: 48, y: 120, w: 264, h: 180 };
  const repo2 = { x: 326, y: 120, w: 264, h: 180 };
  const argocd = { x: 60, y: 474, w: 520, h: 150 };
  // Nodes inside the cluster
  const trigger = { x: 700, y: 120, w: 520, h: 250 };
  const run = { x: 700, y: 474, w: 520, h: 150 };

  const beats: Beat[] = [
    { from: 0, text: 'Two tags land on dependency repos' },
    { from: 60, text: 'GitHub App delivers the webhooks (HMAC-verified)' },
    { from: 100, text: 'Batched together over a 30s tumbling window' },
    { from: 165, text: 'Window closes — gate: never overlap an active run' },
    { from: 215, text: 'Read renovate.trigger.json from each source repo' },
    { from: 285, text: 'Dedup the declared dependents → a single run' },
    { from: 345, text: 'Clone the Renovate CronJob into a one-off Job' },
    { from: 420, text: 'Renovate opens a PR in the Argo CD repo' },
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

      {/* Arrows (drawn before the boxes so opaque panels occlude any crossings) */}
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

      {/* Read renovate.trigger.json: renovate-trigger reads back from each source
          repo. Same routing as the webhook arrows, source & target reversed
          (trigger → repo), in accent so they stay visible over the grey webhooks. */}
      <Arrow
        id="read1"
        x1={trigger.x}
        y1={trigger.y + 70}
        x2={repo1.x + repo1.w}
        y2={repo1.y + repo1.h / 2}
        label=""
        labelX={0}
        labelY={0}
        appearAt={205}
        flowFrom={214}
        flowTo={254}
        width={width}
        height={height}
        color={theme.accent}
      />
      <Arrow
        id="read2"
        x1={trigger.x}
        y1={trigger.y + 110}
        x2={repo2.x + repo2.w}
        y2={repo2.y + repo2.h / 2}
        label=""
        labelX={0}
        labelY={0}
        appearAt={205}
        flowFrom={226}
        flowTo={266}
        width={width}
        height={height}
        color={theme.accent}
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
        appearAt={337}
        flowFrom={341}
        flowTo={381}
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
        appearAt={413}
        flowFrom={415}
        flowTo={459}
        width={width}
        height={height}
      />

      {/* GitHub: two dependency repos + the Argo CD repo. Each source repo lights
          up twice — on its tag, and again while its renovate.trigger.json is read. */}
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
        activeFrom2={210}
        activeTo2={284}
      >
        <Chip label="tag: v1.4.0" appearAt={14} tone="accept" mono />
        <Chip label="→ org/argocd-config" appearAt={224} tone="accent" mono />
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
        activeFrom2={210}
        activeTo2={284}
      >
        <Chip label="tag: v2.1.0" appearAt={36} tone="accept" mono />
        <Chip label="→ org/argocd-config" appearAt={238} tone="accent" mono />
      </Box>

      <Box
        x={argocd.x}
        y={argocd.y}
        w={argocd.w}
        h={argocd.h}
        title="Argo CD repo (Dependent)"
        subtitle="org/argocd-config"
        appearAt={409}
        activeFrom={445}
        activeTo={519}
      >
        <Chip label="PR opened · lib-foo → v1.4.0 · lib-bar → v2.1.0" appearAt={447} tone="accept" />
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
        activeTo={345}
      >
        <div style={{ display: 'flex', flexDirection: 'row', gap: 10, marginTop: 10, flexWrap: 'wrap' }}>
          <Chip label="Batch · 2 repos · 30s window" appearAt={115} tone="accent" />
          <Chip label="RunGate · no active run" appearAt={172} tone="accept" />
          <Chip label="Fetch · renovate.trigger.json ×2" appearAt={218} tone="accent" />
          <Chip label="Dedup · 2 → 1 dependent" appearAt={290} tone="accept" />
        </div>
      </Box>

      <Box
        x={run.x}
        y={run.y}
        w={run.w}
        h={run.h}
        title="Renovate run"
        subtitle="Kubernetes Job (cloned from CronJob)"
        appearAt={350}
        activeFrom={365}
        activeTo={414}
      >
        <Chip label={'RENOVATE_REPOSITORIES=["org/argocd-config"]'} appearAt={367} tone="accent" mono />
      </Box>

      <Caption beats={beats} width={width} y={678} />
    </AbsoluteFill>
  );
};
