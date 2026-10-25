<script lang="ts">
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import Icon from '../../shared/ui/Icon.svelte';

  const CARDS = [
    { key: 'open.trust.key', icon: 'key', reveal: 'reveal-up' },
    { key: 'open.trust.click', icon: 'eye', reveal: 'reveal-up reveal-step-1' },
    { key: 'open.trust.gone', icon: 'trash', reveal: 'reveal-up reveal-step-2' },
  ] as const;

  const { translate } = getTranslator();
  const id = $props.id();
</script>

<section class="px-4 pb-28 sm:px-6 md:pb-36" aria-labelledby="{id}-trust">
  <div class="mx-auto max-w-[1160px]">
    <h2 id="{id}-trust" class="reveal-up text-center text-sm font-medium text-aegis-300">
      {translate('open.trust.title')}
    </h2>
    <ol class="mt-8 grid gap-4 md:grid-cols-3 md:gap-6">
      {#each CARDS as card (card.key)}
        <li
          class={[
            'glow-on-hover glass flex hover-lift items-start gap-4 rounded-card p-5 text-left md:flex-col md:items-center md:gap-0 md:px-6 md:pt-7 md:pb-8 md:text-center',
            card.reveal,
          ]}
        >
          <span
            class="grid size-12 shrink-0 place-items-center rounded-control bg-aegis-500/15 text-aegis-200 ring-1 ring-aegis-400/35"
          >
            <Icon name={card.icon} stroke={1.6} class="size-6" />
          </span>
          <div class="min-w-0">
            <h3 class="text-[17px] font-medium md:mt-6 md:text-lg">{translate(`${card.key}.title`)}</h3>
            <p class="mt-1 max-w-[300px] text-[15px] leading-relaxed text-pretty text-ink-soft md:mx-auto md:mt-2">
              {translate(`${card.key}.text`)}
            </p>
          </div>
        </li>
      {/each}
    </ol>
  </div>
</section>
