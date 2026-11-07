<script lang="ts">
  import type { ClassValue } from 'svelte/elements';

  import Icon from '../icon/Icon.svelte';
  import type { IconName } from '../icon/icons.ts';

  interface Props {
    readonly icon: IconName;
    readonly tone: 'current' | 'neutral' | 'warning' | 'danger';
    readonly size?: 'large' | 'medium' | 'small';
    readonly class?: ClassValue | undefined;
    readonly iconClass?: ClassValue;
    readonly stroke?: number;
  }

  const TONES = {
    current: 'tile-current text-white',
    neutral: 'tile-neutral text-ink-soft',
    warning: 'tile-warning text-white',
    danger: 'tile-danger text-white',
  } as const;
  const SIZES = {
    large: { tile: 'size-12 rounded-pane', icon: 'size-6', stroke: 1.6 },
    medium: { tile: 'size-10 rounded-control', icon: 'size-5', stroke: 1.7 },
    small: { tile: 'size-8 rounded-control', icon: 'size-4', stroke: 1.8 },
  } as const;

  const { icon, tone, size = 'large', class: className, iconClass, stroke }: Props = $props();
</script>

<span class={['grid shrink-0 place-items-center', SIZES[size].tile, TONES[tone], className]} aria-hidden="true">
  <Icon name={icon} class={iconClass ?? SIZES[size].icon} stroke={stroke ?? SIZES[size].stroke} />
</span>
