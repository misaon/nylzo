<script lang="ts">
  import { formatBytes, formatSize } from '../../../shared/language/format.ts';
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import PipelineRing from '../../../shared/pipeline/PipelineRing.svelte';
  import { PRIVATE_TEXT } from '../../../shared/ui/field/private-fields.ts';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import { lingering } from '../../../shared/ui/transition/lingering.ts';
  import type { Composer } from './composer.svelte.ts';

  interface Props {
    readonly composer: Composer;
    readonly noticeId: string;
  }

  const { composer, noticeId }: Props = $props();
  const translator = getTranslator();
  const { translate } = translator;
  const id = $props.id();
  const COUNTER_FROM = 0.8;

  const keepName = lingering();
  const keepSize = lingering();
  const fileName = $derived(keepName(composer.file?.name ?? ''));
  const fileSize = $derived.by(() => {
    if (!composer.file) {
      return keepSize('');
    }
    const size = formatBytes(composer.file.size, translator.language);
    return keepSize(
      composer.problemOf.file === undefined
        ? size
        : translate('compose.file.over', { size, limit: formatBytes(composer.fileLimit, translator.language) }),
    );
  });
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
      'block min-h-[200px] w-full resize-none rounded-t-card bg-transparent px-6 pt-6 pb-14 text-lead text-ink caret-aegis-300 transition-opacity duration-500 outline-none placeholder:text-ink-mute md:min-h-[280px] md:text-lead-lg',
      composer.phase === 'encrypting' ? 'opacity-0' : 'opacity-100',
    ]}></textarea>
  <div class="pointer-events-none absolute inset-x-4 bottom-3 flex items-center justify-between gap-3">
    <span
      class={[
        'flex h-9 min-w-0 origin-left items-center gap-2.5 rounded-control pr-1 pl-1.5 text-caption transition-[opacity,translate,scale,background-color,box-shadow] duration-500',
        composer.problemOf.file
          ? 'bg-[rgb(244_63_94/0.14)] shadow-[inset_0_0_0_1px_rgb(251_113_133/0.65),0_4px_14px_-6px_rgb(0_0_0/0.5)]'
          : 'bg-nebula-400/10 shadow-[inset_0_1px_0_rgb(255_255_255/0.06),0_4px_14px_-6px_rgb(0_0_0/0.5)] ring-1 ring-nebula-300/30',
        composer.file && composer.phase === 'compose'
          ? 'pointer-events-auto translate-y-0 scale-100 opacity-100'
          : 'translate-y-1 scale-95 opacity-0',
      ]}
      inert={!composer.file || composer.phase !== 'compose'}
    >
      {#if composer.problemOf.file}
        <Icon name="warning" class="size-6 shrink-0 rounded-[7px] p-1 text-white tile-danger" />
      {:else}
        <Icon name="file" class="size-6 shrink-0 rounded-[7px] bg-current/16 p-[5px] text-nebula-300" />
      {/if}
      <span
        translate="no"
        class={['truncate font-medium md:max-w-[220px]', composer.problemOf.file ? 'text-[#ffe4e6]' : 'text-ink']}
        >{fileName}</span
      >
      <span
        class={[
          'shrink-0 pl-0.5 text-micro tabular-nums',
          composer.problemOf.file ? 'font-medium text-[#fda4af]' : 'text-ink-mute',
        ]}>{fileSize}</span
      >
      <button
        type="button"
        onclick={composer.removeFile}
        aria-label={translate('create.file.clear')}
        class="group/action grid size-7 shrink-0 place-items-center rounded-control text-ink-soft transition hover:bg-white/10 hover:text-ink max-md:-my-1.5 max-md:-mr-2.5 max-md:size-11"
      >
        <Icon name="close" class="size-3.5" stroke={2} animated />
      </button>
    </span>
    <span
      class={[
        'shrink-0 pr-2 text-micro tabular-nums transition-opacity duration-500',
        composer.problemOf.text ? 'text-rose-300' : 'text-ink-mute',
        composer.phase === 'compose' ? 'opacity-100' : 'opacity-0',
      ]}
      aria-hidden="true"
    >
      {#if composer.textBytes >= composer.textLimit * COUNTER_FROM || composer.problemOf.text !== undefined}
        {translate('compose.counter', {
          size: formatSize(composer.textBytes, translator.language),
          max: formatBytes(composer.textLimit, translator.language),
        })}
      {/if}
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
      class="absolute inset-0 [mask-image:radial-gradient(ellipse_70%_68%_at_50%_50%,transparent_42%,black_100%)] px-6 pt-6 text-caption break-all text-ink-soft/15 tabular-nums"
    >
      {composer.cipher}
    </p>
    <div
      class="absolute inset-x-0 top-0 h-28 animate-scan bg-linear-to-b from-transparent via-aegis-300/6 to-transparent"
    ></div>
    <div
      class="absolute inset-0 bg-[radial-gradient(ellipse_62%_62%_at_50%_50%,oklch(0.13_0.04_270/0.92),transparent_88%)]"
    ></div>
    <div
      class="absolute inset-0 bg-[radial-gradient(40%_46%_at_50%_40%,oklch(0.67_0.16_252/0.11),oklch(0.72_0.14_305/0.05)_55%,transparent_78%)]"
    ></div>
    <div class="absolute inset-0 flex flex-col items-center justify-center px-4">
      <PipelineRing
        nodes={composer.nodes}
        current={composer.timeline.paced.step}
        progress={composer.timeline.paced.progress}
        active={composer.phase === 'encrypting'}
      />
    </div>
  </div>

  <div
    class="pointer-events-none absolute inset-0 rounded-t-card opacity-0 ring-2 ring-aegis-300/50 transition duration-500 group-focus-within:opacity-100"
    aria-hidden="true"
  ></div>
  <div
    class={[
      'pointer-events-none absolute inset-2 grid place-items-center overflow-hidden rounded-pane border border-dashed border-aegis-300/55 bg-void-950/92 backdrop-blur-md transition-opacity',
      composer.dragging ? 'opacity-100' : 'opacity-0',
    ]}
    aria-hidden="true"
  >
    <span class="absolute inset-0 bg-[radial-gradient(55%_65%_at_50%_50%,oklch(0.56_0.19_258/0.22),transparent_72%)]"
    ></span>
    <div class="relative flex flex-col items-center px-6 text-center">
      <span class="relative grid size-12 place-items-center">
        <span class="absolute -inset-2 animate-pulse-core rounded-tile ring-1 ring-aegis-300/35"></span>
        <span
          class="relative grid size-12 place-items-center rounded-pane text-white shadow-[0_0_36px_-6px_oklch(0.56_0.19_258/0.9)] tile-current"
        >
          <Icon name="upload" class="relative size-6" stroke={1.6} />
        </span>
      </span>
      <p class="mt-5 text-title text-ink">{translate('compose.drop.title')}</p>
      <p class="mt-1 text-caption text-ink-mute">
        {translate('compose.drop.note', { size: formatBytes(composer.fileLimit, translator.language) })}
      </p>
    </div>
  </div>
</div>
