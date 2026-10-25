<script lang="ts">
  import type { MessageKey } from '../../shared/language/czech.ts';
  import type { FocusTarget, ReadPage } from './read-page.svelte.ts';

  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import ReportForm from '../../shared/report/ReportForm.svelte';
  import Icon from '../../shared/ui/Icon.svelte';
  import Popover from '../../shared/ui/Popover.svelte';

  interface Props {
    readonly page: ReadPage;
  }

  interface DeletionButton {
    readonly element: Extract<FocusTarget, 'deleteMessage' | 'deleteRest'>;
    readonly label: MessageKey;
    readonly shown: boolean;
    readonly enabled: boolean;
  }

  const { page }: Props = $props();
  const { translate } = getTranslator();
  const id = $props.id();

  const ACTION_STATES = {
    on: 'opacity-100',
    dim: 'pointer-events-none opacity-40',
    off: 'pointer-events-none opacity-0',
  } as const;

  const reportDone = $derived(page.reportSent !== undefined);
  const reportLimited = $derived(page.reportSent === 'limit');

  const deletionButtons: readonly DeletionButton[] = $derived([
    {
      element: 'deleteMessage',
      label: 'read.delete',
      shown: page.ready !== undefined,
      enabled: page.ready !== undefined && !page.deleting,
    },
    {
      element: 'deleteRest',
      label: 'read.delete.rest',
      shown: page.revealed !== undefined && (page.revealed.burnable || page.restDeleted),
      enabled: (page.revealed?.burnable ?? false) && !page.deleting,
    },
  ]);
</script>

<div
  class="relative z-30 mx-auto mt-6 flex max-w-[860px] flex-col-reverse items-center gap-4 px-1 sm:flex-row sm:justify-between"
>
  <div class="stack text-xs">
    <p
      class={[
        'flex items-center justify-center gap-2 text-center text-ink-mute fade-swap',
        page.burnNote ? 'opacity-0' : 'opacity-100',
      ]}
    >
      <Icon name="shieldCheck" stroke={1.7} class="size-4 shrink-0 text-aegis-300" />
      {translate('compose.note')}
    </p>
    <p
      class={[
        'flex items-center justify-center gap-2 text-center fade-swap',
        page.burnFailed ? 'text-rose-300' : 'text-ink-soft',
        page.burnNote ? 'opacity-100' : 'opacity-0',
      ]}
    >
      <Icon name={page.burnFailed ? 'alert' : 'check'} stroke={1.7} class="size-4 shrink-0" />
      {page.burnNote}
    </p>
  </div>
  <div
    class={['flex items-center gap-1 transition-opacity duration-500', ACTION_STATES[page.actions]]}
    inert={page.actions !== 'on'}
  >
    <div class="stack justify-items-end">
      {#each deletionButtons as button (button.element)}
        <button
          {@attach page.focus.attach(button.element)}
          type="button"
          disabled={!button.enabled}
          onclick={page.askDelete}
          inert={!button.shown}
          class={[
            'flex h-11 items-center gap-2 rounded-[9px] px-3 text-[13px] whitespace-nowrap text-ink-mute transition-opacity duration-300 hover:bg-rose-500/10 hover:text-rose-200 disabled:hover:bg-transparent disabled:hover:text-ink-mute md:h-9',
            button.shown ? 'opacity-100' : 'pointer-events-none opacity-0',
          ]}
        >
          <Icon name="trash" class="size-4 shrink-0" />
          {translate(button.label)}
        </button>
      {/each}
    </div>
    <div
      class={[
        'relative transition-opacity duration-300',
        page.reportable ? 'opacity-100' : 'pointer-events-none opacity-0',
      ]}
      inert={!page.reportable}
      data-menu
    >
      <button
        {@attach page.focus.attach('reportButton')}
        type="button"
        aria-haspopup="dialog"
        aria-expanded={page.reporting}
        aria-controls="{id}-report"
        onclick={page.toggleReport}
        class={[
          'flex h-11 items-center gap-2 rounded-[9px] px-3 text-[13px] md:h-9',
          page.reporting ? 'bg-white/8 text-ink' : 'text-ink-mute hover:bg-white/5 hover:text-ink',
        ]}
      >
        <Icon name="flag" class="size-4 shrink-0" />
        {translate('open.report')}
      </button>
      <Popover
        id="{id}-report"
        attachment={page.focus.attach('reportPopover')}
        label={translate('open.report.title')}
        open={page.reporting}
        from="below"
        onclose={page.closeReport}
        class="top-full right-0 mt-2 w-80 max-w-[calc(100vw-2rem)] p-3.5 text-left"
      >
        <div class="stack">
          <div class={['fade-swap', reportDone ? 'pointer-events-none opacity-0' : 'opacity-100']} inert={reportDone}>
            <p class="px-0.5 text-[13px] text-ink">{translate('open.report.title')}</p>
            <p class="mt-1 px-0.5 text-[11px] leading-relaxed text-ink-mute">{translate('open.report.note')}</p>
            {#key page.reportRound}
              <ReportForm
                name="{id}-report-form"
                compact
                hint={translate('open.report.privacy')}
                onsubmit={page.report}
                onsent={page.reportFinished}
              />
            {/key}
          </div>
          <div
            class={[
              'flex flex-col items-center justify-center gap-2 px-2 text-center fade-swap',
              reportDone ? 'opacity-100' : 'pointer-events-none opacity-0',
            ]}
            inert={!reportDone}
          >
            <span
              class={[
                'grid size-10 place-items-center rounded-control ring-1',
                reportLimited
                  ? 'bg-rose-500/12 text-rose-300 ring-rose-300/35'
                  : 'bg-aegis-500/15 text-aegis-200 ring-aegis-400/35',
              ]}
              aria-hidden="true"
            >
              <Icon name={reportLimited ? 'alert' : 'check'} stroke={2} class="size-5" />
            </span>
            <p class="text-[13px] text-ink">
              {translate(reportLimited ? 'open.report.limit' : 'open.report.sent')}
            </p>
            <p class="text-[11px] leading-relaxed text-ink-mute">
              {translate(reportLimited ? 'open.report.limit.note' : 'open.report.sent.note')}
            </p>
          </div>
        </div>
      </Popover>
    </div>
  </div>
</div>
