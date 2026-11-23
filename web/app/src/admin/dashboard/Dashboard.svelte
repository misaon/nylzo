<script lang="ts">
  import Button from '../../shared/ui/button/Button.svelte';
  import Icon from '../../shared/ui/icon/Icon.svelte';
  import type { Live, Overview, Range } from '../api/types.ts';
  import { getAdminTranslator } from '../messages/translator-context.ts';
  import AccessSection from './access/AccessSection.svelte';
  import { setDashboardTools } from './context.ts';
  import { dashboardFormat } from './format.ts';
  import MessagesSection from './messages/MessagesSection.svelte';
  import NowStrip from './now/NowStrip.svelte';
  import { EMPTY_LIVE, emptyOverview } from './overview.ts';
  import ProtectionSection from './protection/ProtectionSection.svelte';
  import RangeSwitch from './RangeSwitch.svelte';
  import ServerSection from './server/ServerSection.svelte';
  import Breakdowns from './traffic/Breakdowns.svelte';
  import TrafficSection from './traffic/TrafficSection.svelte';
  import WhereFrom from './where/WhereFrom.svelte';

  interface Props {
    readonly overview: Overview | undefined;
    readonly live: Live | undefined;
    readonly range: Range;
    readonly failed: boolean;
    readonly stale: boolean;
    readonly onrange: (range: Range) => void;
    readonly onretry: () => void;
  }

  const { overview, live, range, failed, stale, onrange, onretry }: Props = $props();
  const translator = getAdminTranslator();
  const { translate } = translator;
  setDashboardTools({ translate, format: dashboardFormat(translator.language) });

  const now = $derived.by(() => {
    void live;
    void overview;
    return Date.now() / 1000;
  });
  const loading = $derived(overview === undefined);
  const waiting = $derived(live === undefined);
  const data = $derived(overview ?? emptyOverview(range, now));
  const current = $derived(live ?? EMPTY_LIVE);
  const switching = $derived(overview !== undefined && overview.range !== range);
</script>

<div class="grid gap-10 pb-16 md:gap-12">
  <div class="flex flex-wrap items-center justify-between gap-x-6 gap-y-3">
    <h1
      class="font-display text-page font-light text-ink outline-none md:text-page-lg"
      tabindex="-1"
      {@attach (heading) => heading.focus({ preventScroll: true })}
    >
      {translate('dashboard.title')}
    </h1>
    <div role="status">
      {#if failed}
        <div
          class="fixed bottom-4 left-1/2 z-50 flex w-max max-w-[calc(100vw-2rem)] -translate-x-1/2 items-center gap-3 rounded-card bg-void-950 py-2 pr-2 pl-3 text-caption text-ink-soft ring-1 ring-solar-300/40"
        >
          <Icon name="caution" class="size-4 shrink-0 text-solar-300" />
          <p>{translate(loading ? 'dashboard.failed.first' : 'dashboard.failed')}</p>
          <Button variant="tint" size="compact" class="shrink-0" onclick={onretry}>
            <Icon name="refresh" />{translate('access.retry')}
          </Button>
        </div>
      {:else if loading}
        <p class="sr-only">{translate('dashboard.loading')}</p>
      {/if}
    </div>
  </div>

  <NowStrip live={current} overview={data} {waiting} {loading} {stale} />

  <div class="grid gap-10 md:gap-12">
    <div class="flex flex-wrap items-end justify-between gap-x-6 gap-y-4">
      <div>
        <p class="text-eyebrow text-aegis-300 uppercase">{translate('dashboard.range.label')}</p>
        <h2 class="mt-2 font-display text-panel font-light text-ink md:text-panel-lg">
          {translate(`dashboard.range.long.${data.range}`)}
        </h2>
        <p class="mt-1.5 flex items-center gap-2 text-caption text-ink-mute">
          {#if switching}
            <Icon name="refresh" class="size-3.5 shrink-0 motion-safe:animate-spin" />
            {translate('dashboard.loading')}
          {:else}
            <Icon name="clock" class="size-3.5 shrink-0" />
            {translate('dashboard.time')}
          {/if}
        </p>
      </div>
      <RangeSwitch {range} {onrange} />
    </div>

    <div class="grid gap-10 md:gap-12" aria-busy={switching || loading}>
      <TrafficSection overview={data} range={data.range} {loading} />
      <WhereFrom overview={data} {loading} />
      <Breakdowns overview={data} {loading} />
      <MessagesSection overview={data} range={data.range} {loading} />
      <ServerSection overview={data} live={current} {loading} {waiting} />
      <ProtectionSection overview={data} live={current} range={data.range} {loading} {waiting} {now} />
      <AccessSection overview={data} sessionExpiresAt={current.sessionExpiresAt} {loading} {now} />
    </div>
  </div>
</div>
