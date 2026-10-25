<script lang="ts">
  import type { ChipData, ReadPage } from './read-page.svelte.ts';

  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import WorkingStatus from '../../shared/pipeline/WorkingStatus.svelte';
  import Chip from '../../shared/ui/Chip.svelte';
  import Icon from '../../shared/ui/Icon.svelte';

  interface Props {
    readonly page: ReadPage;
    readonly formId: string;
  }

  const { page, formId }: Props = $props();
  const { translate } = getTranslator();
  const id = $props.id();

  const readyBarShown = $derived((page.phase === 'ready' || page.phase === 'opening') && !page.askingDelete);
  const afterBarShown = $derived((page.phase === 'revealed' || page.phase === 'notice') && !page.askingDelete);
  const retryable = $derived(page.shown.view === 'failed' || page.shown.view === 'retry_later');
  const progress = $derived(page.timeline.paced.progress);
</script>

{#snippet chips(list: readonly ChipData[], dim = false)}
  <ul
    class={[
      'flex flex-wrap items-center gap-1.5 transition-opacity duration-500',
      dim ? 'pointer-events-none opacity-40' : 'opacity-100',
    ]}
  >
    {#each list as data (data.text)}
      <Chip icon={data.icon} accent={data.accent ?? false}>{data.text}</Chip>
    {/each}
  </ul>
{/snippet}

<div class="relative border-t border-white/8">
  <div class="stack">
    <div
      {@attach page.focus.attach('bar')}
      class={[
        'flex flex-col gap-2 p-2.5 fade-swap sm:flex-row sm:items-center sm:justify-between sm:gap-3',
        readyBarShown ? 'opacity-100' : 'pointer-events-none opacity-0',
      ]}
      inert={!readyBarShown}
    >
      {@render chips(page.readyChips, page.phase === 'opening')}
      <div class="flex items-center">
        <div class="stack min-w-0 flex-1 sm:flex-none">
          <button
            type="submit"
            form={formId}
            disabled={!page.ready || page.deleting || page.phase !== 'ready'}
            class={[
              'btn-primary flex h-11 w-full hover-lift items-center justify-center gap-2 rounded-[9px] px-5 text-sm font-medium whitespace-nowrap transition-[opacity,translate,scale] duration-500 md:h-9 md:px-4',
              page.phase === 'ready' ? 'opacity-100' : 'pointer-events-none opacity-0',
            ]}
          >
            <span class="sheen" aria-hidden="true"></span>
            <Icon name="eye" stroke={2} class="size-4 shrink-0" />
            {translate('read.reveal')}
          </button>
          <WorkingStatus
            label={translate('open.opening')}
            {progress}
            fill={page.phase === 'opening' ? progress : 0}
            shown={page.phase === 'opening'}
          />
        </div>
      </div>
    </div>

    <div
      class={[
        'flex flex-col gap-2 p-2.5 fade-swap sm:flex-row sm:items-center sm:justify-between sm:gap-3',
        afterBarShown ? 'opacity-100' : 'pointer-events-none opacity-0',
      ]}
      inert={!afterBarShown}
    >
      {@render chips(page.afterChips)}
      {#if retryable}
        <button
          type="button"
          onclick={page.retry}
          class="btn-primary flex h-11 w-full hover-lift items-center justify-center gap-2 rounded-[9px] px-4 text-sm font-medium whitespace-nowrap sm:w-auto md:h-9"
        >
          <Icon name="refresh" class="size-4 shrink-0" />
          {translate('action.retry')}
        </button>
      {:else}
        <a
          href="/"
          class="btn-ghost flex h-11 w-full hover-lift items-center justify-center gap-2 rounded-[9px] px-4 text-[13px] whitespace-nowrap sm:w-auto md:h-9"
        >
          <Icon name="pen" class="size-4 shrink-0" />
          {translate('nav.write')}
        </a>
      {/if}
    </div>

    <div
      role="alertdialog"
      tabindex="-1"
      aria-labelledby="{id}-confirm"
      onkeydown={page.onConfirmKeydown}
      class={[
        'flex flex-col gap-2 p-2.5 fade-swap sm:flex-row sm:items-center sm:justify-between sm:gap-3',
        page.askingDelete ? 'opacity-100' : 'pointer-events-none opacity-0',
      ]}
      inert={!page.askingDelete}
    >
      <p id="{id}-confirm" class="flex items-center gap-2 px-1.5 text-[13px] text-ink-soft">
        <Icon name="trash" class="size-4 shrink-0 text-rose-300" />
        {translate('read.delete.confirm')}
      </p>
      <div class="grid grid-cols-2 gap-1.5 sm:flex sm:items-center">
        <button
          {@attach page.focus.attach('keepMessage')}
          type="button"
          onclick={page.cancelDelete}
          class="btn-ghost flex h-11 hover-lift items-center justify-center rounded-[9px] px-4 text-[13px] whitespace-nowrap md:h-9"
        >
          {translate('read.delete.no')}
        </button>
        <button
          type="button"
          disabled={!page.askingDelete}
          onclick={page.burn}
          class="flex h-11 hover-lift items-center justify-center gap-2 rounded-[9px] bg-rose-500/20 px-4 text-[13px] font-medium whitespace-nowrap text-rose-100 ring-1 ring-rose-300/45 hover:bg-rose-500/30 md:h-9"
        >
          {translate('read.delete.yes')}
        </button>
      </div>
    </div>
  </div>
</div>
