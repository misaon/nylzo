<script lang="ts">
  import type { ClassValue } from 'svelte/elements';

  import { smoothArea, smoothLine } from './geometry/path.ts';
  import { tweenedNumber, tweenedSeries } from './motion.svelte.ts';
  import { STOPS, STROKES } from './tones.ts';
  import type { Tone } from './tones.ts';

  interface Props {
    readonly values: readonly number[];
    readonly keys?: readonly number[] | undefined;
    readonly tone: Tone;
    readonly overlay?: readonly number[] | undefined;
    readonly maximum?: number | undefined;
    readonly class?: ClassValue;
  }

  const WIDTH = 100;
  const HEIGHT = 40;
  const INSET = 3;

  const { values, keys, tone, overlay, maximum, class: className }: Props = $props();
  const id = $props.id();
  const top = tweenedNumber(() => maximum ?? Math.max(1, ...values, ...(overlay ?? [])), 700);
  const motion = { keys: () => keys, onlyLatestChanges: true, milliseconds: 700 };
  const main = tweenedSeries(() => values, motion);
  const second = tweenedSeries(() => overlay ?? [], motion);

  function points(series: readonly number[]): { x: number; y: number }[] {
    const last = Math.max(1, series.length - 1);
    return series.map((value, i) => ({
      x: (i / last) * WIDTH,
      y: HEIGHT - INSET - (Math.max(0, value) / top.current) * (HEIGHT - 2 * INSET),
    }));
  }

  const mainPoints = $derived(points(main.current));
</script>

<svg
  class={['block w-full overflow-visible', className]}
  viewBox="0 0 {WIDTH} {HEIGHT}"
  preserveAspectRatio="none"
  aria-hidden="true"
  focusable="false"
>
  <defs>
    <linearGradient id="{id}-fill" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" class={STOPS[tone]} stop-opacity="0.38"></stop>
      <stop offset="1" class={STOPS[tone]} stop-opacity="0"></stop>
    </linearGradient>
  </defs>
  <line
    x1="0"
    x2={WIDTH}
    y1={HEIGHT - INSET}
    y2={HEIGHT - INSET}
    class="stroke-white/12"
    vector-effect="non-scaling-stroke"
  ></line>
  <path d={smoothArea(mainPoints, HEIGHT - INSET)} fill="url(#{id}-fill)"></path>
  {#if overlay}
    <path
      d={smoothLine(points(second.current))}
      class={STROKES.robots}
      fill="none"
      stroke-width="1.5"
      stroke-dasharray="3 3"
      vector-effect="non-scaling-stroke"
    ></path>
  {/if}
  <path
    d={smoothLine(mainPoints)}
    class={STROKES[tone]}
    fill="none"
    stroke-width="1.75"
    stroke-linejoin="round"
    vector-effect="non-scaling-stroke"
  ></path>
</svg>
