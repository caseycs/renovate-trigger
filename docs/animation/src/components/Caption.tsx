import React from 'react';
import { interpolate, useCurrentFrame } from 'remotion';
import { theme } from '../theme';

export type Beat = { from: number; text: string };

// Bottom caption that cross-fades between beats based on the current frame.
export const Caption: React.FC<{ beats: Beat[]; width: number; y: number }> = ({
  beats,
  width,
  y,
}) => {
  const frame = useCurrentFrame();
  // Find the active beat (last beat whose `from` <= frame).
  let idx = 0;
  for (let i = 0; i < beats.length; i++) {
    if (frame >= beats[i].from) idx = i;
  }
  const beat = beats[idx];
  const fade = interpolate(frame, [beat.from, beat.from + 10], [0, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
  });

  return (
    <div
      style={{
        position: 'absolute',
        left: 0,
        top: y,
        width,
        textAlign: 'center',
        opacity: fade,
        color: theme.text,
        fontSize: 30,
        fontWeight: 600,
        fontFamily: theme.sans,
        padding: '0 60px',
        boxSizing: 'border-box',
      }}
    >
      {beat.text}
    </div>
  );
};
