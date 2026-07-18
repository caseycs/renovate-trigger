import React from 'react';
import { spring, useCurrentFrame, useVideoConfig, interpolate } from 'remotion';
import { theme } from '../theme';

// A small pill that fades/slides in at `appearAt`. `tone` picks the accent.
export const Chip: React.FC<{
  label: string;
  appearAt: number;
  tone?: 'accent' | 'accept' | 'warn' | 'reject' | 'neutral';
  mono?: boolean;
}> = ({ label, appearAt, tone = 'neutral', mono = false }) => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const enter = spring({ frame: frame - appearAt, fps, config: { damping: 200 } });
  const toneColor: Record<string, string> = {
    neutral: theme.subtle,
    accent: theme.accent,
    accept: theme.accept,
    warn: theme.warn,
    reject: theme.reject,
  };
  const color = toneColor[tone];

  return (
    <div
      style={{
        opacity: enter,
        transform: `translateX(${interpolate(enter, [0, 1], [-12, 0])}px)`,
        display: 'inline-flex',
        alignItems: 'center',
        alignSelf: 'flex-start',
        maxWidth: '100%',
        padding: '6px 12px',
        borderRadius: 999,
        border: `2px solid ${color}`,
        background: 'rgba(255,255,255,0.03)',
        color: theme.text,
        fontSize: 17,
        fontFamily: mono ? theme.mono : theme.sans,
        whiteSpace: 'nowrap',
        overflow: 'hidden',
        textOverflow: 'ellipsis',
      }}
    >
      {label}
    </div>
  );
};
