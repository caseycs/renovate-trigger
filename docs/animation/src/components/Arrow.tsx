import React from 'react';
import { interpolate, useCurrentFrame } from 'remotion';
import { theme } from '../theme';

// A straight arrow from (x1,y1) to (x2,y2) with a label and a token that flows
// along it during [flowFrom, flowTo]. Rendered on a full-canvas SVG overlay.
export const Arrow: React.FC<{
  id: string;
  x1: number;
  y1: number;
  x2: number;
  y2: number;
  label: string;
  labelX: number;
  labelY: number;
  labelAnchor?: 'start' | 'middle' | 'end';
  appearAt: number;
  flowFrom: number;
  flowTo: number;
  width: number;
  height: number;
}> = ({
  id,
  x1,
  y1,
  x2,
  y2,
  label,
  labelX,
  labelY,
  labelAnchor = 'start',
  appearAt,
  flowFrom,
  flowTo,
  width,
  height,
}) => {
  const frame = useCurrentFrame();
  const lineOpacity = interpolate(frame, [appearAt, appearAt + 12], [0, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
  });
  const t = interpolate(frame, [flowFrom, flowTo], [0, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
  });
  const tokenX = x1 + (x2 - x1) * t;
  const tokenY = y1 + (y2 - y1) * t;
  const tokenVisible = frame >= flowFrom && frame <= flowTo;

  return (
    <svg
      width={width}
      height={height}
      style={{ position: 'absolute', left: 0, top: 0, pointerEvents: 'none' }}
    >
      <defs>
        <marker id={`head-${id}`} markerWidth="12" markerHeight="12" refX="8" refY="4" orient="auto">
          <path d="M0,0 L8,4 L0,8 Z" fill={theme.stroke} />
        </marker>
      </defs>
      <line
        x1={x1}
        y1={y1}
        x2={x2}
        y2={y2}
        stroke={theme.stroke}
        strokeWidth={3}
        opacity={lineOpacity}
        markerEnd={`url(#head-${id})`}
      />
      <text
        x={labelX}
        y={labelY}
        fill={theme.subtle}
        fontSize={17}
        fontFamily={theme.sans}
        textAnchor={labelAnchor}
        opacity={lineOpacity}
      >
        {label}
      </text>
      {tokenVisible ? <circle cx={tokenX} cy={tokenY} r={9} fill={theme.accent} /> : null}
    </svg>
  );
};
