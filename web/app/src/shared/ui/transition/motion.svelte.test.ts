import { DetachedWindowAPI } from 'happy-dom';
import { afterEach, describe, expect, it } from 'vitest';

import { collapse, motionMs } from './motion.ts';

function reducedMotion(matches: boolean): void {
  const environment: object = globalThis;
  if (!('happyDOM' in environment) || !(environment.happyDOM instanceof DetachedWindowAPI)) {
    throw new TypeError('these tests need the happy-dom environment');
  }
  environment.happyDOM.settings.device.prefersReducedMotion = matches ? 'reduce' : 'no-preference';
}

describe('motion', () => {
  afterEach(() => {
    reducedMotion(false);
  });

  it('drops every duration for reduced motion', () => {
    expect(motionMs(400)).toBe(400);
    reducedMotion(true);
    expect(motionMs(400)).toBe(0);
  });

  it('collapses an element while fading it from its own opacity', () => {
    const element = document.createElement('div');
    element.style.opacity = '0.8';
    element.style.height = '100px';
    document.body.append(element);

    const transition = collapse(element, { duration: 300 });
    const halfway = transition.css?.(0.5, 0.5) ?? '';

    expect(transition.duration).toBe(300);
    expect(halfway).toContain('overflow: hidden');
    expect(halfway).not.toContain(';;');
    expect(halfway).toMatch(/[^;];opacity:0\.4$/u);
    element.remove();
  });
});
