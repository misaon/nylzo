import { prefersReducedMotion } from 'svelte/motion';

export function motionMs(milliseconds: number): number {
  return prefersReducedMotion.current ? 0 : milliseconds;
}
