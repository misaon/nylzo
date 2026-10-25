<script lang="ts">
  import { getI18n, type MessageKey } from '../../lib/i18n.svelte.ts';
  import Icon from '../ui/Icon.svelte';

  const { t } = getI18n();

  type Cell =
    | { readonly kind: 'yes' | 'no' | 'part'; readonly note?: MessageKey; readonly score: number }
    | { readonly kind: 'text'; readonly note: MessageKey; readonly score: number; readonly tone: string };

  const yes = (note?: MessageKey): Cell => ({ kind: 'yes', score: 2, ...(note ? { note } : {}) });
  const part = (note?: MessageKey): Cell => ({ kind: 'part', score: 1, ...(note ? { note } : {}) });
  const no = (note?: MessageKey): Cell => ({ kind: 'no', score: 0, ...(note ? { note } : {}) });
  const bits = (score: number, note: MessageKey, tone = 'text-ink-soft'): Cell => ({ kind: 'text', score, note, tone });

  const services = ['PrivateBin', 'Yopass', 'OneTimeSecret', 'Bitwarden Send'] as const;
  const rows: readonly { readonly label: MessageKey; readonly cells: readonly Cell[] }[] = [
    { label: 'compare.row.client', cells: [yes(), yes(), yes(), no('compare.note.server_encrypts'), yes()] },
    {
      label: 'compare.row.preview',
      cells: [
        yes(),
        yes('compare.note.since'),
        yes('compare.note.default'),
        yes('compare.note.click'),
        part('compare.note.nojs'),
      ],
    },
    {
      label: 'compare.row.passphrase',
      cells: [
        yes(),
        yes(),
        yes('compare.note.replaces'),
        no('compare.note.server_check'),
        no('compare.note.server_check'),
      ],
    },
    {
      label: 'compare.row.secret',
      cells: [
        bits(256, 'compare.note.bits256', 'text-aegis-200'),
        bits(256, 'compare.note.bits256'),
        bits(131, 'compare.note.bits131'),
        { kind: 'text', score: 0, note: 'compare.note.dash', tone: 'text-ink-mute' },
        bits(128, 'compare.note.bits128'),
      ],
    },
    {
      label: 'compare.row.verify',
      cells: [yes('compare.note.verify'), no('compare.note.signed_releases'), no(), no(), no()],
    },
    {
      label: 'compare.row.length',
      cells: [yes('compare.note.blocks'), no('compare.note.compression'), no(), no('compare.note.server_sees'), no()],
    },
    {
      label: 'compare.row.id',
      cells: [
        yes('compare.note.signed'),
        no('compare.note.id_enough'),
        no('compare.note.id_enough'),
        no('compare.note.id_enough'),
        no('compare.note.id_enough'),
      ],
    },
    { label: 'compare.row.stolen', cells: [yes('compare.note.every_change'), no(), no(), no(), no()] },
    {
      label: 'compare.row.ip',
      cells: [
        yes('compare.note.memory'),
        part('compare.note.hmac'),
        no('compare.note.request_log'),
        part('compare.note.truncated'),
        no('compare.note.collects'),
      ],
    },
    { label: 'compare.row.audit', cells: [no('compare.note.not_yet'), no(), no(), no(), no('compare.note.send_out')] },
  ];

  function sole(cells: readonly Cell[]): boolean {
    const [ours, ...others] = cells;
    return ours !== undefined && others.every((cell) => cell.score < ours.score);
  }
</script>

<section id="srovnani" class="px-4 py-28 sm:px-6 md:py-40" aria-labelledby="compare-title">
  <div class="mx-auto max-w-[1160px]">
    <div class="flex reveal-up flex-col gap-4 md:flex-row md:items-end md:justify-between">
      <div class="max-w-[600px]">
        <p class="text-sm font-medium text-aegis-300">{t('compare.eyebrow')}</p>
        <h2
          id="compare-title"
          class="mt-3 font-display text-[28px] leading-[1.2] font-light tracking-[-0.02em] text-balance md:text-[40px]"
        >
          {t('compare.title')}
        </h2>
      </div>
      <p class="max-w-[320px] text-xs leading-relaxed text-pretty text-ink-mute">{t('compare.source')}</p>
    </div>
    <div class="glass mt-12 reveal-tilt overflow-hidden rounded-card">
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <div
        class="overflow-x-auto focus-visible:outline-offset-[-2px]"
        role="region"
        aria-labelledby="compare-title"
        tabindex="0"
      >
        <table class="w-full min-w-[820px] border-collapse text-left">
          <caption class="sr-only">{t('compare.caption')}</caption>
          <thead>
            <tr class="border-b border-white/8">
              <th scope="col" class="w-[27%] px-6 py-4 text-xs font-normal text-ink-mute">{t('compare.feature')}</th>
              <th
                scope="col"
                class="bg-aegis-500/12 px-5 py-4 font-display text-[15px] font-normal text-aegis-200 shadow-[inset_1px_0_0_oklch(0.67_0.16_252/0.3),inset_-1px_0_0_oklch(0.67_0.16_252/0.3),inset_0_2px_0_oklch(0.77_0.115_249/0.7)]"
              >
                Nylzo
              </th>
              {#each services as service (service)}
                <th scope="col" class="px-5 py-4 text-sm font-medium text-ink-soft">{service}</th>
              {/each}
            </tr>
          </thead>
          <tbody>
            {#each rows as row (row.label)}
              {@const only = sole(row.cells)}
              <tr class="border-b border-white/6 last:border-0">
                <th scope="row" class="px-6 py-3.5 text-sm font-normal text-ink">{t(row.label)}</th>
                {#each row.cells as cell, column (column)}
                  <td
                    class={[
                      'px-5 py-3.5 align-middle',
                      column === 0 &&
                        'bg-aegis-500/8 shadow-[inset_1px_0_0_oklch(0.67_0.16_252/0.3),inset_-1px_0_0_oklch(0.67_0.16_252/0.3)]',
                    ]}
                  >
                    {#if cell.kind === 'yes'}
                      <span class="flex items-center gap-2 text-xs text-aegis-200">
                        <span
                          class={[
                            'grid size-5 shrink-0 place-items-center rounded-[6px] ring-1',
                            column === 0 && only
                              ? 'bg-aegis-400 text-void-950 shadow-[0_0_12px_oklch(0.67_0.16_252/0.7)] ring-aegis-200/70'
                              : 'bg-aegis-500/20 ring-aegis-400/45',
                          ]}
                        >
                          <Icon name="check" stroke={2.6} class="size-3" />
                        </span>
                        <span class="sr-only">{t('compare.yes')}</span>{cell.note ? t(cell.note) : ''}
                      </span>
                    {:else if cell.kind === 'no'}
                      <span class="flex items-center gap-2 text-xs text-ink-mute">
                        <span class="grid size-5 shrink-0 place-items-center rounded-[6px] ring-1 ring-white/15"
                          ><Icon name="close" stroke={2.6} class="size-2.5" /></span
                        >
                        <span class="sr-only">{t('compare.no')}</span>{cell.note ? t(cell.note) : ''}
                      </span>
                    {:else if cell.kind === 'part'}
                      <span class="flex items-center gap-2 text-xs text-ink-soft">
                        <span class="grid size-5 shrink-0 place-items-center rounded-[6px] ring-1 ring-white/25"
                          ><Icon name="minus" stroke={2.6} class="size-3" /></span
                        >
                        <span class="sr-only">{t('compare.part')}</span>{cell.note ? t(cell.note) : ''}
                      </span>
                    {:else if cell.kind === 'text'}
                      <span class={['font-mono text-xs', cell.tone]}>{t(cell.note)}</span>
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
