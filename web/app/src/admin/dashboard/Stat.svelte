<script lang="ts">
  import type { ClassValue } from 'svelte/elements';

  import type { Range } from '../api/types.ts';
  import AnimatedNumber from '../charts/AnimatedNumber.svelte';
  import { SWATCHES } from '../charts/tones.ts';
  import type { Tone } from '../charts/tones.ts';
  import { getDashboardTools } from './context.ts';
  import { NOTE, TILE } from './surfaces.ts';
  import Trend from './Trend.svelte';

  interface Props {
    readonly label: string;
    readonly value: number;
    readonly loading: boolean;
    readonly range: Range;
    readonly change?: number | undefined;
    readonly note?: string | undefined;
    readonly tone?: Tone | undefined;
    readonly growthIsGood?: boolean;
    readonly class?: ClassValue;
  }

  const { label, value, loading, range, change, note, tone, growthIsGood = true, class: className }: Props = $props();
  const { format } = getDashboardTools();
</script>

<div class={[TILE, 'flex min-w-0 flex-col px-4 py-3.5', className]}>
  <dt class="flex items-center gap-2 text-caption text-ink-soft">
    {#if tone}
      <span class={['size-2 shrink-0 rounded-full', SWATCHES[tone]]} aria-hidden="true"></span>
    {/if}
    {label}
  </dt>
  <dd class="mt-1 flex flex-1 flex-col">
    <span class="text-stat font-light text-ink tabular-nums">
      {#if loading}
        <span class="inline-block h-6 w-16 animate-pulse rounded-xs bg-white/8 align-middle"></span>
      {:else}
        <AnimatedNumber {value} format={format.count} />
      {/if}
    </span>
    <span class="mt-0.5 block min-h-5">
      {#if !loading}
        <Trend {change} {range} {growthIsGood} />
      {/if}
    </span>
    {#if note}
      <span class={[NOTE, 'mt-2 block']}>{note}</span>
    {/if}
  </dd>
</div>
