<script lang="ts">
  import { formatBytes } from '../../../shared/language/format.ts';
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import WorkingStatus from '../../../shared/pipeline/WorkingStatus.svelte';
  import Button from '../../../shared/ui/button/Button.svelte';
  import { CHIP_FILE, CHIP_IDLE, CHIP_LAYOUT, CHIP_PROBLEM } from '../../../shared/ui/chip/chip-classes.ts';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import type { Composer } from './composer.svelte.ts';
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
    return composer.file ? CHIP_FILE : CHIP_IDLE;
  });
</script>

<div class="relative border-t border-white/8">
  <div class="relative flex flex-col gap-4 p-2.5 sm:flex-row sm:items-center sm:justify-between sm:gap-3">
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
      <div class="group/action relative">
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
            CHIP_LAYOUT,
            'outline-2 outline-offset-2 outline-transparent peer-hover:bg-white/10 peer-hover:text-ink peer-focus-visible:outline-aegis-300',
            attachTone,
          ]}
        >
          <Icon name="paperclip" class="size-4 shrink-0" animated />
          {translate('compose.attach', { size: formatBytes(composer.fileLimit, translator.language) })}
        </label>
      </div>
    </div>

    <div class="stack shrink-0">
      <Button
        {@attach composer.focus.attach('submit')}
        variant="primary"
        type="submit"
        disabled={!composer.limits || composer.busy}
        class={[
          'w-full transition-[opacity,translate,scale] duration-500 max-sm:h-auto max-sm:min-h-11 max-sm:py-2.5 max-sm:text-left max-sm:whitespace-normal',
          composer.busy ? 'pointer-events-none opacity-0' : 'opacity-100',
        ]}
      >
        <Icon name="lock" class="size-4 shrink-0" animated />
        {translate('compose.submit')}
      </Button>
      <WorkingStatus
        label={translate('compose.encrypting')}
        icon="lock"
        progress={composer.timeline.paced.progress}
        shown={composer.busy}
        announced
      />
    </div>
  </div>
</div>
