<script lang="ts">
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import type { UsagePoint } from '../../api/types.ts';
  import TimeChart from '../../charts/TimeChart.svelte';
  import type { Tone } from '../../charts/tones.ts';
  import type { AdminMessageKey } from '../../messages/catalog.ts';
  import { getDashboardTools } from '../context.ts';
  import { CARD } from '../surfaces.ts';
  import { minuteTicks, sampleTime } from './usage.ts';
  import type { UsageSeries } from './usage.ts';

  interface Props {
    readonly title: string;
    readonly label: string;
    readonly usage: readonly UsagePoint[];
    readonly series: UsageSeries;
    readonly tone: Tone;
    readonly current: string;
    readonly detail: string;
  }

  const TICK_GAP = 56;
  const PERCENT_TICKS = { max: 1, values: [0, 0.5, 1] } as const;
  const MISSING = {
    waiting: 'dashboard.now.waiting',
    unavailable: 'dashboard.server.unavailable',
  } as const satisfies Record<Exclude<UsageSeries['kind'], 'measured'>, AdminMessageKey>;

  const { title, label, usage, series, tone, current, detail }: Props = $props();
  const { translate, format } = getDashboardTools();
  const times = $derived(usage.map((point) => point.at));
</script>

<div class={[CARD, 'flex min-w-0 flex-col']}>
  <div class="flex items-baseline justify-between gap-4">
    <h3 class="text-title-sm text-ink">{title}</h3>
    <p class="text-title font-medium text-ink tabular-nums">{current}</p>
  </div>
  <p class="mt-0.5 min-h-5 text-caption text-ink-mute">{detail}</p>
  {#if series.kind !== 'measured'}
    <div class="mt-4 grid h-44 place-items-center rounded-pane bg-white/3 ring-1 ring-white/6">
      <p class="flex items-center gap-2 px-6 text-center text-caption text-ink-mute">
        <Icon name="info" class="size-4 shrink-0" />
        <span>{translate(MISSING[series.kind])}</span>
      </p>
    </div>
  {:else}
    <TimeChart
      class="mt-4 h-44"
      {label}
      area={{ label: title, values: series.values, tone }}
      keys={times}
      ticks={PERCENT_TICKS}
      formatValue={format.percent}
      formatTick={format.percent}
      periodLabel={(index) => format.time(sampleTime(usage, index))}
      tickLabel={(index) => format.clock(sampleTime(usage, index))}
      tickIndexes={(width) => minuteTicks(usage, { width, gap: TICK_GAP })}
    />
  {/if}
</div>
