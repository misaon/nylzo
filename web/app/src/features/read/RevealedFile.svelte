<script lang="ts">
  import type { ReadPage } from './read-page.svelte.ts';
  import type { RevealedFile } from './reader.ts';

  import { fileExtension, splitFileName } from '../../shared/files/file-name.ts';
  import { formatBytes } from '../../shared/language/format.ts';
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import Icon from '../../shared/ui/Icon.svelte';
  import SwapIcon from '../../shared/ui/SwapIcon.svelte';

  interface Props {
    readonly page: ReadPage;
    readonly file: RevealedFile;
  }

  const { page, file }: Props = $props();
  const translator = getTranslator();
  const { translate } = translator;

  const extension = $derived(fileExtension(file.name));
  const parts = $derived(splitFileName(file.name));
</script>

<div class="stack shrink-0 rounded-pane bg-white/3 py-2.5 pr-2.5 pl-3 ring-1 ring-white/8">
  <div
    class={[
      'flex items-center gap-3.5 fade-swap',
      page.confirmingDownload ? 'pointer-events-none opacity-0' : 'opacity-100',
    ]}
    inert={page.confirmingDownload}
  >
    <span
      class="grid size-9 shrink-0 place-items-center rounded-[9px] bg-nebula-400/10 text-nebula-300 ring-1 ring-nebula-300/30"
      aria-hidden="true"
    >
      <Icon name="file" stroke={1.7} class="size-[18px]" />
    </span>
    <span class="min-w-0 flex-1">
      <bdi class="flex min-w-0 text-sm text-ink"
        ><span class="truncate">{parts.stem}</span><span class="shrink-0">{parts.extension}</span></bdi
      >
      <span class="block font-mono text-[11px] text-ink-mute"
        >{formatBytes(file.bytes.length, translator.language)}</span
      >
    </span>
    <button
      {@attach page.focus.attach('download')}
      type="button"
      disabled={page.confirmingDownload}
      onclick={(event) => page.askDownload(event, file)}
      class="btn-ghost flex h-11 shrink-0 hover-lift items-center gap-2 rounded-[9px] px-3 text-[13px] md:h-9"
    >
      <SwapIcon from="download" to="check" swapped={page.downloaded.shown} toStroke={2.2} toTone="text-aegis-200" />
      {translate('open.download')}<span class="sr-only">{translate('open.download.suffix')}</span>
    </button>
  </div>
  <div
    class={[
      'flex flex-col gap-2.5 fade-swap sm:flex-row sm:items-center sm:justify-between',
      page.confirmingDownload ? 'opacity-100' : 'pointer-events-none opacity-0',
    ]}
    inert={!page.confirmingDownload}
  >
    <div class="flex min-w-0 items-center gap-3.5">
      <span
        class="grid size-9 shrink-0 place-items-center rounded-[9px] bg-rose-500/12 text-rose-300 ring-1 ring-rose-300/35"
        aria-hidden="true"
      >
        <Icon name="alert" stroke={1.7} class="size-[18px]" />
      </span>
      <span class="min-w-0">
        <span role="alert" class="block text-sm text-ink"
          >{page.confirmingDownload ? translate('revealed.confirm.title') : ''}</span
        >
        <span class="block text-[11px] text-ink-mute"
          >{extension
            ? translate('revealed.confirm.extension', { extension })
            : translate('revealed.confirm.none')}</span
        >
      </span>
    </div>
    <div class="grid shrink-0 grid-cols-2 gap-1.5 sm:flex">
      <button
        type="button"
        disabled={!page.confirmingDownload}
        onclick={(event) => page.confirmDownload(event, file)}
        class="h-11 rounded-[9px] bg-rose-500/20 px-3 text-[13px] font-medium text-rose-100 ring-1 ring-rose-300/45 hover:bg-rose-500/30 md:h-9"
      >
        {translate('revealed.confirm.download')}
      </button>
      <button
        {@attach page.focus.attach('keepFile')}
        type="button"
        onclick={page.cancelDownload}
        class="btn-ghost h-11 rounded-[9px] px-3 text-[13px] md:h-9"
      >
        {translate('revealed.confirm.cancel')}
      </button>
    </div>
  </div>
</div>
