import type { IconName } from '../ui/icons.ts';

export interface PipelineNode {
  readonly id: string;
  readonly label: string;
  readonly icon: IconName;
  readonly title: string;
  readonly detail: string;
}
