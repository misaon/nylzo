<script lang="ts">
  import type { Composer } from './composer.svelte.ts';

  import { formatBytes, formatSize } from '../../../shared/language/format.ts';
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Pipeline from '../../../shared/pipeline/Pipeline.svelte';
  import Icon from '../../../shared/ui/Icon.svelte';
  import { PRIVATE_TEXT } from '../../../shared/ui/private-fields.ts';

  interface Props {
    readonly composer: Composer;
    readonly noticeId: string;
  }

  const { composer, noticeId }: Props = $props();
  const translator = getTranslator();
  const { translate } = translator;
  const id = $props.id();
</script>

<div
  class="group relative"
  role="presentation"
  ondragover={composer.onDragOver}
  ondragenter={composer.onDragOver}
  ondragleave={composer.onDragLeave}
  ondrop={composer.onDrop}
>
  <label for="{id}-text" class="sr-only">{translate('create.text')}</label>
  <textarea
    id="{id}-text"
    {@attach composer.focus.attach('text')}
    bind:value={composer.text}
    rows="7"
    {...PRIVATE_TEXT}
    readonly={composer.busy}
    placeholder={translate('compose.placeholder')}
    aria-invalid={composer.problemOf.content !== undefined || composer.problemOf.text !== undefined}
    aria-describedby={noticeId}
    class={[
      'block min-h-[200px] w-full resize-none rounded-t-card bg-transparent px-6 pt-6 pb-14 text-base leading-[1.7] text-ink caret-aegis-300 transition-opacity duration-500 outline-none placeholder:text-ink-mute md:min-h-[280px] md:text-[17px]',
      composer.phase === 'encrypting' ? 'opacity-0' : 'opacity-100',
    ]}></textarea>
  <div class="pointer-events-none absolute inset-x-4 bottom-3 flex items-center justify-between gap-3">
    <span
      class={[
        'flex h-8 min-w-0 origin-left items-center gap-2 rounded-[8px] pr-0.5 pl-2.5 text-[13px] ring-1 transition-[opacity,translate,scale] duration-500 ease-silk',
        composer.problemOf.file ? 'bg-rose-500/10 ring-rose-300/50' : 'bg-nebula-400/10 ring-nebula-300/30',
        composer.file && composer.phase === 'compose'
          ? 'pointer-events-auto translate-y-0 scale-100 opacity-100'
          : 'translate-y-1 scale-95 opacity-0',
      ]}
      inert={!composer.file || composer.phase !== 'compose'}
    >
      <Icon name="file" class="size-4 shrink-0 text-nebula-300" />
      <span translate="no" class="truncate text-ink">{composer.file?.name ?? ''}</span>
      <span class="shrink-0 font-mono text-[11px] text-ink-mute"
        >{composer.file ? formatBytes(composer.file.size, translator.language) : ''}</span
      >
      <button
        type="button"
        onclick={composer.removeFile}
        aria-label={translate('create.file.clear')}
        class="grid size-7 shrink-0 place-items-center rounded-[6px] text-ink-soft hover:bg-white/8 hover:text-ink max-md:-my-1.5 max-md:size-11"
      >
        <Icon name="close" class="size-3.5" stroke={2} />
      </button>
    </span>
    <span
      class={[
        'shrink-0 pr-2 font-mono text-[11px] tabular-nums transition-opacity duration-500',
        composer.problemOf.text ? 'text-rose-300' : 'text-ink-mute',
        composer.phase === 'compose' ? 'opacity-100' : 'opacity-0',
      ]}
      aria-hidden="true"
    >
      {translate('compose.counter', {
        size: formatSize(composer.textBytes, translator.language),
        max: formatBytes(composer.textLimit, translator.language),
      })}
    </span>
  </div>

  <div
    class={[
      'pointer-events-none absolute inset-0 overflow-hidden rounded-t-card transition-opacity duration-500',
      composer.phase === 'encrypting' ? 'opacity-100' : 'opacity-0',
    ]}
    aria-hidden="true"
  >
    <p
      class="absolute inset-0 [mask-image:radial-gradient(ellipse_62%_58%_at_50%_50%,transparent_35%,black_100%)] px-6 pt-6 font-mono text-[13px] leading-6 break-all text-aegis-200/35"
    >
      {composer.cipher}
    </p>
    <div
      class="absolute inset-x-0 top-0 h-28 animate-scan bg-linear-to-b from-transparent via-aegis-400/12 to-transparent"
    ></div>
    <div
      class="absolute inset-0 bg-[radial-gradient(ellipse_62%_62%_at_50%_50%,oklch(0.13_0.04_270/0.92),transparent_88%)]"
    ></div>
    <div class="absolute inset-0 flex flex-col items-center justify-center px-4 pb-4">
      <Pipeline
        nodes={composer.nodes}
        current={composer.timeline.paced.step}
        progress={composer.timeline.paced.progress}
        active={composer.phase === 'encrypting'}
      />
    </div>
  </div>

  <div
    class="pointer-events-none absolute inset-0 rounded-t-card opacity-0 shadow-[inset_0_0_0_2px_oklch(0.77_0.115_249/0.7)] transition-opacity duration-500 group-focus-within:opacity-100"
    aria-hidden="true"
  ></div>
  <div
    class={[
      'pointer-events-none absolute inset-2 grid place-items-center rounded-pane border border-dashed border-aegis-300/60 bg-void-950/80 transition-opacity duration-300',
      composer.dragging ? 'opacity-100' : 'opacity-0',
    ]}
    aria-hidden="true"
  >
    <p class="flex items-center gap-3 px-4 text-center text-aegis-200">
      <span class="grid size-9 shrink-0 place-items-center rounded-control bg-aegis-500/15 ring-1 ring-aegis-400/35">
        <Icon name="upload" class="size-[18px]" />
      </span>
      {translate('compose.drop')}
    </p>
  </div>
</div>
