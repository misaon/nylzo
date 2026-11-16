<script lang="ts">
  import type { Overview, Range } from '../../api/types.ts';
  import { SECONDS_PER_DAY, timeTicks } from '../../charts/geometry/time.ts';
  import ShareBar from '../../charts/ShareBar.svelte';
  import TimeChart from '../../charts/TimeChart.svelte';
  import { SWATCHES } from '../../charts/tones.ts';
  import { getDashboardTools } from '../context.ts';
  import { timelineOf, trend } from '../overview.ts';
  import Section from '../Section.svelte';
  import Stat from '../Stat.svelte';
  import { CARD } from '../surfaces.ts';

  interface Props {
    readonly overview: Overview;
    readonly range: Range;
    readonly loading: boolean;
  }

  const TICK_GAP = 64;

  const { overview, range, loading }: Props = $props();
  const { translate, format } = getDashboardTools();
  const timeline = $derived(timelineOf(overview));
  const totals = $derived(overview.totals);

  // Visitors are counted by the day only, so the hours of a short range have no line of them, and its days are
  // too few and too uneven to compare with the days before.
  const daily = $derived(overview.resolutionSeconds === SECONDS_PER_DAY);
  const quiet = $derived(!loading && totals['views.people'] === 0 && totals.visitors === 0);

  const pending = () => '–';

  const STATS = [
    { metric: 'views.people', key: 'people', tone: 'people', growthIsGood: true },
    { metric: 'visitors', key: 'visitors', tone: 'visitors', growthIsGood: true },
    { metric: 'views.robots', key: 'robots', tone: 'robots', growthIsGood: false },
    { metric: 'views.unconfirmed', key: 'unconfirmed', tone: 'quiet', growthIsGood: false },
  ] as const;
</script>

<Section id="dashboard-traffic" icon="eye" title={translate('dashboard.traffic.title')}>
  <dl class="grid grid-cols-2 gap-3 lg:grid-cols-4">
    {#each STATS as stat (stat.metric)}
      <Stat
        label={translate(`dashboard.traffic.${stat.key}`)}
        note={stat.metric === 'visitors' && !daily
          ? translate('dashboard.traffic.visitors.days')
          : translate(`dashboard.traffic.${stat.key}.note`)}
        value={totals[stat.metric]}
        change={stat.metric === 'visitors' && !daily
          ? undefined
          : trend(totals[stat.metric], overview.previous?.[stat.metric])}
        tone={stat.tone}
        growthIsGood={stat.growthIsGood}
        {range}
        {loading}
      />
    {/each}
  </dl>

  <div class={CARD}>
    <div class="flex flex-wrap items-center justify-between gap-x-6 gap-y-2">
      <h3 class="text-title-sm text-ink">{translate('dashboard.traffic.chart')}</h3>
      <ul role="list" class="flex items-center gap-4 text-caption text-ink-soft">
        <li class="flex items-center gap-2">
          <span class={['h-1 w-4 rounded-full', SWATCHES.people]} aria-hidden="true"></span>
          {translate('dashboard.traffic.people')}
        </li>
        {#if daily}
          <li class="flex items-center gap-2">
            <span class="h-0 w-4 border-t-[1.5px] border-dashed border-aegis-200" aria-hidden="true"></span>
            {translate('dashboard.traffic.visitors')}
          </li>
        {/if}
      </ul>
    </div>
    <TimeChart
      class="mt-4 h-56 sm:h-72"
      label={translate('dashboard.traffic.chart')}
      area={{ label: translate('dashboard.traffic.people'), values: overview.series['views.people'], tone: 'people' }}
      line={daily
        ? { label: translate('dashboard.traffic.visitors'), values: overview.series.visitors, tone: 'visitors' }
        : undefined}
      hint={quiet ? translate('dashboard.empty') : undefined}
      formatValue={format.count}
      formatTick={format.compact}
      periodLabel={(index) => format.period(timeline, index)}
      tickLabel={(index) => format.tick(timeline, index)}
      tickIndexes={(width) => timeTicks(timeline, width, TICK_GAP)}
    />
    <div class="mt-6 border-t border-white/8 pt-5">
      <h3 class="mb-3 text-caption text-ink-soft">{translate('dashboard.traffic.share')}</h3>
      <ShareBar
        label={translate('dashboard.traffic.share')}
        parts={[
          { label: translate('dashboard.traffic.people'), tone: 'people' },
          { label: translate('dashboard.traffic.robots'), tone: 'robots' },
          { label: translate('dashboard.traffic.unconfirmed'), tone: 'quiet' },
        ]}
        values={[totals['views.people'], totals['views.robots'], totals['views.unconfirmed']]}
        formatValue={loading ? pending : format.count}
        formatShare={loading ? pending : format.percent}
      />
    </div>
  </div>
</Section>
