<script lang="ts">
  import { fileExtension, splitFileName } from '../../../shared/files/file-name.ts';
  import { formatSize } from '../../../shared/language/format.ts';
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Button from '../../../shared/ui/button/Button.svelte';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import SwapIcon from '../../../shared/ui/icon/SwapIcon.svelte';
  import type { ReadPage } from '../read-page.svelte.ts';
  import type { RevealedFile } from '../reader.ts';

  interface Props {
    readonly page: ReadPage;
    readonly file: RevealedFile;
  }

  const { page, file }: Props = $props();
  const translator = getTranslator();
  const { translate } = translator;
  const id = $props.id();

  const extension = $derived(fileExtension(file.name));
  const kind = $derived(extension.toUpperCase());
  const parts = $derived(splitFileName(file.name));
  const size = $derived(formatSize(file.bytes.length, translator.language));
</script>

<section aria-labelledby="{id}-title" class="flex flex-col gap-2.5">
  <h4 id="{id}-title" class="text-eyebrow text-ink-mute uppercase">{translate('revealed.file')}</h4>
  <div
    class="overflow-hidden rounded-pane bg-[linear-gradient(135deg,oklch(0.72_0.14_305/0.08),oklch(1_0_0/0.02)_55%)] shadow-rim ring-1 ring-nebula-300/20"
  >
    <div class="stack shrink-0 p-3 sm:p-3.5">
      <div
        class={[
          'flex items-center gap-3.5 fade-swap',
          page.confirmingDownload ? 'pointer-events-none opacity-0' : 'opacity-100',
        ]}
        inert={page.confirmingDownload}
      >
        <span
          class="relative grid size-12 shrink-0 place-items-center rounded-pane text-white tile-current"
          aria-hidden="true"
        >
          <Icon name="file" class="size-5" />
          {#if kind}
            <span
              translate="no"
              class="absolute -bottom-1.5 left-1/2 inline-flex h-5 -translate-x-1/2 items-center rounded-xs bg-void-900 px-1.5 text-micro font-medium tracking-[0.06em] text-nebula-300 uppercase ring-1 ring-nebula-300/35"
              >{kind}</span
            >
          {/if}
        </span>
        <span class="min-w-0 flex-1">
          <bdi translate="no" class="flex min-w-0 text-title-sm text-ink"
            ><span class="truncate">{parts.stem}</span><span class="shrink-0">{parts.extension}</span></bdi
          >
          <span translate="no" class="mt-1 block text-caption text-ink-mute tabular-nums"
            >{kind ? `${kind} · ${size}` : size}</span
          >
        </span>
        <Button
          {@attach page.focus.attach('download')}
          variant="tint"
          disabled={page.confirmingDownload}
          onclick={(event: MouseEvent) => void page.askDownload(event)}
          class="max-sm:w-11 max-sm:px-0"
          aria-label={translate('open.download.file')}
        >
          <SwapIcon from="download" to="check" swapped={page.downloaded.shown} toTone="text-aegis-200" animated />
          <span class="max-sm:sr-only" aria-hidden="true">{translate('open.download')}</span>
        </Button>
      </div>
      <div
        role="alertdialog"
        tabindex="-1"
        aria-labelledby="{id}-confirm-title"
        aria-describedby="{id}-confirm"
        onkeydown={page.onDownloadKeydown}
        class={[
          'flex flex-col gap-2.5 fade-swap sm:flex-row sm:items-center sm:justify-between',
          page.confirmingDownload ? 'opacity-100' : 'pointer-events-none opacity-0',
        ]}
        inert={!page.confirmingDownload}
      >
        <div class="flex min-w-0 items-center gap-3.5">
          <span
            class="grid size-10 shrink-0 place-items-center rounded-control text-white tile-danger"
            aria-hidden="true"
          >
            <Icon name="alert" class="size-5" />
          </span>
          <span class="min-w-0">
            <span id="{id}-confirm-title" class="block text-ui text-ink">{translate('revealed.confirm.title')}</span>
            <span id="{id}-confirm" class="block text-micro text-ink-mute"
              >{#if extension}{translate('revealed.confirm.extension')}
                <bdi translate="no">.{extension}</bdi>{:else}{translate('revealed.confirm.none')}{/if}</span
            >
          </span>
        </div>
        <div
          class="flex shrink-0 flex-wrap gap-1.5 *:min-w-fit *:flex-1 max-sm:*:h-auto max-sm:*:min-h-11 max-sm:*:py-2 max-sm:*:text-center max-sm:*:whitespace-normal sm:flex-nowrap sm:*:flex-none"
        >
          <Button
            {@attach page.focus.attach('keepFile')}
            variant="ghost"
            onclick={(event: MouseEvent) => void page.cancelDownload(event)}
          >
            {translate('revealed.confirm.cancel')}
          </Button>
          <Button
            variant="danger"
            disabled={!page.confirmingDownload}
            aria-disabled={page.confirmingDownload && !page.downloadReady}
            onclick={(event: MouseEvent) => void page.confirmDownload(event, file)}
          >
            <Icon name="download" class="size-4 shrink-0" animated />
            {translate('revealed.confirm.download')}
          </Button>
        </div>
      </div>
    </div>
    <p
      class="flex items-center gap-2 border-t border-white/8 bg-white/3 px-3.5 py-2.5 text-caption text-pretty text-ink-mute"
    >
      <Icon name="shield" class="size-3.5 shrink-0" stroke={2} />
      <span>{translate('revealed.file.unchecked')}</span>
    </p>
  </div>
</section>
