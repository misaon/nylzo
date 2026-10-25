<script lang="ts">
  import type { ClassValue } from 'svelte/elements';

  import { ICONS, type IconName, type Shape } from './icons.ts';

  interface Props {
    readonly name: IconName;
    readonly class?: ClassValue;
    readonly stroke?: number;
  }

  const { name, class: className = 'size-4', stroke = 1.8 }: Props = $props();
  const shapes: readonly Shape[] = $derived(ICONS[name]);
</script>

<svg
  class={className}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width={stroke}
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
  focusable="false"
>
  {#each shapes as shape (shape)}
    {#if 'path' in shape}
      <path d={shape.path}></path>
    {:else if 'circle' in shape}
      <circle cx={shape.circle[0]} cy={shape.circle[1]} r={shape.circle[2]}></circle>
    {:else if 'rect' in shape}
      <rect x={shape.rect[0]} y={shape.rect[1]} width={shape.rect[2]} height={shape.rect[3]} rx={shape.rect[4]}></rect>
    {:else}
      <ellipse
        cx={shape.ellipse[0]}
        cy={shape.ellipse[1]}
        rx={shape.ellipse[2]}
        ry={shape.ellipse[3]}
        transform={shape.rotate ? `rotate(${shape.rotate} 12 12)` : undefined}
      ></ellipse>
    {/if}
  {/each}
</svg>
