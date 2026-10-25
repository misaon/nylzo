<script lang="ts">
  import type { ReadPage } from './read-page.svelte.ts';

  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import Icon from '../../shared/ui/Icon.svelte';
  import SwapIcon from '../../shared/ui/SwapIcon.svelte';
  import { PANEL_HIDDEN, PANEL_SHOWN } from './read-page.svelte.ts';
  import RevealedFile from './RevealedFile.svelte';

  interface Props {
    readonly page: ReadPage;
  }

  const { page }: Props = $props();
  const { translate } = getTranslator();
</script>

<div
  class={[
    'absolute inset-0 flex flex-col justify-center gap-5 px-5 py-6 text-left fade-swap sm:px-8 sm:py-8',
    page.phase === 'revealed' ? PANEL_SHOWN : PANEL_HIDDEN,
  ]}
  inert={page.phase !== 'revealed'}
>
  {#if page.revealed}
    <div class="flex items-center justify-between gap-3">
      <div class="flex min-w-0 items-center gap-3">
        <span
          class="grid size-10 shrink-0 place-items-center rounded-control bg-aegis-500/15 text-aegis-200 shadow-[0_0_28px_-6px_oklch(0.56_0.19_258/0.75)] ring-1 ring-aegis-400/35"
          aria-hidden="true"
        >
          <Icon name="unlock" stroke={1.7} class="size-5" />
        </span>
        <div class="min-w-0">
          <h3 {@attach page.focus.attach('revealedTitle')} tabindex="-1" class="text-[15px] text-ink outline-none">
            {translate('revealed.title')}
          </h3>
          <p class="truncate text-[13px] text-ink-mute">{translate('open.revealed.sub')}</p>
        </div>
      </div>
      {#if page.revealed.text !== undefined}
        <button
          type="button"
          onclick={page.copyText}
          aria-label={translate('open.copy.text')}
          title={translate('open.copy.text')}
          class="relative grid size-11 shrink-0 place-items-center rounded-[9px] bg-white/4 text-ink-soft ring-1 ring-white/10 hover:bg-white/8 hover:text-ink md:size-9"
        >
          <span
            class={[
              'absolute inset-0 rounded-[9px] bg-aegis-500/20 shadow-[0_0_18px_-4px_oklch(0.56_0.19_258/0.8)] ring-1 ring-aegis-400/50 transition-opacity duration-500 ease-silk',
              page.copied.shown ? 'opacity-100' : 'opacity-0',
            ]}
          ></span>
          <span class="relative">
            <SwapIcon from="copy" to="check" swapped={page.copied.shown} toStroke={2.2} toTone="text-aegis-200" />
          </span>
        </button>
        <span class="sr-only" role="status">{page.copied.shown ? translate('open.copied.text') : ''}</span>
      {/if}
    </div>
    {#if page.revealText !== undefined}
      <pre
        class="max-h-[220px] min-h-0 [scrollbar-width:thin] overflow-auto rounded-pane px-5 py-4 font-sans text-[15px] leading-[1.75] break-words whitespace-pre-wrap text-ink well">{page.revealText}</pre>
    {/if}
    {#if page.revealed.file}
      <RevealedFile {page} file={page.revealed.file} />
    {/if}
    <p class="flex shrink-0 items-center gap-2 px-1 text-[13px] text-ink-mute">
      <Icon name="eyeOff" class="size-4 shrink-0 text-nebula-300" />
      {translate('revealed.note')}
    </p>
  {/if}
</div>
