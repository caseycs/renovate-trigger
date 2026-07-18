import React from 'react';
import { spring, useCurrentFrame, useVideoConfig, interpolate } from 'remotion';
import { theme } from '../theme';

export const Box: React.FC<{
  x: number;
  y: number;
  w: number;
  h: number;
  title: string;
  subtitle?: string;
  titleSize?: number;
  appearAt: number;
  activeFrom?: number;
  activeTo?: number;
  children?: React.ReactNode;
}> = ({ x, y, w, h, title, subtitle, titleSize = 26, appearAt, activeFrom = -1, activeTo = -1, children }) => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();

  const enter = spring({ frame: frame - appearAt, fps, config: { damping: 200 } });
  const opacity = interpolate(enter, [0, 1], [0, 1]);
  const scale = interpolate(enter, [0, 1], [0.9, 1]);

  const active = frame >= activeFrom && frame <= activeTo && activeFrom >= 0;

  return (
    <div
      style={{
        position: 'absolute',
        left: x,
        top: y,
        width: w,
        height: h,
        transform: `scale(${scale})`,
        opacity,
        background: active ? theme.panelActive : theme.panel,
        border: `3px solid ${active ? theme.strokeActive : theme.stroke}`,
        borderRadius: 16,
        padding: 18,
        boxSizing: 'border-box',
        display: 'flex',
        flexDirection: 'column',
        gap: 8,
        fontFamily: theme.sans,
      }}
    >
      <div style={{ color: theme.text, fontSize: titleSize, fontWeight: 700 }}>{title}</div>
      {subtitle ? (
        <div style={{ color: theme.subtle, fontSize: 20, fontFamily: theme.mono }}>{subtitle}</div>
      ) : null}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8, marginTop: 4 }}>{children}</div>
    </div>
  );
};
