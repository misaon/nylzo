<script lang="ts">
  import { TEXT_LINK } from '../../../shared/ui/link/link-classes.ts';
  import type { Overview } from '../../api/types.ts';
  import BarList from '../../charts/BarList.svelte';
  import { share, sum } from '../../charts/geometry/scale.ts';
  import { SECONDS_PER_DAY } from '../../charts/geometry/time.ts';
  import WorldMap from '../../map/WorldMap.svelte';
  import { getDashboardTools } from '../context.ts';
  import { countryName, UNKNOWN_COUNTRY } from '../names.ts';
  import Section from '../Section.svelte';
  import Skeleton from '../Skeleton.svelte';
  import { CARD, NOTE } from '../surfaces.ts';

  interface Props {
    readonly overview: Overview;
    readonly loading: boolean;
  }

  const { overview, loading }: Props = $props();
  const { translate, format } = getDashboardTools();
  let highlighted = $state<string | undefined>();

  const countries = $derived(
    overview.countries
      .filter((country) => country.value > 0)
      .map((country) => ({
        key: country.name,
        label: countryName(translate, format, country.name),
        value: country.value,
      }))
      .toSorted(
        (first, second) =>
          Number(first.key === UNKNOWN_COUNTRY) - Number(second.key === UNKNOWN_COUNTRY) || second.value - first.value,
      ),
  );
  const visitors = $derived(Object.fromEntries(countries.map((country) => [country.key, country.value])));
  const total = $derived(sum(countries.map((country) => country.value)));
  const focused = $derived(countries.find((country) => country.key === highlighted));
  const named = $derived(countries.filter((country) => country.key !== UNKNOWN_COUNTRY).length);
</script>

<Section id="dashboard-where" icon="world" title={translate('dashboard.where.title')}>
  <div class={[CARD, 'grid gap-6 lg:grid-cols-[minmax(0,1fr)_20rem] lg:gap-8']}>
    <div class="flex min-w-0 flex-col justify-center">
      <WorldMap
        label={translate('dashboard.where.map')}
        {visitors}
        {highlighted}
        onhighlight={(country) => (highlighted = country)}
      />
      <p class="mt-4 flex min-h-12 flex-wrap items-baseline gap-x-2 text-ui sm:min-h-6" aria-live="polite">
        {#if focused}
          <span class="font-medium text-ink">{focused.label}</span>
          <span class="text-ink tabular-nums">{format.count(focused.value)}</span>
          <span class="text-ink-mute tabular-nums">· {format.percent(share(focused.value, total))}</span>
        {:else if countries.length > 0}
          <span class="text-ink-mute">{translate('dashboard.where.hint')}</span>
        {/if}
      </p>
    </div>
    <div class="flex min-w-0 flex-col">
      <div class="flex items-baseline justify-between gap-4">
        <h3 class="text-title-sm text-ink">{translate('dashboard.where.list')}</h3>
        <p class="text-caption text-ink-mute tabular-nums">
          {translate('dashboard.where.countries')}
          <span class="text-ink-soft">{loading ? '–' : format.count(named)}</span>
        </p>
      </div>
      {#if loading}
        <Skeleton rows={6} class="mt-4" />
      {:else if countries.length > 0}
        <!-- The country list scrolls, so keyboard users must be able to focus it. -->
        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
        <div
          class="relative -mx-3 mt-2 lg:max-h-[21rem] lg:overflow-y-auto lg:overscroll-contain"
          role="region"
          aria-label={translate('dashboard.where.list')}
          tabindex="0"
        >
          <BarList
            label={translate('dashboard.where.list')}
            items={countries}
            tone="people"
            formatValue={format.count}
            formatShare={format.percent}
            {highlighted}
            onhighlight={(country) => (highlighted = country)}
            compact
          />
        </div>
      {:else}
        <p class="mt-6 text-caption text-ink-mute">{translate('dashboard.where.empty')}</p>
      {/if}
    </div>
    {#if loading}
      <p class={[NOTE, 'lg:col-span-2']} aria-hidden="true">&nbsp;</p>
    {:else if overview.source.attributionUrl}
      <p class={[NOTE, 'lg:col-span-2']}>
        <a href={overview.source.attributionUrl} rel="noreferrer" target="_blank" class={TEXT_LINK}>
          {overview.source.attribution}
        </a>
        · {translate('dashboard.where.source', { dataset: overview.source.countries })}
      </p>
    {/if}
    {#if overview.resolutionSeconds < SECONDS_PER_DAY}
      <p class={[NOTE, 'mt-1']}>{translate('dashboard.where.days')}</p>
    {/if}
  </div>
</Section>
