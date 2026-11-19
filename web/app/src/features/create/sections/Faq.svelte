<script lang="ts">
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import { LONGEST_TTL_DAYS } from '../../../shared/ttl/ttl-choices.ts';
  import Accordion from '../../../shared/ui/accordion/Accordion.svelte';
  import { reveal } from '../../../shared/ui/reveal/reveal.ts';
  import { FAQ_KEYS } from './faq-entries.ts';

  const translator = getTranslator();
  const { translate } = translator;
  const longest = $derived(translator.plural('count.day', LONGEST_TTL_DAYS));

  const COLUMN_DELAYS = ['md:[--reveal-delay:0ms]', 'md:[--reveal-delay:110ms]'] as const;
</script>

<section class="px-4 pt-20 pb-6 sm:px-6 md:pt-28" aria-labelledby="faq-title">
  <div class="mx-auto max-w-page">
    <p class="reveal-focus text-eyebrow text-aegis-300 uppercase" {@attach reveal}>{translate('faq.eyebrow')}</p>
    <h2
      id="faq-title"
      class="mt-3 reveal-focus font-display text-section font-light text-ink [--reveal-delay:80ms] md:text-section-lg"
      {@attach reveal}
    >
      {translate('faq.title')}
    </h2>
    <div class="mt-10 grid gap-4 md:grid-cols-2 md:items-start">
      {#each FAQ_KEYS as key, index (key)}
        <Accordion
          summary={translate(`${key}.question`)}
          class={['reveal-rise [--reveal-y:16px]', COLUMN_DELAYS[index % COLUMN_DELAYS.length]]}
          attachment={reveal}
        >
          {translate(`${key}.answer`, { days: longest })}
        </Accordion>
      {/each}
    </div>
  </div>
</section>
