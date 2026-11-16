<script lang="ts">
  import type { Overview } from '../../api/types.ts';
  import { getDashboardTools } from '../context.ts';
  import Section from '../Section.svelte';
  import { NOTE, TILE } from '../surfaces.ts';

  interface Props {
    readonly overview: Overview;
    readonly sessionExpiresAt: number;
    readonly loading: boolean;
    readonly now: number;
  }

  const COMMANDS = [
    { key: 'add', command: 'nylzo admin-enroll' },
    { key: 'reset', command: 'nylzo admin-reset' },
  ] as const;

  const { overview, sessionExpiresAt, loading, now }: Props = $props();
  const { translate, format } = getDashboardTools();
  const security = $derived(overview.security);
  const ends = $derived(sessionExpiresAt || security.sessionExpiresAt);
</script>

<Section id="dashboard-access" icon="key" title={translate('dashboard.access.title')}>
  <div class="grid gap-3 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
    <dl class="grid gap-3 sm:grid-cols-3">
      <div class={[TILE, 'px-4 py-3.5']}>
        <dt class="text-caption text-ink-soft">{translate('dashboard.access.passkeys')}</dt>
        <dd class="mt-1 text-title font-medium text-ink tabular-nums">
          {loading ? '–' : format.count(security.passkeys)}
        </dd>
      </div>
      <div class={[TILE, 'px-4 py-3.5']}>
        <dt class="text-caption text-ink-soft">{translate('dashboard.access.last')}</dt>
        <dd class="mt-1 text-title font-medium text-ink">
          {#if loading}
            –
          {:else if security.lastSignIn > 0}
            <time
              datetime={new Date(security.lastSignIn * 1000).toISOString()}
              title={format.moment(security.lastSignIn)}
            >
              {format.relative(Math.min(security.lastSignIn, now), now)}
            </time>
          {:else}
            {translate('dashboard.access.never')}
          {/if}
        </dd>
      </div>
      <div class={[TILE, 'px-4 py-3.5']}>
        <dt class="text-caption text-ink-soft">{translate('dashboard.access.session')}</dt>
        <dd class="mt-1 text-title font-medium text-ink">
          {#if ends > 0}
            <time datetime={new Date(ends * 1000).toISOString()} title={format.moment(ends)}>
              {format.relative(ends, now)}
            </time>
          {:else}
            –
          {/if}
          <span class={[NOTE, 'mt-1 block font-normal']}>{translate('dashboard.access.session.note')}</span>
        </dd>
      </div>
    </dl>
    <div class={[TILE, 'grid gap-2.5 px-4 py-3.5']}>
      {#each COMMANDS as entry (entry.key)}
        <div class="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
          <span class="text-caption text-ink-soft">{translate(`dashboard.access.${entry.key}`)}</span>
          <span class="rounded-xs px-2 py-0.5 text-caption text-aegis-200 well" translate="no">{entry.command}</span>
        </div>
      {/each}
      <p class={NOTE}>{translate('dashboard.access.command')}</p>
    </div>
  </div>
</Section>
