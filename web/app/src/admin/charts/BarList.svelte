<script lang="ts">
  import { flip } from 'svelte/animate';

  import { motionMs } from '../../shared/ui/transition/motion.ts';
  import { sum } from './geometry/scale.ts';
  import type { BarItem, Tone } from './tones.ts';
  import Track from './Track.svelte';

  interface Props {
    readonly label: string;
    readonly items: readonly BarItem[];
    readonly tone: Tone;
    readonly formatValue: (value: number) => string;
    readonly formatShare: (share: number) => string;
    readonly highlighted?: string | undefined;
    readonly onhighlight?: ((key: string | undefined) => void) | undefined;
    readonly compact?: boolean;
  }

  const REORDER_MS = 400;

  const { label, items, tone, formatValue, formatShare, highlighted, onhighlight, compact = false }: Props = $props();
  const padding = $derived(compact ? 'px-3 py-1.5' : 'px-3 py-2.5');
  const total = $derived(sum(items.map((item) => item.value)));
  const largest = $derived(Math.max(0, ...items.map((item) => item.value)));
</script>

{#snippet row(item: BarItem)}
  <span class="flex items-baseline gap-3">
    <span class="min-w-0 flex-1 truncate text-ui text-ink-soft">{item.label}</span>
    <span class="text-ui font-medium text-ink tabular-nums">{formatValue(item.value)}</span>
    <span class="w-12 text-right text-caption text-ink-mute tabular-nums">
      {formatShare(total > 0 ? item.value / total : 0)}
    </span>
  </span>
  <Track class={compact ? 'mt-1.5' : 'mt-2'} value={largest > 0 ? item.value / largest : 0} {tone} />
{/snippet}

<ul role="list" aria-label={label} class={compact ? 'grid' : 'grid gap-1'}>
  {#each items as item (item.key)}
    <li animate:flip={{ duration: motionMs(REORDER_MS) }}>
      {#if onhighlight}
        <button
          type="button"
          class={[
            'block w-full rounded-control text-left transition-colors',
            padding,
            highlighted === item.key ? 'bg-white/8' : 'hover:bg-white/5',
          ]}
          onclick={() => onhighlight(item.key)}
          onpointerenter={() => onhighlight(item.key)}
          onpointerleave={(event) => {
            if (event.pointerType === 'mouse') {
              onhighlight(undefined);
            }
          }}
          onfocus={() => onhighlight(item.key)}
          onblur={() => onhighlight(undefined)}
        >
          {@render row(item)}
        </button>
      {:else}
        <div class={padding}>{@render row(item)}</div>
      {/if}
    </li>
  {/each}
</ul>
