<script lang="ts">
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Button from '../../../shared/ui/button/Button.svelte';
  import ConfirmDialog from '../../../shared/ui/dialog/ConfirmDialog.svelte';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import SwapIcon from '../../../shared/ui/icon/SwapIcon.svelte';
  import Tooltip from '../../../shared/ui/tooltip/Tooltip.svelte';
  import type { ReadPage } from '../read-page.svelte.ts';
  import RevealedFile from './RevealedFile.svelte';

  interface Props {
    readonly page: ReadPage;
  }

  const { page }: Props = $props();
  const { translate } = getTranslator();
  const id = $props.id();

  const FRAME_LINE =
    'bg-[linear-gradient(135deg,var(--color-aegis-200),var(--color-aegis-300)_45%,var(--color-nebula-300))] [mask-image:linear-gradient(#000_0_0),linear-gradient(#000_0_0)] [mask-clip:content-box,border-box] [mask-composite:exclude]';

  const shown = $derived(page.phase === 'revealed');
  const hiddenCount = $derived(page.marked?.hidden ?? 0);
  const settled = $derived(page.marked !== undefined && page.revealText === page.marked.shown);
  const lastOpen = $derived(page.revealed !== undefined && !page.revealed.burnable && !page.restDeleted);
  const historyNoted = $derived(page.linkInHistory && ((page.revealed?.burnable ?? false) || page.restDeleted));
</script>

<div
  class={[
    'flex flex-col justify-center gap-10 px-5 pt-6 pb-12 text-left fade-swap max-sm:pb-8 sm:gap-12 sm:px-8 sm:pt-7 sm:pb-14',
    shown ? 'relative translate-y-0 opacity-100' : 'pointer-events-none absolute inset-0 translate-y-3 opacity-0',
  ]}
  inert={!shown}
>
  {#if page.revealed}
    <div class="flex items-center justify-between gap-3">
      <div class="flex min-w-0 items-center gap-3">
        <span class="grid size-10 shrink-0 place-items-center rounded-full text-white tile-current" aria-hidden="true">
          <Icon name="unlock" class="size-5" />
        </span>
        <div class="min-w-0">
          <h3 {@attach page.focus.attach('revealedTitle')} tabindex="-1" class="text-title-sm text-ink outline-none">
            {translate('revealed.title')}
          </h3>
          <p class="truncate text-caption text-ink-mute">{translate('open.revealed.sub')}</p>
        </div>
      </div>
      {#if page.revealed.text !== undefined}
        <Button
          variant="tint"
          onclick={page.askCopy}
          aria-label={translate('open.copy.text')}
          class="max-sm:w-11 max-sm:px-0"
        >
          <span
            class={[
              'absolute inset-0 rounded-control bg-aegis-500/20 shadow-[0_0_24px_-6px_oklch(0.56_0.19_258/0.7)] ring-1 ring-aegis-300/50 transition-opacity duration-500',
              page.copied.shown ? 'opacity-100' : 'opacity-0',
            ]}
          ></span>
          <SwapIcon class="relative" from="copy" to="check" swapped={page.copied.shown} toTone="text-aegis-200" />
          <span class="relative hidden sm:inline">{translate('open.copy.text')}</span>
        </Button>
        <span class="sr-only" role="status">{page.copied.shown ? translate('open.copied.text') : ''}</span>
      {/if}
      {#if page.confirmingCopy}
        <ConfirmDialog
          id="{id}-copy"
          title={translate('revealed.hidden.confirm')}
          tone="warning"
          icon="alert"
          confirmLabel={translate('revealed.hidden.copy')}
          dismissLabel={translate('revealed.hidden.keep')}
          onconfirm={page.copyText}
          onclose={page.cancelCopy}
        />
      {/if}
    </div>

    <div class="flex flex-col gap-8 sm:gap-10">
      <div class="flex flex-col gap-3">
        {#if page.revealText !== undefined}
          <div class="relative">
            <span class="pointer-events-none absolute -inset-1 opacity-20 blur-[6px]" aria-hidden="true">
              <span class={['absolute inset-1 rounded-pane p-[2px]', FRAME_LINE]}></span>
            </span>
            <pre
              translate="no"
              class="max-h-[320px] min-h-[136px] overflow-auto rounded-pane bg-[linear-gradient(180deg,oklch(1_0_0/0.06),oklch(1_0_0/0.025))] px-5 py-4 font-sans text-lead break-words whitespace-pre-wrap text-ink shadow-[inset_0_1px_0_oklch(1_0_0/0.06),0_18px_40px_-18px_oklch(0.05_0.03_275/0.9)] sm:min-h-[160px] sm:px-6 sm:py-5 md:max-h-[360px] md:text-lead-lg">{#if settled && page.marked}{#each page.marked.pieces as piece (piece.key)}{#if piece.hidden}<mark
                      class="mx-px rounded-[4px] bg-rose-500/15 px-1 text-micro text-rose-200 ring-1 ring-rose-300/40"
                      >{piece.text}</mark
                    >{:else}{piece.text}{/if}{/each}{:else}{page.revealText}{/if}</pre>
            <span
              class={['pointer-events-none absolute inset-0 rounded-pane p-px opacity-45', FRAME_LINE]}
              aria-hidden="true"
            ></span>
          </div>
        {/if}
        <div class="flex flex-wrap items-center gap-x-5 gap-y-1.5 text-caption text-ink-mute">
          {#if hiddenCount > 0}
            <span role="alert" class="inline-flex items-center gap-1.5 text-rose-300">
              <Icon name="alert" class="size-3.5 shrink-0" stroke={2} />
              {translate('revealed.hidden', { count: hiddenCount })}
            </span>
          {/if}
          {#if lastOpen}
            <span class="inline-flex items-center gap-1.5">
              <Icon name="clock" class="size-3.5 shrink-0 text-[oklch(0.82_0.12_80)]" stroke={2} />
              {translate('revealed.note')}
            </span>
          {/if}
          {#if historyNoted}
            <span
              class={[
                'inline-flex items-center gap-1.5 transition-opacity duration-500',
                page.restDeleted && 'opacity-0',
              ]}
              aria-hidden={page.restDeleted}
            >
              <Icon name="alert" class="size-3.5 shrink-0 text-[oklch(0.82_0.12_80)]" stroke={2} />
              {translate('revealed.history')}
            </span>
          {/if}
          <Tooltip
            label={translate('revealed.sender')}
            title={translate('revealed.sender.title')}
            text={translate('revealed.sender.text')}
          />
        </div>
        <div
          class={[
            'text-caption text-ink-mute transition-[opacity,height,margin] [interpolate-size:allow-keywords]',
            page.clipboard === 'untouched' ? '-mt-3 h-0 overflow-y-clip opacity-0' : 'mt-0 h-auto opacity-100',
          ]}
          inert={page.clipboard === 'untouched'}
        >
          <p role="status" class="flex flex-wrap items-center gap-x-3 gap-y-1 px-1">
            {translate(page.clipboard === 'cleared' ? 'clipboard.cleared' : 'clipboard.copied')}
            <Button
              variant="ghost"
              size="compact"
              onclick={page.clearClipboard}
              disabled={page.clipboard !== 'copied'}
              class={page.clipboard === 'copied' ? 'opacity-100' : 'opacity-0'}
            >
              {translate('clipboard.clear')}
            </Button>
          </p>
        </div>
      </div>
      {#if page.revealed.file}
        <RevealedFile {page} file={page.revealed.file} />
      {/if}
    </div>
  {/if}
</div>
