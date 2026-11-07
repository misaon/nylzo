<script lang="ts">
  import type { ClassValue } from 'svelte/elements';

  import { ICON_MOTION, ICONS } from './icons.ts';
  import type { IconName, Shape } from './icons.ts';

  interface Props {
    readonly name: IconName;
    readonly class?: ClassValue;
    readonly stroke?: number | undefined;
    readonly animated?: boolean;
    readonly paint?: string;
  }

  const { name, class: className = 'size-4', stroke = 1.8, animated = false, paint = 'currentColor' }: Props = $props();
  const shapes: readonly Shape[] = $derived(ICONS[name]);
  const motion = $derived(animated ? ICON_MOTION[name] : undefined);
  const svgMotion = $derived(motion?.shape === undefined ? motion?.classes : undefined);
</script>

<svg
  class={[className, svgMotion]}
  viewBox="0 0 24 24"
  fill="none"
  stroke={paint}
  stroke-width={stroke}
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
  focusable="false"
>
  {#each shapes as shape, index (shape)}
    {const shapeMotion = $derived(motion?.shape === index ? motion.classes : undefined)}
    {#if 'path' in shape}
      <path class={shapeMotion} d={shape.path}></path>
    {:else if 'circle' in shape}
      <circle class={shapeMotion} cx={shape.circle[0]} cy={shape.circle[1]} r={shape.circle[2]}></circle>
    {:else if 'rect' in shape}
      <rect
        class={shapeMotion}
        x={shape.rect[0]}
        y={shape.rect[1]}
        width={shape.rect[2]}
        height={shape.rect[3]}
        rx={shape.rect[4]}
      ></rect>
    {:else}
      <ellipse
        class={shapeMotion}
        cx={shape.ellipse[0]}
        cy={shape.ellipse[1]}
        rx={shape.ellipse[2]}
        ry={shape.ellipse[3]}
        transform={shape.rotate ? `rotate(${shape.rotate} 12 12)` : undefined}
      ></ellipse>
    {/if}
  {/each}
</svg>
