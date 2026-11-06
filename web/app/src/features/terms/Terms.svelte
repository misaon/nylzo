<script lang="ts">
  import type { MessageKey } from '../../shared/language/czech.ts';
  import { formatDays } from '../../shared/language/format.ts';
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import Icon from '../../shared/ui/icon/Icon.svelte';
  import { LONGEST_TTL_DAYS } from '../create/composer/create-form.ts';

  interface TermsSection {
    readonly title: MessageKey;
    readonly items: readonly MessageKey[];
    readonly note?: MessageKey;
  }

  const SUMMARY = ['terms.summary.1', 'terms.summary.2', 'terms.summary.3', 'terms.summary.4'] as const;

  const SECTIONS: readonly TermsSection[] = [
    { title: 'terms.operator.title', items: ['terms.operator.1', 'terms.operator.2'] },
    { title: 'terms.agreement.title', items: ['terms.agreement.1'] },
    {
      title: 'terms.service.title',
      items: ['terms.service.1', 'terms.service.2', 'terms.service.3', 'terms.service.4'],
    },
    {
      title: 'terms.forbidden.title',
      items: [
        'terms.forbidden.1',
        'terms.forbidden.2',
        'terms.forbidden.3',
        'terms.forbidden.4',
        'terms.forbidden.5',
        'terms.forbidden.6',
      ],
      note: 'terms.forbidden.note',
    },
    { title: 'terms.removal.title', items: ['terms.removal.1', 'terms.removal.2', 'terms.removal.3'] },
    { title: 'terms.warranty.title', items: ['terms.warranty.1', 'terms.warranty.2', 'terms.warranty.3'] },
    { title: 'terms.brand.title', items: ['terms.brand.1'] },
    { title: 'terms.other.title', items: ['terms.other.1', 'terms.other.2'] },
  ];

  const translator = getTranslator();
  const { translate } = translator;
  const longest = $derived(formatDays(LONGEST_TTL_DAYS, translator.language));
</script>

<article aria-labelledby="legal-title">
  <p class="text-eyebrow text-aegis-300 uppercase">{translate('terms.eyebrow')}</p>
  <h1 id="legal-title" class="mt-3 font-display text-page font-light text-ink md:text-page-lg">
    {translate('terms.title')}
  </h1>
  <p class="mt-3 text-caption text-ink-mute">{translate('terms.effective')}</p>
  <section class="glass-soft mt-8 rounded-card p-5 md:p-6">
    <h2 class="text-title font-medium text-ink">{translate('terms.summary.title')}</h2>
    <ul class="mt-4 grid gap-2.5 text-body-sm text-pretty text-ink-soft">
      {#each SUMMARY as item (item)}
        <li class="flex gap-2.5">
          <Icon name="check" class="mt-0.5 size-4 shrink-0 text-aegis-300" stroke={2} />
          <span>{translate(item)}</span>
        </li>
      {/each}
    </ul>
    <p class="mt-4 text-caption text-ink-mute">{translate('terms.summary.note')}</p>
  </section>
  {#each SECTIONS as section (section.title)}
    <h2 class="mt-10 text-title font-medium text-ink">{translate(section.title)}</h2>
    <ul class="mt-3 grid list-disc gap-2 pl-5 text-body text-pretty text-ink-soft">
      {#each section.items as item (item)}
        <li>{translate(item, { days: longest })}</li>
      {/each}
    </ul>
    {#if section.note}
      <p class="mt-3 text-body text-pretty text-ink-soft">{translate(section.note)}</p>
    {/if}
  {/each}
</article>
