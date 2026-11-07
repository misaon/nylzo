<script lang="ts">
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import { reveal } from '../../shared/ui/reveal/reveal.ts';
  import IconTile from '../../shared/ui/tile/IconTile.svelte';

  const CARDS = [
    { key: 'open.trust.key', icon: 'key', order: '[--rv-i:0]' },
    { key: 'open.trust.click', icon: 'eye', order: '[--rv-i:1]' },
    { key: 'open.trust.gone', icon: 'trash', order: '[--rv-i:2]' },
  ] as const;

  const { translate } = getTranslator();
  const id = $props.id();
</script>

<section class="px-4 pb-28 sm:px-6 md:pb-36" aria-labelledby="{id}-trust">
  <div class="mx-auto max-w-[1160px]" {@attach reveal}>
    <h2 id="{id}-trust" class="rv-focus text-center text-eyebrow text-aegis-300 uppercase">
      {translate('open.trust.title')}
    </h2>
    <ol class="mt-10 grid gap-6 md:grid-cols-3">
      {#each CARDS as card (card.key)}
        <li
          class={[
            'glow-on-hover glass flex hover-lift rv-rise items-start gap-4 rounded-card p-5 text-left shadow-[0_20px_50px_-28px_rgb(40_70_200/0.4),inset_0_1px_0_rgb(255_255_255/0.06)] [--rv-delay:120ms] [--rv-s:0.96] [--rv-step:120ms] md:flex-col md:items-center md:gap-0 md:px-6 md:pt-7 md:pb-8 md:text-center',
            card.order,
          ]}
        >
          <IconTile icon={card.icon} tone="current" />
          <div class="min-w-0">
            <h3 class="text-title font-medium tracking-[-0.01em] md:mt-6">{translate(`${card.key}.title`)}</h3>
            <p class="mt-1 max-w-[300px] text-body leading-[1.65] text-pretty text-ink-soft md:mx-auto md:mt-2">
              {translate(`${card.key}.text`)}
            </p>
          </div>
        </li>
      {/each}
    </ol>
  </div>
</section>
