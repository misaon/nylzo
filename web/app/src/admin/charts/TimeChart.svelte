<script lang="ts">
  import type { ClassValue } from 'svelte/elements';

  import ChartTooltip from './ChartTooltip.svelte';
  import { smoothArea, smoothLine } from './geometry/path.ts';
  import { nearestIndex, steppedIndex } from './geometry/pointer.ts';
  import { niceTicks } from './geometry/scale.ts';
  import type { Ticks } from './geometry/scale.ts';
  import { tweenedNumber, tweenedSeries } from './motion.svelte.ts';
  import { FILLS, STOPS, STROKES } from './tones.ts';
  import type { ChartSeries, TooltipRow } from './tones.ts';

  interface Props {
    readonly label: string;
    readonly area: ChartSeries;
    readonly line?: ChartSeries | undefined;
    readonly keys?: readonly number[] | undefined;
    readonly ticks?: Ticks | undefined;
    readonly class?: ClassValue;
    readonly hint?: string | undefined;
    readonly formatValue: (value: number) => string;
    readonly formatTick: (value: number) => string;
    readonly periodLabel: (index: number) => string;
    readonly tickLabel: (index: number) => string;
    readonly tickIndexes: (width: number) => readonly number[];
  }

  const TOP = 10;
  const RIGHT = 6;
  const BOTTOM = 26;
  const LEFT = 46;
  const LABEL_EDGE = 28;

  const {
    label,
    area,
    line,
    keys,
    ticks,
    class: className,
    hint,
    formatValue,
    formatTick,
    periodLabel,
    tickLabel,
    tickIndexes,
  }: Props = $props();
  const id = $props.id();

  let width = $state(0);
  let height = $state(0);
  let active = $state<number | undefined>();

  const count = $derived(area.values.length);
  const scale = $derived(ticks ?? niceTicks(Math.max(0, ...area.values, ...(line?.values ?? []))));
  const top = tweenedNumber(() => scale.max);
  const areaValues = tweenedSeries(() => area.values, { keys: () => keys });
  const lineValues = tweenedSeries(() => line?.values ?? [], { keys: () => keys });
  const left = LEFT;
  const right = $derived(Math.max(LEFT + 1, width - RIGHT));
  const bottom = $derived(Math.max(TOP + 1, height - BOTTOM));
  const xTicks = $derived(tickIndexes(right - left));
  const shown = $derived(Math.min(active ?? count - 1, count - 1));

  function xAt(index: number): number {
    return count <= 1 ? (left + right) / 2 : left + (index / (count - 1)) * (right - left);
  }

  function yAt(value: number): number {
    return bottom - (Math.max(0, value) / Math.max(1, top.current)) * (bottom - TOP);
  }

  function anchor(x: number): 'start' | 'middle' | 'end' {
    if (x - left < LABEL_EDGE) {
      return 'start';
    }
    return right - x < LABEL_EDGE ? 'end' : 'middle';
  }

  const areaPoints = $derived(areaValues.current.map((value, i) => ({ x: xAt(i), y: yAt(value) })));
  const linePoints = $derived(lineValues.current.map((value, i) => ({ x: xAt(i), y: yAt(value) })));
  const areaLine = $derived(smoothLine(areaPoints));

  function rows(index: number): TooltipRow[] {
    const list = [{ label: area.label, value: formatValue(area.values[index] ?? 0), tone: area.tone }];
    if (line) {
      list.push({ label: line.label, value: formatValue(line.values[index] ?? 0), tone: line.tone });
    }
    return list;
  }

  const valueText = $derived(
    count === 0
      ? label
      : `${periodLabel(shown)}: ${rows(shown)
          .map((row) => `${row.label} ${row.value}`)
          .join(', ')}`,
  );

  function follow(event: PointerEvent & { readonly currentTarget: HTMLDivElement }): void {
    const bounds = event.currentTarget.getBoundingClientRect();
    active = nearestIndex(event.clientX - bounds.left, left, right - left, count);
  }

  function step(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      active = undefined;
      return;
    }
    const next = steppedIndex(event.key, active ?? count - 1, count);
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
  aria-valuemax={Math.max(0, count - 1)}
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
  onfocus={() => (active ??= Math.max(0, count - 1))}
  onblur={() => (active = undefined)}
  onkeydown={step}
>
  <svg class="absolute inset-0 size-full overflow-visible" {width} {height} aria-hidden="true" focusable="false">
    <defs>
      <linearGradient id="{id}-fill" x1="0" y1="0" x2="0" y2="1">
        <stop offset="0" class={STOPS[area.tone]} stop-opacity="0.5"></stop>
        <stop offset="0.7" class={STOPS[area.tone]} stop-opacity="0.08"></stop>
        <stop offset="1" class={STOPS[area.tone]} stop-opacity="0"></stop>
      </linearGradient>
      <filter id="{id}-glow" x="-5%" y="-40%" width="110%" height="180%">
        <feGaussianBlur stdDeviation="5"></feGaussianBlur>
      </filter>
    </defs>
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
    {#each xTicks as index (index)}
      <text
        x={xAt(index)}
        y={bottom + 19}
        text-anchor={anchor(xAt(index))}
        class={['text-micro tabular-nums', index === active ? 'fill-ink' : 'fill-ink-mute']}
      >
        {tickLabel(index)}
      </text>
    {/each}
    <path d={smoothArea(areaPoints, bottom)} fill="url(#{id}-fill)"></path>
    <path d={areaLine} class={STROKES[area.tone]} fill="none" stroke-width="6" opacity="0.55" filter="url(#{id}-glow)"
    ></path>
    <path
      d={areaLine}
      class={STROKES[area.tone]}
      fill="none"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
    ></path>
    {#if line}
      <path
        d={smoothLine(linePoints)}
        class={STROKES[line.tone]}
        fill="none"
        stroke-width="1.5"
        stroke-dasharray="5 4"
        stroke-linecap="round"
        opacity="0.9"
      ></path>
    {/if}
    {#if active !== undefined && count > 0}
      <line x1={xAt(active)} x2={xAt(active)} y1={TOP} y2={bottom} class="stroke-white/30" stroke-width="1"></line>
      {#if line}
        <circle
          cx={xAt(active)}
          cy={yAt(line.values[active] ?? 0)}
          r="3.5"
          class={['stroke-void-950', FILLS[line.tone]]}
          stroke-width="2"
        ></circle>
      {/if}
      <circle cx={xAt(active)} cy={yAt(area.values[active] ?? 0)} r="9" class={FILLS[area.tone]} opacity="0.25"
      ></circle>
      <circle
        cx={xAt(active)}
        cy={yAt(area.values[active] ?? 0)}
        r="4"
        class={['stroke-white', FILLS[area.tone]]}
        stroke-width="1.5"
      ></circle>
      <ChartTooltip x={xAt(active)} top={TOP} {bottom} {left} {right} title={periodLabel(active)} rows={rows(active)} />
    {/if}
  </svg>
  {#if hint}
    <p class="pointer-events-none absolute inset-x-0 top-[38%] px-6 text-center text-caption text-pretty text-ink-mute">
      {hint}
    </p>
  {/if}
</div>
