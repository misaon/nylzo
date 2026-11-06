<script lang="ts">
  import { getTranslator } from '../../../../shared/language/translator.svelte.ts';
  import Icon from '../../../../shared/ui/icon/Icon.svelte';
  import type { IconName } from '../../../../shared/ui/icon/icons.ts';
  import { reveal } from '../../../../shared/ui/reveal/reveal.ts';
  import { COMPARE_ROWS, HEAD_FADE } from './compare-rows.ts';
  import type { MarkKind } from './compare-rows.ts';

  const { translate } = getTranslator();

  const PRODUCTS = ['PrivateBin', 'Yopass', 'OneTimeSecret', 'Bitwarden Send'] as const;
  const STICKY =
    'sticky left-0 z-10 bg-void-900 shadow-[1px_0_0_oklch(1_0_0/0.08)] md:static md:bg-transparent md:shadow-none';
  const NYLZO_COLUMN =
    'bg-aegis-500/12 shadow-[inset_1px_0_0_oklch(0.67_0.16_252/0.3),inset_-1px_0_0_oklch(0.67_0.16_252/0.3)]';
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
      mark: 'bg-aegis-400 text-void-950 shadow-[0_0_8px_oklch(0.86_0.06_246/0.9)]',
      label: 'compare.yes',
    },
    no: { icon: 'close', tone: 'text-ink-mute', mark: 'bg-white/6 text-ink-mute', label: 'compare.no' },
    partial: { icon: 'minus', tone: 'text-ink-soft', mark: 'bg-solar-300/10 text-solar-300', label: 'compare.part' },
  };
</script>

<span id="srovnani" class="block" aria-hidden="true"></span>

<section id="comparison" class="px-4 py-28 sm:px-6 md:py-40" aria-labelledby="compare-title">
  <div class="mx-auto max-w-[1160px]">
    <div class="flex flex-col gap-4 md:flex-row md:items-end md:justify-between" {@attach reveal}>
      <div class="max-w-[600px]">
        <p class="rv-focus text-eyebrow text-aegis-300 uppercase [--rv-i:0]">{translate('compare.eyebrow')}</p>
        <h2
          id="compare-title"
          class="mt-3 rv-focus font-display text-section font-light text-balance [--rv-i:1] md:text-section-lg"
        >
          {translate('compare.title')}
        </h2>
      </div>
    </div>
    <p class="mt-6 flex rv-focus items-center gap-1.5 text-micro text-ink-mute md:hidden" {@attach reveal}>
      <Icon name="arrowRight" class="size-3.5 shrink-0" stroke={2} />
      {translate('compare.hint')}
    </p>
    <div class="glass mt-4 rv-rise overflow-hidden rounded-card md:mt-12" {@attach reveal}>
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <div
        class="overflow-x-auto outline-2 outline-offset-[-2px] outline-transparent transition duration-300 ease-silk focus-visible:outline-offset-[-2px]"
        role="region"
        aria-labelledby="compare-title"
        tabindex="0"
      >
        <table class="w-full min-w-[45.5rem] table-fixed border-collapse text-left md:min-w-0">
          <caption class="sr-only">{translate('compare.caption')}</caption>
          <colgroup>
            <col class="w-[12rem] md:w-[38%]" />
            <col class="w-[7.5rem] md:w-[14%]" />
            {#each PRODUCTS as product (product)}
              <col class="w-[6.5rem] md:w-[12%]" />
            {/each}
          </colgroup>
          <thead>
            <tr class={['rv-fade border-b border-white/8', HEAD_FADE]}>
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
            {#each COMPARE_ROWS as row (row.label)}
              <tr class={['rv-fade border-b border-white/8 last:border-0', row.fade]}>
                <th scope="row" class={['px-4 py-3 text-ui font-normal text-balance text-ink md:px-6', STICKY]}>
                  {translate(row.label)}
                </th>
                {#each row.cells as cell, column (column)}
                  <td class={['px-4 py-3 text-center align-middle', column === 0 && NYLZO_COLUMN]}>
                    {#if cell.kind === 'text'}
                      <span class={['text-micro tabular-nums', cell.tone]}>{cell.note ? translate(cell.note) : ''}</span
                      >
                    {:else}
                      {const mark = $derived(MARKS[cell.kind])}
                      <span class={['flex flex-col items-center gap-1 text-micro', mark.tone]}>
                        <span
                          class={[
                            'grid size-5 shrink-0 rv-pop place-items-center rounded-full [--rv-time:0.55s]',
                            mark.mark,
                            cell.delay,
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
