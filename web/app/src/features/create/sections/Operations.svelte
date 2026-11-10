<script lang="ts">
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import { VERIFIED_CLIENTS_URL } from '../../../shared/shell/repository.ts';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import { ARROW_LINK } from '../../../shared/ui/link/link-classes.ts';
  import { reveal, REVEAL_INDEXES } from '../../../shared/ui/reveal/reveal.ts';
  import IconTile from '../../../shared/ui/tile/IconTile.svelte';

  const { translate } = getTranslator();

  const NODE_GLOW = 'shadow-[0_0_24px_-6px_oklch(0.56_0.19_258/0.7)]';
  const nodes = [
    { label: 'operations.release', zone: 'operations.signed', icon: 'shieldCheck', focus: false },
    { label: 'operations.check', zone: 'operations.every', icon: 'refresh', focus: true },
    { label: 'operations.instance', zone: 'operations.compared', icon: 'server', focus: false },
  ] as const;
  const safeguards = [
    { key: 'operations.tls', icon: 'atom' },
    { key: 'operations.host', icon: 'server' },
    { key: 'operations.access', icon: 'keyhole' },
    { key: 'operations.updates', icon: 'update' },
  ] as const;
  const LINE =
    'absolute top-6 h-px reveal-draw bg-aegis-400/50 shadow-glow max-sm:hidden [--reveal-delay:450ms] [--reveal-duration:0.8s]';
  const COMET =
    'absolute inset-y-[-1px] left-0 w-1/3 bg-linear-to-r from-transparent via-aegis-200 to-transparent motion-safe:animate-beam motion-reduce:hidden';
  const LABEL =
    'reveal-focus block text-caption text-balance text-ink [--reveal-blur:4px] [--reveal-y:6px] [--reveal-delay:1000ms] sm:mt-3';
  const ZONE =
    'reveal-focus mt-0.5 block text-eyebrow text-balance text-aegis-300 uppercase [--reveal-blur:4px] [--reveal-y:6px] [--reveal-delay:1120ms]';
  const FACT =
    'mt-auto inline-flex items-center gap-1.5 self-start rounded-full bg-aegis-500/12 px-3 py-1.5 text-caption text-aegis-200 tabular-nums ring-1 ring-aegis-400/35';
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
      <div class="overflow-hidden rounded-pane well" {@attach reveal}>
        <div class="px-5 py-7 md:px-8 md:py-9" role="img" aria-label={translate('operations.diagram')}>
          <div class="relative grid gap-y-5 sm:grid-cols-3 sm:gap-x-1.5 sm:gap-y-0" aria-hidden="true">
            <span class={[LINE, 'right-1/2 left-[16.67%] origin-right']}>
              <span class="absolute inset-0 overflow-visible"><span class={COMET}></span></span>
            </span>
            <span class={[LINE, 'right-[16.67%] left-1/2 origin-left']}>
              <span class="absolute inset-0 -scale-x-100 overflow-visible"><span class={COMET}></span></span>
            </span>
            <span class="absolute inset-y-6 left-6 w-px bg-aegis-400/50 shadow-glow sm:hidden"></span>
            {#each nodes as node (node.label)}
              <div class="relative flex items-center gap-4 sm:flex-col sm:gap-0">
                <span class="relative shrink-0 rounded-pane bg-void-950">
                  <IconTile
                    icon={node.icon}
                    tone={node.focus ? 'current' : 'neutral'}
                    class={[
                      'relative reveal-pop',
                      node.focus
                        ? 'shadow-[0_0_0_6px_oklch(0.56_0.19_258/0.14),0_0_30px_-4px_oklch(0.56_0.19_258/0.85)] [--reveal-delay:300ms]'
                        : [NODE_GLOW, '[--reveal-delay:900ms]'],
                    ]}
                  />
                  {#if node.focus}
                    <span
                      class="pointer-events-none absolute -inset-1.5 rounded-[18px] ring-1 ring-aegis-300/35 motion-safe:animate-pulse-core"
                    ></span>
                  {/if}
                </span>
                <span class="min-w-0 sm:text-center">
                  <span class={LABEL}>{translate(node.label)}</span>
                  <span class={ZONE}>{translate(node.zone)}</span>
                </span>
              </div>
            {/each}
          </div>
        </div>
      </div>
    </div>
    <div class="relative mt-10 overflow-hidden rounded-pane well md:mt-12" {@attach reveal}>
      <ul role="list" class="grid md:grid-cols-2">
        {#each safeguards as safeguard, index (safeguard.key)}
          <li
            class={[
              'flex reveal-focus flex-col gap-5 border-white/8 p-6 [--reveal-blur:5px] [--reveal-step:90ms] max-md:not-first:border-t md:p-8 md:odd:border-r md:nth-[-n+2]:border-b',
              REVEAL_INDEXES[index],
            ]}
          >
            <div class="flex items-center gap-3.5">
              <IconTile
                icon={safeguard.icon}
                tone="current"
                size="medium"
                class={['reveal-pop [--reveal-delay:150ms] [--reveal-step:90ms]', REVEAL_INDEXES[index]]}
              />
              <h3 class="text-title-sm text-balance text-ink">{translate(`${safeguard.key}.title`)}</h3>
            </div>
            <p class="text-ui text-pretty text-ink-soft">{translate(`${safeguard.key}.text`)}</p>
            <p class={FACT}>
              <Icon name="check" class="size-3.5 shrink-0" stroke={2} />
              {translate(`${safeguard.key}.fact`)}
            </p>
          </li>
        {/each}
      </ul>
    </div>
    <div
      class="relative mt-3 flex reveal-focus flex-col gap-3 rounded-pane px-5 py-4 well [--reveal-blur:4px] [--reveal-index:4] [--reveal-step:90ms] sm:flex-row sm:items-center sm:justify-between sm:gap-6 md:mt-4 md:px-7 md:py-5"
      {@attach reveal}
    >
      <div class="flex items-center gap-3">
        <IconTile icon="terminal" tone="neutral" size="small" />
        <p class="max-w-[640px] text-caption text-pretty text-ink-soft">{translate('operations.apps')}</p>
      </div>
      <a
        href={VERIFIED_CLIENTS_URL}
        rel="noreferrer"
        class={[ARROW_LINK, '-my-2.5 min-h-11 shrink-0 text-caption max-sm:ml-11']}
      >
        {translate('operations.apps.link')}
        <Icon name="arrowRight" class="size-3.5" stroke={2} animated />
      </a>
    </div>
  </div>
</section>
