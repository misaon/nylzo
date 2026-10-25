<script lang="ts">
  import { getI18n } from '../../../shared/i18n/i18n.svelte.ts';
  import Icon from '../../../shared/ui/Icon.svelte';

  const { t } = getI18n();

  const limits = [
    { key: 'limits.save', icon: 'camera' },
    { key: 'limits.link', icon: 'link' },
    { key: 'limits.server', icon: 'shield' },
    { key: 'limits.abuse', icon: 'ban' },
  ] as const;
</script>

{#snippet device(icon: 'laptop' | 'phone', label: string)}
  <div class="flex w-[84px] flex-col items-center text-center">
    <span
      class="grid size-14 place-items-center rounded-control bg-nebula-400/10 text-nebula-300 ring-1 ring-nebula-300/50 outline-1 outline-offset-[3px] outline-nebula-300/40 outline-dashed"
    >
      <Icon name={icon} stroke={1.6} class="size-6" />
    </span>
    <span class="mt-3 text-[13px] text-ink">{label}</span>
    <span class="text-[11px] font-medium tracking-[0.08em] whitespace-nowrap text-nebula-300 uppercase"
      >{t('limits.outside')}</span
    >
  </div>
{/snippet}

{#snippet wire(direction: 'out' | 'in')}
  <div
    class={[
      'relative mt-7 h-px bg-linear-to-r',
      direction === 'out'
        ? 'from-nebula-300/40 via-aegis-400/80 to-aegis-400/80'
        : 'from-aegis-400/80 via-aegis-400/80 to-nebula-300/40',
    ]}
  >
    <span
      class="absolute top-1/2 left-1/2 grid size-6 -translate-x-1/2 -translate-y-1/2 place-items-center rounded-full bg-void-900 text-aegis-300 ring-1 ring-aegis-400/50"
    >
      <Icon name="keyhole" stroke={2.2} class="size-3" />
    </span>
  </div>
{/snippet}

<section id="hranice" class="px-4 py-28 sm:px-6 md:py-40" aria-labelledby="limits-title">
  <div class="glass relative mx-auto max-w-[1160px] reveal-up overflow-hidden rounded-card p-6 md:p-10">
    <div
      class="pointer-events-none absolute -top-24 -left-24 size-80 rounded-full bg-[radial-gradient(closest-side,oklch(0.5_0.17_305/0.3),transparent)]"
      aria-hidden="true"
    ></div>
    <div class="relative grid items-center gap-10 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)] lg:gap-14">
      <div>
        <p class="text-sm font-medium text-nebula-300">{t('limits.eyebrow')}</p>
        <h2
          id="limits-title"
          class="mt-3 font-display text-[26px] leading-[1.2] font-light tracking-[-0.02em] text-balance md:text-[36px]"
        >
          {t('limits.title')}
        </h2>
        <p class="mt-4 max-w-[440px] leading-relaxed text-pretty text-ink-soft">{t('limits.sub')}</p>
      </div>

      <div class="rounded-pane px-4 py-6 well md:px-8 md:py-8" role="img" aria-label={t('limits.diagram')}>
        <div class="grid grid-cols-[auto_minmax(0,1fr)_auto_minmax(0,1fr)_auto] items-start" aria-hidden="true">
          {@render device('laptop', t('limits.you'))}
          {@render wire('out')}
          <div class="flex w-[84px] flex-col items-center text-center">
            <span
              class="grid size-14 place-items-center rounded-control bg-aegis-500/15 text-aegis-300 shadow-[0_0_24px_-6px_oklch(0.56_0.19_258/0.7)] ring-1 ring-aegis-400/45"
            >
              <Icon name="database" stroke={1.6} class="size-6" />
            </span>
            <span class="mt-3 text-[13px] text-ink">{t('limits.server')}</span>
            <span class="text-[11px] font-medium tracking-[0.08em] whitespace-nowrap text-aegis-300 uppercase"
              >{t('limits.ours')}</span
            >
          </div>
          {@render wire('in')}
          {@render device('phone', t('limits.recipient'))}
        </div>
      </div>
    </div>

    <ul class="relative mt-10 grid gap-3 md:grid-cols-2">
      {#each limits as limit (limit.key)}
        <li class="flex items-center gap-4 rounded-pane p-4 well">
          <span
            class="grid size-10 shrink-0 place-items-center rounded-control bg-nebula-400/10 text-nebula-300 ring-1 ring-nebula-300/30"
          >
            <Icon name={limit.icon} stroke={1.6} class="size-5" />
          </span>
          <span class="min-w-0">
            <span class="block text-[15px] text-ink">{t(`${limit.key}.title`)}</span>
            <span class="mt-0.5 block text-[13px] leading-relaxed text-ink-mute">{t(`${limit.key}.note`)}</span>
          </span>
        </li>
      {/each}
    </ul>
    <div class="relative mt-6 flex flex-col gap-3 px-1 sm:flex-row sm:items-center sm:justify-between sm:gap-6">
      <p class="max-w-[760px] text-xs leading-relaxed text-ink-mute">{t('limits.liability')}</p>
      <a
        href="/report"
        class="btn-ghost flex h-11 shrink-0 items-center justify-center gap-2 self-start rounded-control px-4 text-[13px] sm:self-auto md:h-9"
      >
        <Icon name="flag" class="size-4" />
        {t('limits.report')}
      </a>
    </div>
  </div>
</section>
