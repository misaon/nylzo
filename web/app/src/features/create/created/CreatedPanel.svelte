<script lang="ts">
  import type { Attachment } from 'svelte/attachments';
  import { fade, slide } from 'svelte/transition';

  import { clearClipboard } from '../../../shared/clipboard/clear-clipboard.ts';
  import { formatMoment } from '../../../shared/language/format.ts';
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import { MAX_LABEL_CHARACTERS } from '../../../shared/sent-records/sent-record.ts';
  import Button from '../../../shared/ui/button/Button.svelte';
  import { PRIVATE_TEXT } from '../../../shared/ui/field/private-fields.ts';
  import { FocusTargets, keepFocus } from '../../../shared/ui/focus/focus.ts';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import SwapIcon from '../../../shared/ui/icon/SwapIcon.svelte';
  import IconTile from '../../../shared/ui/tile/IconTile.svelte';
  import { Flash } from '../../../shared/ui/transition/flash.svelte.ts';
  import { motionMs } from '../../../shared/ui/transition/motion.ts';
  import type { Remembered } from '../composer/composer.svelte.ts';
  import type { Created } from '../create-message.ts';
  import { linkSegments } from './link-segments.ts';

  interface Props {
    readonly created: Created;
    readonly opensMax: number;
    readonly passphrase: string;
    readonly canRemember: boolean;
    readonly remembered: Remembered;
    readonly onremember: (label: string) => void;
    readonly onanother: () => void;
  }

  const { created, opensMax, passphrase, canRemember, remembered, onremember, onanother }: Props = $props();
  const translator = getTranslator();
  const { translate } = translator;
  const id = $props.id();

  const COPIED_MS = 2400;
  const SAVED_MS = 900;
  const PASSPHRASE_MASK = '•'.repeat(18);

  const copied = new Flash(COPIED_MS);
  const passphraseCopied = new Flash(COPIED_MS);
  const justSaved = new Flash(SAVED_MS);
  const focus = new FocusTargets<'clipboard' | 'link' | 'title'>();
  let entered = $state(false);
  let editing = $state(false);
  let clipboard = $state<'cleared' | 'copied' | 'failed' | 'untouched'>('untouched');
  let passphraseShown = $state(false);
  let label = $state('');

  const segments = $derived(linkSegments(created.link));
  const shareable = $derived(
    matchMedia('(pointer: coarse)').matches &&
      typeof navigator.share === 'function' &&
      navigator.canShare({ url: created.link }),
  );
  const trackingShown = $derived(canRemember && (remembered !== 'remembered' || justSaved.shown));
  const clipboardText = $derived.by(() => {
    if (clipboard === 'failed') {
      return translate('created.copy_failed');
    }
    return translate(clipboard === 'cleared' ? 'clipboard.cleared' : 'clipboard.copied');
  });

  const enter: Attachment = () => {
    const frame = requestAnimationFrame(() => {
      entered = true;
      focus.focus('title');
    });
    return () => cancelAnimationFrame(frame);
  };

  async function copy() {
    try {
      await navigator.clipboard.writeText(created.link);
      clipboard = 'copied';
      copied.show();
    } catch {
      clipboard = 'failed';
      focus.focus('link');
    }
  }

  async function copyPassphrase() {
    try {
      await navigator.clipboard.writeText(passphrase);
      clipboard = 'copied';
      passphraseCopied.show();
    } catch {
      passphraseShown = true;
    }
  }

  async function clearCopies() {
    await clearClipboard((state) => {
      clipboard = state;
    });
    await focus.keepFocusAfterUpdate('clipboard');
  }

  async function share() {
    try {
      await navigator.share({ url: created.link });
    } catch {}
  }

  function item(delay: string): string[] {
    return entered
      ? ['translate-y-0 opacity-100 duration-500', delay]
      : ['translate-y-1.5 opacity-0 duration-150 ease-in-out'];
  }
</script>

{#snippet copyLinkButton()}
  <Button
    variant="primary"
    size="large"
    onclick={() => void copy()}
    aria-label={translate('created.copy')}
    class={shareable ? 'w-full' : 'w-full sm:w-auto'}
  >
    <SwapIcon class="relative" from="copy" to="check" swapped={copied.shown} size="size-[18px]" />
    <span class="relative">
      <span class="sm:hidden">{translate('created.copy.short')}</span>
      <span class="hidden sm:inline">{translate('created.copy')}</span>
    </span>
  </Button>
{/snippet}

<div
  class={[
    'relative flex flex-col overflow-hidden text-left fade-swap',
    entered ? 'translate-y-0 opacity-100' : 'translate-y-3 opacity-0',
  ]}
  {@attach enter}
  out:fade={{ duration: motionMs(400) }}
>
  <div
    class="relative flex min-h-0 flex-1 flex-col items-center justify-center px-5 py-9 text-center sm:px-10 md:py-12"
  >
    <div
      class="pointer-events-none absolute inset-0 bg-[radial-gradient(50%_60%_at_50%_24%,oklch(0.67_0.16_252/0.1),oklch(0.72_0.14_305/0.04)_55%,transparent_80%)]"
      aria-hidden="true"
    ></div>
    <span
      class={[
        'relative grid size-12 shrink-0 place-items-center transition-[opacity,scale] md:size-14',
        entered ? 'scale-100 opacity-100 delay-60 duration-500' : 'scale-75 opacity-0 duration-150 ease-in-out',
      ]}
      aria-hidden="true"
    >
      <span
        class="absolute -inset-6 rounded-full bg-[radial-gradient(closest-side,oklch(0.67_0.16_252/0.32),transparent)]"
      ></span>
      <span class="absolute inset-0 rounded-full tile-current"></span>
      <span class="absolute inset-0 rounded-full opacity-0 ring-1 ring-aegis-300/50 motion-safe:animate-burst"></span>
      <Icon name="check" class="relative size-6 text-white" stroke={1.6} />
    </span>
    <h2
      {@attach focus.attach('title')}
      tabindex="-1"
      class={[
        'relative mt-5 font-display text-panel font-light text-ink transition-[opacity,translate] outline-none md:text-panel-lg',
        item('delay-120'),
      ]}
    >
      {translate('created.title')}
    </h2>
    <p
      class={[
        'relative mt-2 text-ui text-pretty text-ink-soft transition-[opacity,translate] md:text-body',
        item('delay-120'),
      ]}
    >
      {translate(passphrase === '' ? 'created.sub' : 'created.sub.passphrase')}
    </p>

    <div class={['relative mt-7 w-full max-w-[720px] transition-[opacity,translate] md:mt-8', item('delay-180')]}>
      <div class="relative rounded-card">
        <span
          class={[
            'pointer-events-none absolute inset-0 rounded-card shadow-[0_0_36px_-6px_oklch(0.56_0.19_258/0.9)] ring-1 ring-aegis-300/50 transition-opacity duration-500 max-sm:hidden',
            copied.shown || clipboard === 'failed' ? 'opacity-100' : 'opacity-0',
          ]}
          aria-hidden="true"
        ></span>
        <div class="rounded-card well max-sm:p-2">
          <div class="flex flex-col gap-2.5 sm:flex-row sm:items-center sm:gap-3 sm:p-2 sm:pl-4">
            <div
              class="flex min-w-0 flex-1 items-center gap-3 rounded-pane px-2 py-1 text-left outline-2 outline-offset-3 outline-transparent transition has-[input:focus-visible]:outline-aegis-300 sm:p-0"
            >
              <Icon name="lock" class="size-4 shrink-0 text-aegis-300" />
              <div class="relative min-h-11 min-w-0 flex-1">
                <p
                  translate="no"
                  aria-hidden="true"
                  class={[
                    'pointer-events-none flex h-full min-h-11 min-w-0 items-center text-body-sm whitespace-nowrap tabular-nums transition-opacity md:text-body',
                    editing ? 'opacity-0' : 'opacity-100',
                  ]}
                >
                  {#each segments as segment (segment.id)}
                    {#if segment.secret}
                      <span
                        class="relative shrink-0 bg-linear-to-r from-aegis-200 via-aegis-300 to-nebula-300 bg-clip-text font-medium text-transparent after:absolute after:inset-x-0 after:-bottom-1 after:h-0.5 after:rounded-full after:bg-linear-to-r after:from-aegis-300/70 after:to-nebula-300/70 after:shadow-[0_0_8px_oklch(0.72_0.14_305/0.6)]"
                      >
                        <span class="sm:hidden">{segment.short}</span>
                        <span class="hidden sm:inline">{segment.text}</span>
                      </span>
                    {:else if segment.id === 'host'}
                      <span
                        class="min-w-0 overflow-hidden whitespace-nowrap text-ink-mute max-[379px]:[mask-image:linear-gradient(90deg,black_70%,transparent)] sm:shrink-0"
                        >{segment.text}</span
                      >
                    {:else}
                      <span class="shrink-0 text-ink-mute">
                        <span class="sm:hidden">{segment.short}</span>
                        <span class="hidden sm:inline">{segment.text}</span>
                      </span>
                    {/if}
                  {/each}
                </p>
                <label class="sr-only" for="{id}-link">{translate('created.link')}</label>
                <input
                  id="{id}-link"
                  {@attach focus.attach('link')}
                  type="text"
                  readonly
                  autocomplete="off"
                  translate="no"
                  spellcheck="false"
                  value={created.link}
                  onfocus={(event) => {
                    editing = true;
                    event.currentTarget.select();
                  }}
                  onblur={() => (editing = false)}
                  class={[
                    'absolute inset-0 h-full w-full bg-transparent text-ui tabular-nums caret-transparent outline-none md:text-body',
                    editing ? 'text-ink-soft' : 'text-transparent',
                  ]}
                />
              </div>
            </div>
            {#if shareable}
              <div class="grid grid-cols-2 gap-2.5">
                {@render copyLinkButton()}
                <Button variant="tint" size="large" onclick={() => void share()} class="w-full">
                  <Icon name="upload" class="size-4 shrink-0" />
                  <span>{translate('created.share')}</span>
                </Button>
              </div>
            {:else}
              {@render copyLinkButton()}
            {/if}
          </div>
          <p
            class="-mx-2 mt-2 -mb-2 flex items-center gap-2 rounded-b-card border-t border-white/8 bg-white/4 px-4 py-3 text-left text-caption text-pretty text-ink-soft sm:m-0"
          >
            <Icon name="alert" class="size-4 shrink-0 text-[oklch(0.82_0.12_80)]" />
            <span>{translate('created.once')}</span>
          </p>
        </div>
      </div>

      {#if passphrase !== ''}
        <div
          class="mt-3 flex flex-col gap-2.5 rounded-card text-left well max-sm:p-2 sm:flex-row sm:items-center sm:gap-3 sm:p-2 sm:pl-4"
        >
          <div class="flex min-h-11 min-w-0 flex-1 items-center gap-3 rounded-pane px-2 sm:px-0">
            <Icon name="key" class="size-4 shrink-0 text-nebula-300" />
            <span class="shrink-0 text-body-sm text-ink-mute">{translate('create.passphrase')}</span>
            <span
              translate="no"
              class={['truncate text-body font-medium text-ink', passphraseShown ? '' : 'tracking-[0.18em]']}
              >{passphraseShown ? passphrase : PASSPHRASE_MASK}</span
            >
            <button
              type="button"
              aria-label={translate(passphraseShown ? 'created.passphrase.hide' : 'created.passphrase.show')}
              onclick={() => (passphraseShown = !passphraseShown)}
              class="group/action ml-auto grid size-11 shrink-0 place-items-center rounded-control text-ink-mute transition hover:bg-white/6 hover:text-ink md:size-9"
            >
              <SwapIcon from="eye" to="eyeOff" swapped={passphraseShown} />
            </button>
          </div>
          <Button variant="tint" size="large" onclick={() => void copyPassphrase()} class="w-full sm:w-auto">
            <SwapIcon from="copy" to="check" swapped={passphraseCopied.shown} />
            <span>{translate('created.passphrase.copy')}</span>
          </Button>
        </div>
      {/if}

      <div class="relative mt-3 text-caption text-ink-mute sm:mt-4 md:mt-5">
        <ul
          role="list"
          class={[
            'mx-auto flex w-fit items-center justify-center gap-6 fade-swap max-sm:hidden',
            clipboard === 'untouched' ? 'opacity-100' : 'opacity-0',
          ]}
        >
          <li class="flex items-center gap-2">
            <span class="size-1.5 shrink-0 rounded-full bg-ink-mute" aria-hidden="true"></span>
            {translate('created.legend.server')}
          </li>
          <li class="flex items-center gap-2">
            <span
              class="h-1 w-4 shrink-0 rounded-full bg-linear-to-r from-aegis-300 to-nebula-300 shadow-[0_0_8px_oklch(0.72_0.14_305/0.6)]"
              aria-hidden="true"
            ></span>
            {translate('created.legend.key')}
          </li>
        </ul>
        <p
          {@attach focus.attach('clipboard')}
          role="status"
          tabindex="-1"
          class={[
            'absolute inset-x-0 top-0 flex flex-col items-center justify-center gap-x-3 gap-y-1 fade-swap outline-none max-sm:static max-sm:mt-3 max-sm:overflow-y-clip max-sm:[transition-property:opacity,transform,translate,height,margin-top] max-sm:duration-200 max-sm:[interpolate-size:allow-keywords] max-sm:inert:mt-0 max-sm:inert:h-0 sm:flex-row',
            clipboard === 'untouched' ? 'pointer-events-none opacity-0' : 'opacity-100',
            clipboard === 'failed' && 'text-rose-300',
          ]}
          inert={clipboard === 'untouched'}
        >
          <span class="text-pretty">{clipboard === 'untouched' ? '' : clipboardText}</span>
          {#if clipboard === 'copied'}
            <Button variant="ghost" size="compact" onclick={() => void clearCopies()}>
              {translate('clipboard.clear')}
            </Button>
          {/if}
        </p>
      </div>
    </div>
  </div>

  {#if trackingShown}
    <div
      out:slide={{ duration: motionMs(400) }}
      onoutroend={() => keepFocus(focus.get('title'))}
      class={[
        'flex flex-col gap-3 border-t border-white/8 px-4 py-4 text-left transition-[opacity,translate] sm:flex-row sm:items-center sm:gap-4',
        item('delay-220'),
      ]}
    >
      <div class="flex min-w-0 flex-1 items-center gap-3">
        <IconTile icon="eye" tone="neutral" size="medium" />
        <div class="min-w-0">
          <p class="text-ui font-medium text-ink">{translate('created.track.title')}</p>
          <p class="text-caption text-pretty text-ink-mute">{translate('created.track.note')}</p>
        </div>
      </div>
      <form
        class="flex flex-row gap-2 sm:items-center"
        onsubmit={(event) => {
          event.preventDefault();
          justSaved.show();
          onremember(label);
        }}
      >
        <label class="flex h-11 min-w-0 flex-1 items-center rounded-control px-3.5 well sm:w-56 sm:flex-none md:h-9">
          <span class="sr-only">{translate('created.track.label')}</span>
          <input
            type="text"
            bind:value={label}
            maxlength={MAX_LABEL_CHARACTERS}
            {...PRIVATE_TEXT}
            readonly={remembered === 'saving' || remembered === 'remembered'}
            placeholder={translate('created.track.placeholder')}
            class="h-full min-w-0 flex-1 bg-transparent text-caption text-ink outline-none placeholder:text-ink-mute"
          />
        </label>
        <Button
          variant="tint"
          size="field"
          type="submit"
          disabled={remembered === 'saving' || remembered === 'remembered'}
        >
          <span class="stack place-items-center">
            <span
              class={['transition-opacity', remembered === 'remembered' && 'opacity-0']}
              aria-hidden={remembered === 'remembered'}>{translate('created.track.save')}</span
            >
            <span
              class={[
                'transition-[opacity,scale]',
                remembered === 'remembered' ? 'scale-100 opacity-100' : 'scale-50 opacity-0',
              ]}
            >
              <Icon name="check" class="size-4" stroke={2} />
              <span class="sr-only">{remembered === 'remembered' ? translate('created.track.saved') : ''}</span>
            </span>
          </span>
        </Button>
      </form>
    </div>
  {/if}

  <div
    class={[
      'flex flex-col gap-2.5 border-t border-white/8 p-3 transition-[opacity,translate] sm:flex-row sm:items-center sm:justify-between sm:gap-3 sm:pl-4',
      item('delay-240'),
    ]}
  >
    <ul
      role="list"
      class="flex flex-wrap items-center justify-start gap-x-5 gap-y-1.5 px-3 py-2 text-caption text-ink-soft max-sm:w-full sm:gap-x-4 sm:px-2 sm:py-0"
    >
      <li class="flex items-center gap-1.5 whitespace-nowrap max-sm:min-h-6 max-sm:text-ui">
        <Icon name="clock" class="size-4 shrink-0 text-ink-mute" />
        {translate('created.expires', { time: formatMoment(created.expiresAt, translator.language) })}
      </li>
      <li class="flex items-center gap-1.5 whitespace-nowrap max-sm:min-h-6 max-sm:text-ui">
        <Icon name="eye" class="size-4 shrink-0 text-ink-mute" />
        {translator.plural('compose.opens', opensMax)}
      </li>
      {#if passphrase !== ''}
        <li class="flex items-center gap-1.5 whitespace-nowrap text-nebula-300 max-sm:min-h-6 max-sm:text-ui">
          <Icon name="key" class="size-4 shrink-0" />
          {translate('create.passphrase')}
        </li>
      {/if}
    </ul>
    <Button variant="ghost" size="compact" onclick={onanother} class="w-full sm:w-auto">
      <Icon name="plus" class="size-4 shrink-0" animated />
      {translate('created.another')}
    </Button>
  </div>
</div>
