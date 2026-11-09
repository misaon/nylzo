<script lang="ts">
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import { REPOSITORY_URL } from '../../../shared/shell/repository.ts';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import { ARROW_LINK } from '../../../shared/ui/link/link-classes.ts';
  import { reveal } from '../../../shared/ui/reveal/reveal.ts';
  import SectionIntro from '../../../shared/ui/section/SectionIntro.svelte';
  import IconTile from '../../../shared/ui/tile/IconTile.svelte';

  const { translate } = getTranslator();

  const layers = [
    {
      key: 'tech.crypto',
      icon: 'lock',
      revealIndex: 'md:[--reveal-index:0] lg:[--reveal-index:0]',
      items: ['tech.crypto.1', 'tech.crypto.2', 'tech.crypto.3', 'tech.crypto.4', 'tech.crypto.5'],
    },
    {
      key: 'tech.server',
      icon: 'server',
      revealIndex: 'md:[--reveal-index:1] lg:[--reveal-index:1]',
      items: ['tech.server.1', 'tech.server.2', 'tech.server.3', 'tech.server.4', 'tech.server.5'],
    },
    {
      key: 'tech.web',
      icon: 'globe',
      revealIndex: 'md:[--reveal-index:0] lg:[--reveal-index:2]',
      items: ['tech.web.1', 'tech.web.2', 'tech.web.3', 'tech.web.4', 'tech.web.5'],
    },
    {
      key: 'tech.release',
      icon: 'shieldCheck',
      revealIndex: 'md:[--reveal-index:1] lg:[--reveal-index:3]',
      items: [
        'tech.release.1',
        'tech.release.2',
        'tech.release.3',
        'tech.release.4',
        'tech.release.5',
        'tech.release.6',
      ],
    },
  ] as const;
  const tests = ['tech.test.headers', 'tech.test.observatory', 'tech.test.pagespeed', 'tech.test.scorecard'] as const;
</script>

<section id="technology" class="px-4 py-28 sm:px-6 md:py-40" aria-labelledby="tech-title">
  <div class="mx-auto max-w-page">
    <div class="max-w-[640px]" {@attach reveal}>
      <SectionIntro id="tech-title" eyebrow={translate('tech.eyebrow')} title={translate('tech.title')} />
      <p
        class="mt-5 max-w-[520px] reveal-focus text-lead leading-relaxed text-pretty text-ink-soft [--reveal-index:2] md:text-lead-lg"
      >
        {translate('tech.sub')}
      </p>
    </div>
    <div
      class="mt-10 flex reveal-focus flex-col gap-4 rounded-pane px-5 py-4 well [--reveal-blur:4px] lg:flex-row lg:items-center lg:gap-6"
      {@attach reveal}
    >
      <a href={`${REPOSITORY_URL}#security`} rel="noreferrer" class={[ARROW_LINK, '-my-2.5 min-h-11 shrink-0 text-ui']}>
        {translate('tech.tests.title')}
        <Icon name="arrowRight" class="size-3.5" stroke={2} animated />
      </a>
      <ul role="list" class="flex flex-wrap gap-2">
        {#each tests as test (test)}
          <li
            class="inline-flex items-center gap-2 rounded-full bg-aegis-500/12 px-3 py-1.5 text-caption text-aegis-200 tabular-nums ring-1 ring-aegis-400/35"
          >
            <Icon name="check" class="size-3.5 shrink-0" stroke={2} />
            {translate(test)}
          </li>
        {/each}
      </ul>
    </div>
    <div class="mt-5 grid gap-4 md:grid-cols-2 md:gap-5 lg:grid-cols-4">
      {#each layers as layer (layer.key)}
        <article
          class={[
            'glow-on-hover glass flex reveal-rise flex-col rounded-card p-6 [--reveal-step:120ms] md:p-7',
            layer.revealIndex,
          ]}
          {@attach reveal}
        >
          <div class="flex items-center gap-4">
            <IconTile
              icon={layer.icon}
              tone="current"
              class={['reveal-pop [--reveal-delay:300ms] [--reveal-step:120ms]', layer.revealIndex]}
            />
            <h3 class="text-title text-balance">{translate(`${layer.key}.title`)}</h3>
          </div>
          <ul role="list" class="mt-5 grid gap-3">
            {#each layer.items as item (item)}
              <li class="grid grid-cols-[auto_minmax(0,1fr)] gap-2.5 text-body-sm text-pretty text-ink-soft">
                <Icon name="check" class="mt-1 size-3.5 text-aegis-300" stroke={2} />
                {translate(item)}
              </li>
            {/each}
          </ul>
        </article>
      {/each}
    </div>
  </div>
</section>
