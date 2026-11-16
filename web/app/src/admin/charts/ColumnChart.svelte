<script lang="ts">
  import type { ClassValue } from 'svelte/elements';

  import ChartTooltip from './ChartTooltip.svelte';
  import { bandIndex, steppedIndex } from './geometry/pointer.ts';
  import { groupSeries, niceTicks, sum } from './geometry/scale.ts';
  import { tweenedNumber, tweenedSeries } from './motion.svelte.ts';
  import { FILLS } from './tones.ts';
  import type { ChartSeries, TooltipRow } from './tones.ts';

  interface Props {
    readonly label: string;
    readonly series: readonly ChartSeries[];
    readonly class?: ClassValue;
    readonly hint?: string | undefined;
    readonly formatValue: (value: number) => string;
    readonly formatTick: (value: number) => string;
    readonly periodLabel: (first: number, last: number) => string;
    readonly tickLabel: (index: number) => string;
    readonly tickIndexes: (width: number) => readonly number[];
  }

  const TOP = 10;
  const RIGHT = 6;
  const BOTTOM = 26;
  const LEFT = 46;
  const MINIMUM_BAND = 9;
  const COLUMN_SHARE = 0.64;
  const LABEL_EDGE = 28;

  const {
    label,
    series,
    class: className,
    hint,
    formatValue,
    formatTick,
    periodLabel,
    tickLabel,
    tickIndexes,
  }: Props = $props();

  let width = $state(0);
  let height = $state(0);
  let active = $state<number | undefined>();

  const left = LEFT;
  const right = $derived(Math.max(LEFT + 1, width - RIGHT));
  const bottom = $derived(Math.max(TOP + 1, height - BOTTOM));
  const count = $derived(series[0]?.values.length ?? 0);
  const fitting = $derived(Math.max(1, Math.floor((right - left) / MINIMUM_BAND)));
  const groupSize = $derived(Math.max(1, Math.ceil(count / fitting)));
  const grouped = $derived(series.map((entry) => groupSeries(entry.values, groupSize)));
  const groups = $derived(grouped[0]?.length ?? 0);
  const totals = $derived(Array.from({ length: groups }, (_, i) => sum(grouped.map((values) => values[i] ?? 0))));
  const scale = $derived(niceTicks(Math.max(0, ...totals)));
  const top = tweenedNumber(() => scale.max);
  const heights = tweenedSeries(() => grouped.flat());
  const band = $derived(groups === 0 ? 0 : (right - left) / groups);
  const columnWidth = $derived(Math.max(1.5, band * COLUMN_SHARE));
  const shown = $derived(Math.min(active ?? groups - 1, groups - 1));
  const xTicks = $derived(
    Map.groupBy(tickIndexes(right - left), (index) => Math.floor(index / groupSize))
      .entries()
      .map(([group, indexes]) => ({ group, index: indexes[0] ?? group * groupSize }))
      .toArray(),
  );

  function yAt(value: number): number {
    return bottom - (Math.max(0, value) / top.current) * (bottom - TOP);
  }

  function center(group: number): number {
    return left + (group + 0.5) * band;
  }

  function anchor(x: number): 'start' | 'middle' | 'end' {
    if (x - left < LABEL_EDGE) {
      return 'start';
    }
    return right - x < LABEL_EDGE ? 'end' : 'middle';
  }

  function stack(
    group: number,
  ): { readonly y: number; readonly height: number; readonly tone: string; readonly key: string }[] {
    let base = 0;
    return series.map((entry, layer) => {
      const value = heights.current[layer * groups + group] ?? 0;
      const segment = {
        y: yAt(base + value),
        height: Math.max(0, yAt(base) - yAt(base + value) - (base > 0 ? 1.5 : 0)),
        tone: FILLS[entry.tone],
        key: entry.label,
      };
      base += value;
      return segment;
    });
  }

  function title(group: number): string {
    const first = group * groupSize;
    return periodLabel(first, Math.min(count - 1, first + groupSize - 1));
  }

  function rows(group: number): TooltipRow[] {
    return series.map((entry, layer) => ({
      label: entry.label,
      value: formatValue(grouped[layer]?.[group] ?? 0),
      tone: entry.tone,
    }));
  }

  const valueText = $derived(
    groups === 0
      ? label
      : `${title(shown)}: ${rows(shown)
          .map((row) => `${row.label} ${row.value}`)
          .join(', ')}`,
  );

  function follow(event: PointerEvent & { readonly currentTarget: HTMLDivElement }): void {
    const bounds = event.currentTarget.getBoundingClientRect();
    active = bandIndex(event.clientX - bounds.left, left, right - left, groups);
  }

  function step(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      active = undefined;
      return;
    }
    const next = steppedIndex(event.key, active ?? groups - 1, groups);
    if (next !== undefined) {
      event.preventDefault();
      active = next;
    }
  }
</script>

<div
  bind:clientWidth={width}
  bind:clientHeight={height}
  class={['relative touch-pan-y rounded-control select-none', className]}
  role="slider"
  tabindex="0"
  aria-label={label}
  aria-valuemin={0}
  aria-valuemax={Math.max(0, groups - 1)}
  aria-valuenow={Math.max(0, shown)}
  aria-valuetext={valueText}
  onpointermove={follow}
  onpointerdown={follow}
  onpointerleave={(event) => {
    if (event.pointerType === 'mouse') {
      active = undefined;
    }
  }}
  onpointercancel={() => (active = undefined)}
  onfocus={() => (active ??= Math.max(0, groups - 1))}
  onblur={() => (active = undefined)}
  onkeydown={step}
>
  <svg class="absolute inset-0 size-full overflow-visible" {width} {height} aria-hidden="true" focusable="false">
    {#each scale.values as tick (tick)}
      {#if tick <= top.current}
        <line
          x1={left}
          x2={right}
          y1={yAt(tick)}
          y2={yAt(tick)}
          class={tick === 0 ? 'stroke-white/18' : 'stroke-white/7'}
          stroke-dasharray={tick === 0 ? undefined : '2 5'}
        ></line>
        <text x={left - 12} y={yAt(tick)} dy="0.32em" text-anchor="end" class="fill-ink-mute text-micro tabular-nums">
          {formatTick(tick)}
        </text>
      {/if}
    {/each}
    {#each xTicks as { group, index } (group)}
      <text
        x={center(group)}
        y={bottom + 19}
        text-anchor={anchor(center(group))}
        class={['text-micro tabular-nums', group === active ? 'fill-ink' : 'fill-ink-mute']}
      >
        {tickLabel(index)}
      </text>
    {/each}
    {#if active !== undefined && groups > 0}
      <rect x={left + active * band} y={TOP} width={band} height={bottom - TOP} rx="4" class="fill-white/6"></rect>
    {/if}
    {#each Array.from({ length: groups }, (_, i) => i) as group (group)}
      {#each stack(group) as segment (segment.key)}
        {#if segment.height > 0}
          <rect
            x={center(group) - columnWidth / 2}
            y={segment.y}
            width={columnWidth}
            height={segment.height}
            rx={Math.min(2, columnWidth / 2)}
            class={[segment.tone, 'transition-opacity', active !== undefined && active !== group && 'opacity-50']}
          ></rect>
        {/if}
      {/each}
    {/each}
    {#if active !== undefined && groups > 0}
      <ChartTooltip x={center(active)} top={TOP} {bottom} {left} {right} title={title(active)} rows={rows(active)} />
    {/if}
  </svg>
  {#if hint}
    <p class="pointer-events-none absolute inset-x-0 top-[38%] px-6 text-center text-caption text-pretty text-ink-mute">
      {hint}
    </p>
  {/if}
</div>
