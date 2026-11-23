<script lang="ts">
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import type { Live, Overview } from '../../api/types.ts';
  import AnimatedNumber from '../../charts/AnimatedNumber.svelte';
  import { sum } from '../../charts/geometry/scale.ts';
  import Sparkline from '../../charts/Sparkline.svelte';
  import { SWATCHES } from '../../charts/tones.ts';
  import { getDashboardTools } from '../context.ts';
  import { CARD, NOTE, TILE } from '../surfaces.ts';
  import UsageTile from './UsageTile.svelte';

  interface Props {
    readonly live: Live;
    readonly overview: Overview;
    readonly waiting: boolean;
    readonly loading: boolean;
    readonly stale: boolean;
  }

  const { live, overview, waiting, loading, stale }: Props = $props();
  const { translate, format } = getDashboardTools();

  const latest = $derived(live.usage.at(-1));
  const processor = $derived(live.usageSupported && latest && latest.processor >= 0 ? latest.processor : undefined);
  const memory = $derived(
    live.usageSupported && latest && latest.memoryTotal > 0 ? latest.memoryUsed / latest.memoryTotal : undefined,
  );
  const usageTimes = $derived(live.usage.map((point) => point.at));
  const processorHistory = $derived(live.usage.map((point) => Math.max(0, point.processor)));
  const memoryHistory = $derived(
    live.usage.map((point) => (point.memoryTotal > 0 ? point.memoryUsed / point.memoryTotal : 0)),
  );
  const storage = $derived(overview.storage);
  const diskKnown = $derived(storage.totalBytes > 0);

  function missing(): string {
    if (!live.usageSupported) {
      return translate('dashboard.now.unavailable');
    }
    return translate('dashboard.now.waiting');
  }
</script>

<section aria-labelledby="dashboard-now" class={[CARD, 'grid gap-5 lg:grid-cols-12 lg:gap-6']}>
  <div class="flex min-w-0 flex-col lg:col-span-5">
    <div class="flex items-center justify-between gap-4">
      <h2 id="dashboard-now" class="text-title text-ink">{translate('dashboard.now.title')}</h2>
      {#if stale}
        <span
          class="inline-flex items-center gap-2 rounded-full bg-white/6 py-1 pr-3 pl-2.5 text-micro font-medium text-ink-soft ring-1 ring-white/12"
        >
          <span class="size-2 rounded-full bg-solar-300" aria-hidden="true"></span>
          {latest
            ? translate('dashboard.now.stale', { time: format.clockAt(latest.at) })
            : translate('dashboard.now.paused')}
        </span>
      {:else}
        <span
          class="inline-flex items-center gap-2 rounded-full bg-aegis-500/12 py-1 pr-3 pl-2.5 text-micro font-medium text-aegis-200 ring-1 ring-aegis-400/30"
        >
          <span class="relative grid size-2 place-items-center" aria-hidden="true">
            <span class="absolute size-4 rounded-full bg-aegis-400/40 motion-safe:animate-halo"></span>
            <span class="relative size-2 rounded-full bg-aegis-300 shadow-glow"></span>
          </span>
          <span>{translate('dashboard.now.live')}</span>
        </span>
      {/if}
    </div>
    <p class="mt-5 mb-2 text-caption text-ink-soft">{translate('dashboard.now.online')}</p>
    <p
      class="font-display text-[4rem] leading-none font-light text-ink tabular-nums drop-shadow-[0_0_28px_oklch(0.56_0.19_258/0.75)] sm:text-[4.5rem]"
    >
      {#if waiting}
        <span class="inline-block h-12 w-20 animate-pulse rounded-control bg-white/8 align-middle"></span>
      {:else}
        <AnimatedNumber value={live.online} format={format.count} />
      {/if}
    </p>
    <p class={[NOTE, 'mt-2']}>{translate('dashboard.now.online.note')}</p>
    <div class="mt-auto pt-6">
      <Sparkline values={live.people} tone="people" overlay={live.robots} class="h-16" />
      <div class="mt-2.5 flex flex-wrap items-center gap-x-4 gap-y-1 text-micro text-ink-mute">
        <span class="flex items-center gap-1.5">
          <span class={['size-2 rounded-full', SWATCHES.people]} aria-hidden="true"></span>
          {translate('dashboard.now.people')}
          <span class="text-ink-soft tabular-nums">{waiting ? '–' : format.count(sum(live.people))}</span>
        </span>
        <span class="flex items-center gap-1.5">
          <span class={['size-2 rounded-full', SWATCHES.robots]} aria-hidden="true"></span>
          {translate('dashboard.now.robots')}
          <span class="text-ink-soft tabular-nums">{waiting ? '–' : format.count(sum(live.robots))}</span>
        </span>
        <span class="ml-auto">{translate('dashboard.now.minutes')}</span>
      </div>
    </div>
  </div>

  <div class="grid min-w-0 gap-3 sm:grid-cols-2 lg:col-span-7">
    <UsageTile
      icon="gauge"
      label={translate('dashboard.now.processor')}
      value={processor === undefined ? '–' : format.percent(processor)}
      detail={processor === undefined ? missing() : translate('dashboard.now.five')}
      share={processor}
      tone="people"
      history={processorHistory}
      keys={usageTimes}
    />
    <UsageTile
      icon="rack"
      label={translate('dashboard.now.memory')}
      value={memory === undefined ? '–' : format.percent(memory)}
      detail={memory === undefined || !latest
        ? missing()
        : translate('dashboard.now.memory.value', {
            used: format.bytes(latest.memoryUsed),
            total: format.bytes(latest.memoryTotal),
          })}
      share={memory}
      tone="robots"
      history={memoryHistory}
      keys={usageTimes}
    />
    <UsageTile
      icon="storage"
      label={translate('dashboard.now.disk')}
      value={diskKnown ? format.bytes(storage.freeBytes) : '–'}
      detail={diskKnown
        ? `${translate('dashboard.now.disk.value', { total: format.bytes(storage.totalBytes) })}. ${translate('dashboard.now.disk.reserve', { reserve: format.bytes(storage.reserveBytes) })}`
        : missing()}
      share={diskKnown ? 1 - storage.freeBytes / storage.totalBytes : undefined}
      tone="people"
      reserve={diskKnown ? storage.reserveBytes / storage.totalBytes : undefined}
      tall
    />
    <div class={[TILE, 'flex min-w-0 flex-col gap-1 p-4']}>
      <p class="flex items-center gap-2 text-caption text-ink-soft">
        <Icon name="envelope" class="size-4 shrink-0 text-aegis-300" />
        {translate('dashboard.now.stored')}
      </p>
      <p class="mt-1 text-stat font-light text-ink tabular-nums">
        {#if loading}
          <span class="inline-block h-6 w-16 animate-pulse rounded-xs bg-white/8 align-middle"></span>
        {:else}
          <AnimatedNumber value={overview.stored} format={format.count} />
        {/if}
      </p>
      <p class={[NOTE, 'mt-auto']}>{translate('dashboard.now.stored.note')}</p>
    </div>
  </div>
</section>
