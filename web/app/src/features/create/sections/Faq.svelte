<script lang="ts">
  import { formatDays } from '../../../shared/language/format.ts';
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Accordion from '../../../shared/ui/accordion/Accordion.svelte';
  import { reveal } from '../../../shared/ui/reveal/reveal.ts';
  import { LONGEST_TTL_DAYS } from '../composer/create-form.ts';
  import { FAQ_COLUMNS } from './faq-entries.ts';

  const translator = getTranslator();
  const { translate } = translator;
  const longest = $derived(formatDays(LONGEST_TTL_DAYS, translator.language));

  const columns = FAQ_COLUMNS;
  const COLUMN_DELAYS = ['md:[--rv-delay:0ms]', 'md:[--rv-delay:110ms]'] as const;
</script>

<section class="px-4 pt-20 pb-6 sm:px-6 md:pt-28" aria-labelledby="faq-title">
  <div class="mx-auto max-w-[1160px]">
    <p class="rv-focus text-eyebrow text-aegis-300 uppercase" {@attach reveal}>{translate('faq.eyebrow')}</p>
    <h2
      id="faq-title"
      class="mt-3 rv-focus font-display text-section font-light text-ink [--rv-delay:80ms] md:text-section-lg"
      {@attach reveal}
    >
      {translate('faq.title')}
    </h2>
    <div class="mt-10 grid gap-4 md:grid-cols-2 md:items-start">
      {#each columns as items, column (column)}
        <div class="grid content-start gap-4 max-md:contents">
          {#each items as item (item.key)}
            <Accordion
              summary={translate(`${item.key}.question`)}
              class={['rv-rise [--rv-y:16px]', item.order, COLUMN_DELAYS[column]]}
              attachment={reveal}
            >
              {translate(`${item.key}.answer`, { days: longest })}
            </Accordion>
          {/each}
        </div>
      {/each}
    </div>
  </div>
</section>
