<script lang="ts">
  import type { Composer } from './composer.svelte.ts';

  import { formatBytes } from '../../../shared/language/format.ts';
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import WorkingStatus from '../../../shared/pipeline/WorkingStatus.svelte';
  import Icon from '../../../shared/ui/Icon.svelte';
  import { CHIP_BASE, CHIP_IDLE, CHIP_PROBLEM } from './chip-classes.ts';
  import OpensMenu from './OpensMenu.svelte';
  import SecurityMenu from './SecurityMenu.svelte';
  import TtlMenu from './TtlMenu.svelte';

  interface Props {
    readonly composer: Composer;
    readonly noticeId: string;
  }

  const { composer, noticeId }: Props = $props();
  const translator = getTranslator();
  const { translate } = translator;
  const id = $props.id();

  const attachTone = $derived.by(() => {
    if (composer.problemOf.file) {
      return CHIP_PROBLEM;
    }
    return composer.file ? 'bg-nebula-400/12 text-nebula-300 ring-nebula-300/35' : CHIP_IDLE;
  });
</script>

<div class="relative border-t border-white/8">
  <div class="relative flex flex-col gap-2 p-2.5 sm:flex-row sm:items-center sm:justify-between sm:gap-3">
    <div
      class={[
        'grid grid-cols-2 gap-1.5 transition-opacity duration-500 sm:flex sm:items-center',
        composer.busy ? 'pointer-events-none opacity-40' : 'opacity-100',
      ]}
      inert={composer.busy}
    >
      <TtlMenu {composer} />
      <OpensMenu {composer} />
      <SecurityMenu {composer} />
      <div class="relative">
        <input
          id="{id}-file"
          {@attach composer.focus.attach('file')}
          type="file"
          class="peer absolute inset-0 z-10 h-full w-full cursor-pointer opacity-0"
          onchange={(event) => composer.pickFile(event.currentTarget.files?.[0])}
          aria-invalid={composer.problemOf.file !== undefined}
          aria-describedby={noticeId}
        />
        <label
          for="{id}-file"
          class={[
            CHIP_BASE,
            'peer-hover:bg-white/8 peer-hover:text-ink peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-aegis-300',
            attachTone,
          ]}
        >
          <Icon name="paperclip" class="size-4 shrink-0" />
          {translate('compose.attach')}<span class="sr-only"
            >{translate('compose.attach.limit', { size: formatBytes(composer.fileLimit, translator.language) })}</span
          >
        </label>
      </div>
    </div>

    <div class="stack shrink-0">
      <button
        {@attach composer.focus.attach('submit')}
        type="submit"
        disabled={!composer.limits || composer.busy}
        class={[
          'btn-primary flex h-11 w-full hover-lift items-center justify-center gap-2 rounded-[9px] px-5 text-sm font-medium whitespace-nowrap transition-[opacity,translate,scale] duration-500 md:h-9 md:px-4',
          composer.busy ? 'pointer-events-none opacity-0' : 'opacity-100',
        ]}
      >
        <span class="sheen" aria-hidden="true"></span>
        <Icon name="lock" stroke={2} class="size-4 shrink-0" />
        <span class="hidden lg:inline">{translate('compose.submit')}</span>
        <span class="lg:hidden">{translate('create.submit')}</span>
      </button>
      <WorkingStatus
        label={translate('compose.encrypting')}
        progress={composer.timeline.paced.progress}
        shown={composer.busy}
        announced
      />
    </div>
  </div>
</div>
