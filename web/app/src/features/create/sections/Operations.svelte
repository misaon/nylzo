<script lang="ts">
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import { VERIFIED_CLIENTS_URL } from '../../../shared/shell/repository.ts';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import { ARROW_LINK } from '../../../shared/ui/link/link-classes.ts';
  import { reveal, REVEAL_INDEXES } from '../../../shared/ui/reveal/reveal.ts';
  import IconTile from '../../../shared/ui/tile/IconTile.svelte';

  const { translate } = getTranslator();

  const safeguards = [
    { key: 'operations.watch', icon: 'refresh', border: 'border-b md:col-span-2' },
    { key: 'operations.tls', icon: 'lock', border: 'border-b md:border-r' },
    { key: 'operations.host', icon: 'server', border: 'border-b' },
    { key: 'operations.access', icon: 'keyhole', border: 'max-md:border-b md:border-r' },
    { key: 'operations.updates', icon: 'update', border: '' },
  ] as const;
  const nodes = [
    { label: 'operations.release', zone: 'operations.signed', icon: 'shieldCheck', delay: '[--reveal-delay:900ms]' },
    { label: 'operations.instance', zone: 'operations.compared', icon: 'server', delay: '[--reveal-delay:420ms]' },
    { label: 'operations.check', zone: 'operations.every', icon: 'check', delay: '[--reveal-delay:900ms]' },
  ] as const;
  const LABEL = 'reveal-focus mt-3 text-center text-caption text-balance text-ink [--reveal-blur:4px] [--reveal-y:6px]';
  const ZONE =
    'reveal-focus mt-0.5 text-center text-eyebrow text-balance text-aegis-300 uppercase [--reveal-blur:4px] [--reveal-y:6px]';
</script>

<span id="hranice" class="block" aria-hidden="true"></span>
<span id="limits" class="block" aria-hidden="true"></span>

<section id="operations" class="px-4 py-28 sm:px-6 md:py-40" aria-labelledby="operations-title">
  <div class="glass mx-auto max-w-page reveal-rise overflow-hidden rounded-card p-6 md:p-10" {@attach reveal}>
    <div
      class="pointer-events-none absolute -top-32 -right-24 size-96 reveal-glow rounded-full bg-[radial-gradient(closest-side,oklch(0.56_0.19_258/0.14),transparent)] [--reveal-delay:200ms]"
      aria-hidden="true"
    ></div>
    <div class="relative grid items-center gap-10 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)] lg:gap-14">
      <div>
        <p class="reveal-focus text-eyebrow text-aegis-300 uppercase [--reveal-delay:150ms] [--reveal-index:0]">
          {translate('operations.eyebrow')}
        </p>
        <h2
          id="operations-title"
          class="mt-3 reveal-focus font-display text-section font-light text-balance [--reveal-delay:150ms] [--reveal-index:1] md:text-section-lg"
        >
          {translate('operations.title')}
        </h2>
        <p
          class="mt-4 max-w-[440px] reveal-focus text-lead leading-relaxed text-pretty text-ink-soft [--reveal-delay:150ms] [--reveal-index:2] md:text-lead-lg"
        >
          {translate('operations.sub')}
        </p>
      </div>
      <div
        class="rounded-pane px-4 py-6 well md:px-8 md:py-8"
        role="img"
        aria-label={translate('operations.diagram')}
        {@attach reveal}
      >
        <div class="relative grid grid-cols-3 gap-x-1.5" aria-hidden="true">
          <span
            class="absolute inset-x-[16%] top-6 h-px origin-left reveal-draw bg-aegis-400/60 shadow-glow [--reveal-delay:250ms] [--reveal-duration:1.1s]"
          ></span>
          {#each nodes as node (node.label)}
            <div class="relative flex flex-col items-center">
              <IconTile
                icon={node.icon}
                tone="current"
                class={['reveal-pop shadow-[0_0_24px_-6px_oklch(0.56_0.19_258/0.7)]', node.delay]}
              />
              <span class={[LABEL, '[--reveal-delay:1000ms]']}>{translate(node.label)}</span>
              <span class={[ZONE, '[--reveal-delay:1120ms]']}>{translate(node.zone)}</span>
            </div>
          {/each}
        </div>
      </div>
    </div>
    <div class="relative mt-8 overflow-hidden rounded-pane well md:mt-10" {@attach reveal}>
      <ul role="list" class="grid md:grid-cols-2">
        {#each safeguards as safeguard, index (safeguard.key)}
          <li
            class={[
              'grid reveal-focus grid-cols-[auto_minmax(0,1fr)] content-start items-center gap-x-4 border-white/8 p-5 [--reveal-blur:5px] [--reveal-step:90ms] md:p-6',
              safeguard.border,
              REVEAL_INDEXES[index],
            ]}
          >
            <IconTile
              icon={safeguard.icon}
              tone="current"
              size="medium"
              class={[
                'row-span-2 reveal-pop self-center [--reveal-delay:150ms] [--reveal-step:90ms]',
                REVEAL_INDEXES[index],
              ]}
            />
            <h3 class="col-start-2 text-title-sm text-balance text-ink">
              {translate(`${safeguard.key}.title`)}
            </h3>
            <p class="col-start-2 mt-1 text-caption text-pretty text-ink-soft">{translate(`${safeguard.key}.text`)}</p>
          </li>
        {/each}
      </ul>
      <div
        class="flex reveal-focus flex-col gap-3 border-t border-white/8 bg-white/4 px-5 py-4 [--reveal-blur:4px] [--reveal-index:4] [--reveal-step:90ms] sm:flex-row sm:items-center sm:justify-between sm:gap-6 md:px-6"
      >
        <p class="max-w-[640px] text-caption text-pretty text-ink-soft">{translate('operations.apps')}</p>
        <a href={VERIFIED_CLIENTS_URL} rel="noreferrer" class={[ARROW_LINK, '-my-2.5 min-h-11 shrink-0 text-caption']}>
          {translate('operations.apps.link')}
          <Icon name="arrowRight" class="size-3.5" stroke={2} animated />
        </a>
      </div>
    </div>
  </div>
</section>
