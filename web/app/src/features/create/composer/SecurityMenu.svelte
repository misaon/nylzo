<script lang="ts">
  import { MIN_PASSPHRASE_CHARS } from '@nylzo/core';

  import type { Composer } from './composer.svelte.ts';

  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Popover from '../../../shared/ui/Popover.svelte';
  import Switch from '../../../shared/ui/Switch.svelte';
  import { WEAK_BELOW_WORDS } from '../create-message.ts';
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
    label={translate('compose.passphrase.chip')}
    problem={composer.problemOf.passphrase !== undefined}
    accent={composer.protect}
  />
  <Popover
    id="{id}-security"
    label={translate('compose.security')}
    open={composer.menu === 'security'}
    onclose={() => composer.closeMenu('security')}
    class="inset-x-0 top-full mt-2 p-1 sm:inset-x-auto sm:left-0 sm:w-72"
  >
    <button
      type="button"
      role="switch"
      aria-checked={composer.protect}
      onclick={composer.toggleProtect}
      class="flex w-full items-start gap-3 rounded-[7px] p-2 text-left hover:bg-white/4"
    >
      <span class="mt-px"><Switch on={composer.protect} /></span>
      <span>
        <span class="block text-[13px] text-ink">{translate('compose.passphrase.switch')}</span>
        <span class="mt-0.5 block text-[11px] leading-relaxed text-ink-mute"
          >{translate('compose.passphrase.switch.note')}</span
        >
      </span>
    </button>
    <div
      class={[
        'flex flex-col gap-1.5 px-2 pt-0.5 pb-2 transition-opacity duration-500',
        composer.protect ? 'opacity-100' : 'opacity-40',
      ]}
    >
      <label for="{id}-passphrase" class="sr-only">{translate('create.passphrase')}</label>
      <input
        id="{id}-passphrase"
        {@attach composer.focus.attach('passphrase')}
        bind:value={composer.passphrase}
        type="password"
        disabled={!composer.protect}
        autocomplete="off"
        autocapitalize="off"
        spellcheck="false"
        data-1p-ignore=""
        data-lpignore="true"
        data-bwignore="true"
        data-form-type="other"
        placeholder={translate('compose.passphrase.placeholder', { words: WEAK_BELOW_WORDS })}
        aria-invalid={composer.problemOf.passphrase !== undefined}
        aria-describedby="{id}-passphrase-hint {id}-passphrase-problem"
        class={[
          'h-11 w-full rounded-[7px] px-3 text-[13px] text-ink well placeholder:text-ink-mute md:h-8',
          composer.problemOf.passphrase && 'shadow-[inset_0_0_0_1px_oklch(0.8_0.08_350/0.7)]',
        ]}
      />
      <p id="{id}-passphrase-hint" class="text-[11px] leading-relaxed text-ink-mute">
        {translate('create.passphrase.hint', { min: MIN_PASSPHRASE_CHARS })}
      </p>
      <p
        id="{id}-passphrase-problem"
        class={[
          'min-h-[1lh] text-[11px] leading-relaxed',
          composer.problemOf.passphrase ? 'text-rose-300' : 'text-nebula-300',
        ]}
      >
        {composer.passphraseMessage}
      </p>
    </div>
    <div class="mx-2 h-px bg-white/8"></div>
    <button
      type="button"
      role="switch"
      aria-checked={composer.remember}
      onclick={() => (composer.remember = !composer.remember)}
      class="flex w-full items-start gap-3 rounded-[7px] p-2 text-left hover:bg-white/4"
    >
      <span class="mt-px"><Switch on={composer.remember} /></span>
      <span>
        <span class="block text-[13px] text-ink">{translate('compose.remember.switch')}</span>
        <span class="mt-0.5 block text-[11px] leading-relaxed text-ink-mute">{translate('compose.remember.note')}</span>
      </span>
    </button>
  </Popover>
</div>
