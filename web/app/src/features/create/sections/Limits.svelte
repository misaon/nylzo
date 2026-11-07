<script lang="ts">
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import { VERIFIED_CLIENTS_URL } from '../../../shared/shell/repository.ts';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import { ARROW_LINK, INLINE_TARGET, TEXT_LINK } from '../../../shared/ui/link/link-classes.ts';
  import { reveal } from '../../../shared/ui/reveal/reveal.ts';
  import IconTile from '../../../shared/ui/tile/IconTile.svelte';

  const { translate } = getTranslator();

  const limits = [
    { key: 'limits.save', icon: 'camera', revealIndex: '[--reveal-index:0]' },
    { key: 'limits.link', icon: 'link', revealIndex: '[--reveal-index:1]' },
    { key: 'limits.server', icon: 'shield', revealIndex: '[--reveal-index:2]' },
    { key: 'limits.abuse', icon: 'ban', revealIndex: '[--reveal-index:3]' },
  ] as const;
  const DEVICE =
    'reveal-pop grid size-12 place-items-center rounded-pane text-white outline-1 outline-offset-3 outline-solar-300/30 outline-dashed tile-warning [--reveal-delay:900ms]';
  const BADGE =
    'reveal-pop grid size-5 shrink-0 place-items-center rounded-full bg-aegis-500/12 text-aegis-200 ring-1 ring-aegis-400/35 [--reveal-delay:560ms]';
  const DASHED =
    'reveal-draw mt-6 h-1 bg-[repeating-linear-gradient(90deg,oklch(0.86_0.12_85/0.55)_0_6px,transparent_6px_11px)] [--reveal-delay:820ms]';
  const LABEL = 'reveal-focus mt-3 text-center text-caption text-balance text-ink [--reveal-blur:4px] [--reveal-y:6px]';
  const ZONE =
    'reveal-focus mt-0.5 text-center text-eyebrow whitespace-nowrap uppercase [--reveal-blur:4px] [--reveal-y:6px]';
</script>

<span id="hranice" class="block" aria-hidden="true"></span>

<section id="limits" class="px-4 py-28 sm:px-6 md:py-40" aria-labelledby="limits-title">
  <div class="glass mx-auto max-w-page reveal-rise overflow-hidden rounded-card p-6 md:p-10" {@attach reveal}>
    <div
      class="pointer-events-none absolute -top-32 -right-24 size-96 reveal-glow rounded-full bg-[radial-gradient(closest-side,oklch(0.86_0.12_85/0.1),transparent)] [--reveal-delay:200ms]"
      aria-hidden="true"
    ></div>
    <div class="relative grid items-center gap-10 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)] lg:gap-14">
      <div>
        <p class="reveal-focus text-eyebrow text-solar-300 uppercase [--reveal-delay:150ms] [--reveal-index:0]">
          {translate('limits.eyebrow')}
        </p>
        <h2
          id="limits-title"
          class="mt-3 reveal-focus font-display text-section font-light text-balance [--reveal-delay:150ms] [--reveal-index:1] md:text-section-lg"
        >
          {translate('limits.title')}
        </h2>
        <p
          class="mt-4 max-w-[440px] reveal-focus text-lead leading-relaxed text-pretty text-ink-soft [--reveal-delay:150ms] [--reveal-index:2] md:text-lead-lg"
        >
          {translate('limits.sub')}
        </p>
      </div>
      <div
        class="rounded-pane px-4 py-6 well md:px-8 md:py-8"
        role="img"
        aria-label={translate('limits.diagram')}
        {@attach reveal}
      >
        <div class="grid grid-cols-[minmax(0,1fr)_minmax(0,1.5fr)_minmax(0,1fr)] gap-x-1.5" aria-hidden="true">
          <div class="flex justify-center">
            <span class={DEVICE}><Icon name="laptop" class="size-6" stroke={1.6} /></span>
          </div>
          <div class="flex items-center justify-center gap-1.5">
            <span class={BADGE}><Icon name="keyhole" class="size-3" stroke={2.2} /></span>
            <span class="hidden h-px flex-1 origin-right reveal-draw bg-aegis-400/50 [--reveal-delay:280ms] sm:block"
            ></span>
            <IconTile
              icon="database"
              tone="current"
              class="reveal-pop shadow-[0_0_24px_-6px_oklch(0.56_0.19_258/0.7)]"
            />
            <span class="hidden h-px flex-1 origin-left reveal-draw bg-aegis-400/50 [--reveal-delay:280ms] sm:block"
            ></span>
            <span class={BADGE}><Icon name="keyhole" class="size-3" stroke={2.2} /></span>
          </div>
          <div class="flex justify-center">
            <span class={DEVICE}><Icon name="phone" class="size-6" stroke={1.6} /></span>
          </div>
          <span class={[DASHED, 'origin-right rounded-l-full']}></span>
          <span
            class="mt-6 h-1 origin-center reveal-draw rounded-full bg-aegis-400 shadow-glow [--reveal-delay:250ms] [--reveal-duration:1.1s]"
          ></span>
          <span class={[DASHED, 'origin-left rounded-r-full']}></span>
          <span class={[LABEL, '[--reveal-delay:1000ms]']}>{translate('limits.you')}</span>
          <span class={[LABEL, '[--reveal-delay:1060ms]']}>{translate('limits.server')}</span>
          <span class={[LABEL, '[--reveal-delay:1120ms]']}>{translate('limits.recipient')}</span>
          <span class={[ZONE, 'text-solar-300 [--reveal-delay:1090ms]']}>{translate('limits.outside')}</span>
          <span class={[ZONE, 'text-aegis-300 [--reveal-delay:1150ms]']}>{translate('limits.ours')}</span>
          <span class={[ZONE, 'text-solar-300 [--reveal-delay:1210ms]']}>{translate('limits.outside')}</span>
        </div>
      </div>
    </div>
    <div class="relative mt-8 overflow-hidden rounded-pane well md:mt-10" {@attach reveal}>
      <ul role="list" class="grid md:grid-cols-2">
        {#each limits as limit (limit.key)}
          <li
            class={[
              'grid reveal-focus grid-cols-[auto_minmax(0,1fr)] content-start items-center gap-x-4 border-white/8 p-5 [--reveal-blur:5px] [--reveal-step:90ms] max-md:not-last:border-b md:p-6 md:odd:border-r md:nth-[-n+2]:border-b',
              limit.revealIndex,
            ]}
          >
            <IconTile
              icon={limit.icon}
              tone="warning"
              size="medium"
              class={[
                'row-span-2 reveal-pop self-center [--reveal-delay:150ms] [--reveal-step:90ms]',
                limit.revealIndex,
              ]}
            />
            <h3 class="col-start-2 text-title-sm text-balance text-ink">
              {translate(`${limit.key}.title`)}
            </h3>
            <p class="col-start-2 mt-1 text-caption text-pretty text-ink-soft">{translate(`${limit.key}.note`)}</p>
            {#if limit.key === 'limits.server'}
              <a
                href={VERIFIED_CLIENTS_URL}
                rel="noreferrer"
                class={[ARROW_LINK, 'col-start-2 -mb-2.5 min-h-11 text-caption']}
              >
                {translate('limits.server.link')}
                <Icon name="arrowRight" class="size-3.5" stroke={2} animated />
              </a>
            {/if}
          </li>
        {/each}
      </ul>
      <div
        class="flex reveal-focus flex-col gap-3 border-t border-white/8 bg-white/4 px-5 py-4 [--reveal-blur:4px] [--reveal-index:4] [--reveal-step:90ms] sm:flex-row sm:items-center sm:justify-between sm:gap-6 md:px-6"
      >
        <p class="max-w-[760px] text-caption text-ink-mute">
          {translate('limits.liability')}
          <a href="/terms" class={[TEXT_LINK, INLINE_TARGET]}>{translate('limits.terms')}</a>.
        </p>
      </div>
    </div>
  </div>
</section>
