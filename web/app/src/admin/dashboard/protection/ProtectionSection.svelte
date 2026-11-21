<script lang="ts">
  import type { Live, Overview, Range } from '../../api/types.ts';
  import ColumnChart from '../../charts/ColumnChart.svelte';
  import { timeTicks } from '../../charts/geometry/time.ts';
  import { getDashboardTools } from '../context.ts';
  import { timelineOf, trend } from '../overview.ts';
  import Section from '../Section.svelte';
  import Stat from '../Stat.svelte';
  import { CARD } from '../surfaces.ts';
  import RefusedClients from './RefusedClients.svelte';

  interface Props {
    readonly overview: Overview;
    readonly live: Live;
    readonly range: Range;
    readonly loading: boolean;
    readonly waiting: boolean;
    readonly now: number;
  }

  const TICK_GAP = 64;

  const { overview, live, range, loading, waiting, now }: Props = $props();
  const { translate, format } = getDashboardTools();
  const timeline = $derived(timelineOf(overview));
  const refusals = $derived(overview.totals.refusals);
</script>

<Section id="dashboard-protection" icon="shield" title={translate('dashboard.protection.title')}>
  <div class={[CARD, 'grid gap-5 lg:grid-cols-[16rem_minmax(0,1fr)] lg:gap-6']}>
    <dl class="grid content-start">
      <Stat
        label={translate('dashboard.protection.refusals')}
        note={translate('dashboard.protection.refusals.note')}
        value={refusals}
        change={trend({ current: refusals, previous: overview.previous?.refusals })}
        tone="danger"
        growthIsGood={false}
        {range}
        {loading}
      />
    </dl>
    <ColumnChart
      class="h-44"
      label={translate('dashboard.protection.chart')}
      series={[{ label: translate('dashboard.protection.refusals'), values: overview.series.refusals, tone: 'danger' }]}
      formatValue={format.count}
      formatTick={format.compact}
      periodLabel={(first, last) => format.period(timeline, first, last)}
      tickLabel={(index) => format.tick(timeline, index)}
      tickIndexes={(width) => timeTicks(timeline, { width, gap: TICK_GAP })}
    />
  </div>
  <RefusedClients clients={live.refused} {waiting} {now} />
</Section>
