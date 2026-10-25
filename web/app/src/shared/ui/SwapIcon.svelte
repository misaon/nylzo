<script lang="ts">
  import type { ClassValue } from 'svelte/elements';

  import type { IconName } from './icons.ts';

  import Icon from './Icon.svelte';

  interface Props {
    readonly from: IconName;
    readonly to: IconName;
    readonly swapped: boolean;
    readonly size?: string;
    readonly toStroke?: number;
    readonly toTone?: ClassValue;
    readonly class?: ClassValue;
  }

  const { from, to, swapped, size = 'size-4', toStroke, toTone, class: className }: Props = $props();
</script>

<span class={[className, 'stack place-items-center']}>
  <Icon
    name={from}
    class={[
      size,
      'transition-[opacity,scale] duration-300 ease-silk',
      swapped ? 'scale-50 opacity-0' : 'scale-100 opacity-100',
    ]}
  />
  <Icon
    name={to}
    stroke={toStroke}
    class={[
      size,
      'transition-[opacity,scale] duration-300 ease-silk',
      toTone,
      swapped ? 'scale-100 opacity-100' : 'scale-50 opacity-0',
    ]}
  />
</span>
