<script lang="ts">
  import { share, sum } from './geometry/scale.ts';
  import { tweenedSeries } from './motion.svelte.ts';
  import { FILLS, SWATCHES } from './tones.ts';
  import type { ChartSeries } from './tones.ts';

  interface Props {
    readonly label: string;
    readonly parts: readonly Omit<ChartSeries, 'values'>[];
    readonly values: readonly number[];
    readonly formatValue: (value: number) => string;
    readonly formatShare: (share: number) => string;
  }

  const SPAN = 1000;
  const GAP = 4;

  const { label, parts, values, formatValue, formatShare }: Props = $props();
  const total = $derived(sum(values));
  const widths = tweenedSeries(() => values.map((value) => share(value, total) * SPAN));

  function offset(index: number): number {
    return sum(widths.current.slice(0, index));
  }
</script>

<div>
  <span class="relative block h-3 overflow-hidden rounded-full bg-white/6 shadow-rim">
    <svg class="absolute inset-0 size-full" viewBox="0 0 {SPAN} 1" preserveAspectRatio="none" aria-hidden="true">
      {#each parts as part, i (part.label)}
        <rect
          x={offset(i) + (i === 0 ? 0 : GAP / 2)}
          width={Math.max(0, (widths.current[i] ?? 0) - (i === 0 ? GAP / 2 : GAP))}
          height="1"
          class={FILLS[part.tone]}
        ></rect>
      {/each}
    </svg>
  </span>
  <ul role="list" aria-label={label} class="mt-4 grid gap-x-6 gap-y-3 sm:grid-cols-3">
    {#each parts as part, i (part.label)}
      <li class="flex items-start gap-2.5">
        <span class={['mt-1.5 size-2 shrink-0 rounded-full', SWATCHES[part.tone]]} aria-hidden="true"></span>
        <span class="min-w-0">
          <span class="block text-caption text-ink-soft">{part.label}</span>
          <span class="block text-ui text-ink tabular-nums">
            <span class="font-medium">{formatValue(values[i] ?? 0)}</span>
            <span class="text-ink-mute">· {formatShare(share(values[i] ?? 0, total))}</span>
          </span>
        </span>
      </li>
    {/each}
  </ul>
</div>
