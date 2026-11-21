<script lang="ts">
  import { MIN_PASSPHRASE_CHARACTERS } from '@nylzo/core';

  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Button from '../../../shared/ui/button/Button.svelte';
  import { privatePassphrase, submitOnEnter } from '../../../shared/ui/field/private-fields.ts';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import ErrorNote from '../../../shared/ui/notice/ErrorNote.svelte';
  import Switch from '../../../shared/ui/switch/Switch.svelte';
  import { lingering } from '../../../shared/ui/transition/lingering.ts';
  import { WEAK_BELOW_WORDS } from '../create-message.ts';
  import type { Composer } from './composer.svelte.ts';
  import ComposerMenu from './ComposerMenu.svelte';

  interface Props {
    readonly composer: Composer;
  }

  const { composer }: Props = $props();
  const { translate } = getTranslator();
  const id = $props.id();
  const problem = $derived(composer.problemOf.passphrase !== undefined);
  const keepProblem = lingering();
</script>

<ComposerMenu
  {composer}
  menu="security"
  icon="key"
  label={translate(composer.protect ? 'compose.passphrase.chip' : 'compose.passphrase.chip.off')}
  reserve={translate(composer.protect ? 'compose.passphrase.chip.off' : 'compose.passphrase.chip')}
  title={translate('compose.security')}
  problem={composer.problemOf.passphrase !== undefined}
  accent={composer.protect}
  class="sm:w-96"
>
  <button
    type="button"
    role="switch"
    aria-checked={composer.protect}
    aria-labelledby="{id}-protect"
    aria-describedby="{id}-protect-note"
    onclick={() => void composer.toggleProtect()}
    class="grid w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 rounded-control press-soft p-2.5 text-left transition hover:bg-white/4"
  >
    <span id="{id}-protect" class="text-ui font-medium text-ink">{translate('compose.passphrase.switch')}</span>
    <span class="shrink-0"><Switch on={composer.protect} /></span>
    <span id="{id}-protect-note" class="col-span-2 mt-1 text-caption text-pretty text-ink-mute"
      >{translate('compose.passphrase.switch.note')}</span
    >
  </button>
  <div class={['px-2.5 pb-2.5 transition-opacity duration-500', !composer.protect && 'opacity-40']}>
    <label for="{id}-passphrase" class="sr-only">{translate('create.passphrase')}</label>
    <div class="flex items-center gap-2">
      <div
        class={[
          'flex h-11 min-w-0 flex-1 items-center gap-2.5 rounded-control px-3.5 well transition md:h-10',
          composer.problemOf.passphrase
            ? 'shadow-[0_0_24px_-8px_oklch(0.65_0.2_15/0.7)] outline-1 -outline-offset-1 outline-rose-300/70'
            : 'outline-offset-3 outline-transparent not-forced-colors:outline-2 has-[input:focus-visible]:outline-2 has-[input:focus-visible]:outline-aegis-300',
        ]}
      >
        <Icon name="key" class="size-4 shrink-0 text-aegis-300" />
        <input
          id="{id}-passphrase"
          {@attach composer.focus.attach('passphrase')}
          bind:value={composer.passphrase}
          type="text"
          disabled={!composer.protect}
          {...privatePassphrase(true)}
          onfocus={composer.reachPassphrase}
          onkeydown={submitOnEnter}
          placeholder={translate('compose.passphrase.placeholder', {
            min: MIN_PASSPHRASE_CHARACTERS,
            words: WEAK_BELOW_WORDS,
          })}
          aria-invalid={problem}
          aria-describedby={problem ? `${id}-passphrase-problem` : undefined}
          class="h-full min-w-0 flex-1 bg-transparent text-ui text-ink outline-none placeholder:text-ink-mute"
        />
      </div>
      <Button variant="tint" size="compact" disabled={!composer.protect} onclick={composer.generatePassphrase}>
        <Icon name="refresh" animated />
        {translate('compose.passphrase.generate')}
      </Button>
    </div>
    <div
      class={[
        'overflow-y-clip transition-[opacity,height,margin] duration-300 [interpolate-size:allow-keywords]',
        problem ? 'mt-2 h-auto opacity-100' : 'mt-0 h-0 opacity-0',
      ]}
      inert={!problem}
    >
      <ErrorNote
        id="{id}-passphrase-problem"
        message={keepProblem(problem ? composer.passphraseMessage : '')}
        class="py-0.5"
      />
    </div>
  </div>
</ComposerMenu>
