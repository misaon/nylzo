<script lang="ts">
  import { MIN_PASSPHRASE_CHARACTERS } from '@nylzo/core';

  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Button from '../../../shared/ui/button/Button.svelte';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import Popover from '../../../shared/ui/Popover.svelte';
  import { PRIVATE_PASSPHRASE, submitOnEnter } from '../../../shared/ui/private-fields.ts';
  import Switch from '../../../shared/ui/Switch.svelte';
  import { WEAK_BELOW_WORDS } from '../create-message.ts';
  import type { Composer } from './composer.svelte.ts';
  import MenuChip from './MenuChip.svelte';

  interface Props {
    readonly composer: Composer;
  }

  const { composer }: Props = $props();
  const { translate } = getTranslator();
  const id = $props.id();
</script>

<div class="sm:relative" data-menu>
  <MenuChip
    {composer}
    menu="security"
    popoverId="{id}-security"
    icon="key"
    label={translate(composer.protect ? 'compose.passphrase.chip' : 'compose.passphrase.chip.off')}
    reserve={translate(composer.protect ? 'compose.passphrase.chip.off' : 'compose.passphrase.chip')}
    problem={composer.problemOf.passphrase !== undefined}
    accent={composer.protect}
  />
  <Popover
    id="{id}-security"
    label={translate('compose.security')}
    open={composer.menu === 'security'}
    onclose={() => composer.closeMenu('security')}
    class="inset-x-0 top-full mt-2 p-1.5 sm:inset-x-auto sm:left-0 sm:w-96"
  >
    <button
      type="button"
      role="switch"
      aria-checked={composer.protect}
      onclick={composer.toggleProtect}
      class="grid w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 rounded-control p-2.5 text-left transition duration-300 ease-silk hover:bg-white/4"
    >
      <span class="text-ui font-medium text-ink">{translate('compose.passphrase.switch')}</span>
      <span class="shrink-0"><Switch on={composer.protect} /></span>
      <span class="col-span-2 mt-1 text-caption text-pretty text-ink-mute"
        >{translate('compose.passphrase.switch.note')}</span
      >
    </button>
    <div class={['px-2.5 pb-2.5 transition-opacity duration-500 ease-silk', !composer.protect && 'opacity-40']}>
      <label for="{id}-passphrase" class="sr-only">{translate('create.passphrase')}</label>
      <div class="flex items-center gap-2">
        <div
          class={[
            'flex h-11 min-w-0 flex-1 items-center gap-2.5 rounded-control px-3.5 well transition duration-300 ease-silk md:h-10',
            composer.problemOf.passphrase &&
              'shadow-[0_0_24px_-8px_oklch(0.65_0.2_15/0.7)] outline-1 -outline-offset-1 outline-rose-300/70',
          ]}
        >
          <Icon name="key" class="size-4 shrink-0 text-aegis-300" />
          <input
            id="{id}-passphrase"
            {@attach composer.focus.attach('passphrase')}
            bind:value={composer.passphrase}
            type="text"
            disabled={!composer.protect}
            {...PRIVATE_PASSPHRASE}
            onkeydown={submitOnEnter}
            placeholder={translate('compose.passphrase.placeholder', {
              min: MIN_PASSPHRASE_CHARACTERS,
              words: WEAK_BELOW_WORDS,
            })}
            aria-invalid={composer.problemOf.passphrase !== undefined}
            aria-describedby="{id}-passphrase-hint {id}-passphrase-problem"
            class="h-full min-w-0 flex-1 bg-transparent text-ui text-ink outline-none placeholder:text-ink-mute"
          />
        </div>
        <Button variant="tint" size="compact" disabled={!composer.protect} onclick={composer.generatePassphrase}>
          <Icon name="refresh" animated />
          {translate('compose.passphrase.generate')}
        </Button>
      </div>
      <div class="mt-2 grid *:col-start-1 *:row-start-1">
        <p
          id="{id}-passphrase-hint"
          class={['text-micro text-pretty text-ink-mute', composer.problemOf.passphrase !== undefined && 'invisible']}
        >
          {translate('create.passphrase.hint')}
        </p>
        <p id="{id}-passphrase-problem" class="flex items-start gap-1.5 text-caption text-rose-300 empty:invisible">
          {composer.problemOf.passphrase === undefined ? '' : composer.passphraseMessage}
        </p>
      </div>
    </div>
  </Popover>
</div>
