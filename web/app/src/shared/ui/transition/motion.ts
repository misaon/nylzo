import { prefersReducedMotion } from 'svelte/motion';
import { slide } from 'svelte/transition';
import type { SlideParams, TransitionConfig } from 'svelte/transition';

export function motionMs(milliseconds: number): number {
  return prefersReducedMotion.current ? 0 : milliseconds;
}

// oxlint-disable-next-line typescript/prefer-readonly-parameter-types -- Svelte hands transitions a live element and its own parameter type.
export function collapse(node: Element, params?: SlideParams): TransitionConfig {
  const sliding = slide(node, params);
  const opacity = Number(getComputedStyle(node).opacity);
  return { ...sliding, css: (t, u) => `${(sliding.css?.(t, u) ?? '').replace(/;+$/u, '')};opacity:${t * opacity}` };
}
