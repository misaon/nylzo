import type { IconName } from '../ui/icons.ts';

export interface PipelineNode {
  readonly id: string;
  readonly label: string;
  readonly icon: IconName;
  readonly title: string;
  readonly detail: string;
}

export interface PaceInput {
  readonly elapsedMs: number;
  readonly minMs: number;
  readonly steps: number;
  readonly reached: number;
  readonly fraction?: number | undefined;
}

export interface Pace {
  readonly step: number;
  readonly progress: number;
  readonly finished: boolean;
}

const UNKNOWN_FRACTION = 0.9;
const MAX_FRACTION = 0.95;

export function pace({ elapsedMs, minMs, steps, reached, fraction }: PaceInput): Pace {
  const timed = minMs <= 0 ? 1 : Math.min(1, Math.max(0, elapsedMs / minMs));
  const within = Math.min(MAX_FRACTION, Math.max(0, fraction ?? UNKNOWN_FRACTION));
  const real = reached >= steps ? 1 : (Math.max(0, reached) + within) / steps;
  const progress = Math.min(timed, real);
  return {
    step: Math.min(steps - 1, Math.floor(progress * steps)),
    progress,
    finished: reached >= steps && timed >= 1,
  };
}
