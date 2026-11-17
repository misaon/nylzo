<script lang="ts">
  import type { Overview, Range } from '../../api/types.ts';
  import ColumnChart from '../../charts/ColumnChart.svelte';
  import { periodNumbers, timeTicks } from '../../charts/geometry/time.ts';
  import { SWATCHES } from '../../charts/tones.ts';
  import { getDashboardTools } from '../context.ts';
  import { timelineOf, trend } from '../overview.ts';
  import Section from '../Section.svelte';
  import Stat from '../Stat.svelte';
  import { CARD, NOTE } from '../surfaces.ts';

  interface Props {
    readonly overview: Overview;
    readonly range: Range;
    readonly loading: boolean;
  }

  const TICK_GAP = 64;
  const EVENTS = [
    { metric: 'messages.created', key: 'created', tone: 'people' },
    { metric: 'messages.opened', key: 'opened', tone: 'opened' },
    { metric: 'messages.ended', key: 'ended', tone: 'ended' },
    { metric: 'messages.expired', key: 'expired', tone: 'quiet' },
  ] as const;

  const { overview, range, loading }: Props = $props();
  const { translate, format } = getDashboardTools();
  const timeline = $derived(timelineOf(overview));
  const periods = $derived(periodNumbers(timeline));
  const quiet = $derived(!loading && EVENTS.every((event) => overview.totals[event.metric] === 0));
  const series = $derived(
    EVENTS.map((event) => ({
      label: translate(`dashboard.messages.${event.key}`),
      values: overview.series[event.metric],
      tone: event.tone,
    })),
  );
</script>

<Section id="dashboard-messages" icon="envelope" title={translate('dashboard.messages.title')}>
  <dl class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
    {#each EVENTS as event (event.metric)}
      <Stat
        label={translate(`dashboard.messages.${event.key}`)}
        value={overview.totals[event.metric]}
        change={trend(overview.totals[event.metric], overview.previous?.[event.metric])}
        tone={event.tone}
        growthIsGood={event.key !== 'expired'}
        {range}
        {loading}
      />
    {/each}
    <Stat
      class="max-sm:col-span-2"
      label={translate('dashboard.messages.stored')}
      value={overview.stored}
      {range}
      {loading}
    />
  </dl>
  <div class={CARD}>
    <div class="flex flex-wrap items-center justify-between gap-x-6 gap-y-2">
      <h3 class="text-title-sm text-ink">{translate('dashboard.messages.chart')}</h3>
      <ul role="list" class="flex flex-wrap items-center gap-x-4 gap-y-1 text-caption text-ink-soft">
        {#each series as entry (entry.label)}
          <li class="flex items-center gap-2">
            <span class={['size-2 rounded-xs', SWATCHES[entry.tone]]} aria-hidden="true"></span>
            {entry.label}
          </li>
        {/each}
      </ul>
    </div>
    <ColumnChart
      class="mt-4 h-52 sm:h-60"
      label={translate('dashboard.messages.chart')}
      {series}
      keys={periods}
      hint={quiet ? translate('dashboard.empty') : undefined}
      formatValue={format.count}
      formatTick={format.compact}
      periodLabel={(first, last) => format.period(timeline, first, last)}
      tickLabel={(index) => format.tick(timeline, index)}
      tickIndexes={(width) => timeTicks(timeline, width, TICK_GAP)}
    />
    <p class={[NOTE, 'mt-4']}>{translate('dashboard.messages.note')}</p>
  </div>
</Section>
