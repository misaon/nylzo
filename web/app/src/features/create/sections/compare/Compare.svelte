<script lang="ts">
  import { getTranslator } from '../../../../shared/language/translator.svelte.ts';
  import Icon from '../../../../shared/ui/icon/Icon.svelte';
  import type { IconName } from '../../../../shared/ui/icon/icons.ts';
  import { reveal, REVEAL_INDEXES } from '../../../../shared/ui/reveal/reveal.ts';
  import SectionIntro from '../../../../shared/ui/section/SectionIntro.svelte';
  import { COMPARE_ROWS } from './compare-rows.ts';
  import type { MarkKind } from './compare-rows.ts';

  const { translate } = getTranslator();

  const PRODUCTS = ['PrivateBin', 'Yopass', 'OneTimeSecret', 'Bitwarden Send'] as const;
  const STICKY =
    'sticky left-0 z-10 bg-void-900 shadow-[1px_0_0_oklch(1_0_0/0.08)] md:static md:bg-transparent md:shadow-none';
  const NYLZO_COLUMN =
    'bg-aegis-500/12 shadow-[inset_1px_0_0_oklch(0.67_0.16_252/0.3),inset_-1px_0_0_oklch(0.67_0.16_252/0.3)]';
  const MARK_DELAYS = [
    '[--reveal-delay:520ms]',
    '[--reveal-delay:690ms]',
    '[--reveal-delay:860ms]',
    '[--reveal-delay:1030ms]',
    '[--reveal-delay:1200ms]',
  ] as const;
  const MARKS: Readonly<
    Record<
      MarkKind,
      {
        readonly icon: IconName;
        readonly tone: string;
        readonly mark: string;
        readonly label: 'compare.yes' | 'compare.no' | 'compare.part';
      }
    >
  > = {
    yes: { icon: 'check', tone: 'text-aegis-200', mark: 'bg-aegis-500/20', label: 'compare.yes' },
    strong: {
      icon: 'check',
      tone: 'text-aegis-200',
      mark: 'bg-aegis-400 text-void-950 shadow-glow',
      label: 'compare.yes',
    },
    no: { icon: 'close', tone: 'text-ink-mute', mark: 'bg-white/6 text-ink-mute', label: 'compare.no' },
    partial: { icon: 'minus', tone: 'text-ink-soft', mark: 'bg-solar-300/10 text-solar-300', label: 'compare.part' },
  };
</script>

<span id="srovnani" class="block" aria-hidden="true"></span>

<section id="comparison" class="px-4 py-28 sm:px-6 md:py-40" aria-labelledby="compare-title">
  <div class="mx-auto max-w-page">
    <div class="max-w-[600px]" {@attach reveal}>
      <SectionIntro id="compare-title" eyebrow={translate('compare.eyebrow')} title={translate('compare.title')} />
    </div>
    <p class="mt-6 flex reveal-focus items-center gap-1.5 text-micro text-ink-mute md:hidden" {@attach reveal}>
      <Icon name="arrowRight" class="size-3.5 shrink-0" stroke={2} />
      {translate('compare.hint')}
    </p>
    <div
      class="glass mt-4 reveal-rise overflow-hidden rounded-card outline-offset-3 outline-transparent transition-[outline-color] not-forced-colors:outline-2 has-focus-visible:outline-2 has-focus-visible:outline-aegis-300 md:mt-12"
      {@attach reveal}
    >
      <!-- The table scrolls sideways on a phone, so keyboard users must be able to focus it. -->
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <div class="overflow-x-auto outline-none" role="region" aria-labelledby="compare-title" tabindex="0">
        <table
          class="w-full min-w-170 table-fixed border-collapse text-left [--reveal-delay:150ms] [--reveal-step:45ms] md:min-w-0"
        >
          <caption class="sr-only">{translate('compare.caption')}</caption>
          <colgroup>
            <col class="w-36 md:w-[38%]" />
            <col class="w-30 md:w-[14%]" />
            {#each PRODUCTS as product (product)}
              <col class="w-26 md:w-[12%]" />
            {/each}
          </colgroup>
          <thead>
            <tr class="reveal-fade border-b border-white/8">
              <th scope="col" class={['px-4 py-4 text-micro font-normal text-ink-mute md:px-6', STICKY]}>
                {translate('compare.feature')}
              </th>
              <th
                scope="col"
                class="bg-aegis-500/12 px-4 py-4 text-center font-display text-panel font-normal text-aegis-200 shadow-[inset_1px_0_0_oklch(0.67_0.16_252/0.3),inset_-1px_0_0_oklch(0.67_0.16_252/0.3),inset_0_2px_0_oklch(0.77_0.115_249/0.7)] md:text-panel-lg"
              >
                Nylzo
              </th>
              {#each PRODUCTS as product (product)}
                <th scope="col" class="px-4 py-4 text-center text-ui font-medium text-ink-soft">{product}</th>
              {/each}
            </tr>
          </thead>
          <tbody>
            {#each COMPARE_ROWS as row, index (row.label)}
              <tr class={['reveal-fade border-b border-white/8 last:border-0', REVEAL_INDEXES[index + 1]]}>
                <th scope="row" class={['px-4 py-3 text-ui font-normal text-balance text-ink md:px-6', STICKY]}>
                  {translate(row.label)}
                </th>
                {#each row.cells as cell, column (column)}
                  <td class={['px-4 py-3 text-center align-middle', column === 0 && NYLZO_COLUMN]}>
                    {#if cell.kind === 'text'}
                      <span class={['text-micro tabular-nums', cell.tone]}>{translate(cell.note)}</span>
                    {:else}
                      {const mark = $derived(MARKS[cell.kind])}
                      <span class={['flex flex-col items-center gap-1 text-micro', mark.tone]}>
                        <span
                          class={[
                            'grid size-5 shrink-0 reveal-pop place-items-center rounded-full [--reveal-duration:0.55s] [--reveal-step:35ms]',
                            mark.mark,
                            MARK_DELAYS[column],
                          ]}
                        >
                          <Icon name={mark.icon} class="size-3" stroke={2.2} />
                        </span>
                        <span class="sr-only">{translate(mark.label)}</span>
                        {#if cell.note}
                          <span class="leading-snug text-ink-mute">{translate(cell.note)}</span>
                        {/if}
                      </span>
                    {/if}
                  </td>
                {/each}
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </div>
  </div>
</section>
