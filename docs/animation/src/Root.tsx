import React from 'react';
import { Composition } from 'remotion';
import { Showcase } from './Showcase';

export const Root: React.FC = () => {
  return (
    <Composition
      id="Showcase"
      component={Showcase}
      durationInFrames={460}
      fps={30}
      width={1280}
      height={720}
    />
  );
};
