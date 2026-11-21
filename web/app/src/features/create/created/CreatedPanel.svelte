<script lang="ts">
  import { slide } from 'svelte/transition';

  import { formatMoment } from '../../../shared/language/format.ts';
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import { MAX_LABEL_CHARACTERS } from '../../../shared/sent-records/sent-record.ts';
  import Button from '../../../shared/ui/button/Button.svelte';
  import { PRIVATE_TEXT } from '../../../shared/ui/field/private-fields.ts';
  import { FocusTargets, keepFocus } from '../../../shared/ui/focus/focus.ts';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import SwapIcon from '../../../shared/ui/icon/SwapIcon.svelte';
  import ErrorNote from '../../../shared/ui/notice/ErrorNote.svelte';
  import IconTile from '../../../shared/ui/tile/IconTile.svelte';
  import { Flash } from '../../../shared/ui/transition/flash.svelte.ts';
  import { collapse, motionMs } from '../../../shared/ui/transition/motion.ts';
  import type { Remembered } from '../composer/composer.svelte.ts';
  import type { Created } from '../create-message.ts';
  import { linkSegments } from './link-segments.ts';

  interface Props {
    readonly created: Pick<Created, 'expiresAt' | 'link'>;
    readonly opensMax: number;
    readonly passphrase: string;
    readonly trackable: boolean;
    readonly keeps: boolean;
    readonly remembered: Remembered;
    readonly onremember: (label: string) => void;
    readonly ontrackingleft: () => void;
    readonly onanother: () => void;
    readonly sizing?: boolean;
  }

  const {
    created,
    opensMax,
    passphrase,
    trackable,
    keeps,
    remembered,
    onremember,
    ontrackingleft,
    onanother,
    sizing = false,
  }: Props = $props();
  const translator = getTranslator();
  const { translate } = translator;
  const id = $props.id();

  const COPIED_MS = 2400;
  const SAVED_MS = 900;
  const PASSPHRASE_MASK = '•'.repeat(18);
  // Safari stops breaking after hyphens under keep-all, so only CJK passphrases keep their words whole.
  const CJK = /[\p{scx=Han}\p{scx=Hiragana}\p{scx=Katakana}\p{scx=Hangul}]/u;
  const RISE = 'transition-[opacity,translate] duration-500 starting:[translate:0_0.375rem] starting:opacity-0';

  const copied = new Flash(COPIED_MS);
  const passphraseCopied = new Flash(COPIED_MS);
  const justSaved = new Flash(SAVED_MS);
  const focus = new FocusTargets<'link' | 'title'>();
  let editing = $state(false);
  let copyFailed = $state(false);
  let passphraseShown = $state(false);
  let tracking = $state(false);
  let label = $state('');

  const segments = $derived(linkSegments(created.link));
  const shareable = $derived(
    matchMedia('(pointer: coarse)').matches &&
      typeof navigator.share === 'function' &&
      navigator.canShare({ url: created.link }),
  );
  const trackingOffered = $derived(trackable && !tracking && remembered !== 'remembered');
  const trackingShown = $derived(trackable && tracking && (remembered !== 'remembered' || justSaved.shown));
  const trackingLocked = $derived(remembered === 'saving' || remembered === 'remembered');

  async function copy() {
    try {
      await navigator.clipboard.writeText(created.link);
      copyFailed = false;
      copied.show();
    } catch {
      copyFailed = true;
      focus.focus('link');
    }
  }

  async function copyPassphrase() {
    try {
      await navigator.clipboard.writeText(passphrase);
      passphraseCopied.show();
    } catch {
      passphraseShown = true;
    }
  }

  async function share() {
    try {
      await navigator.share({ url: created.link });
    } catch {}
  }
</script>

{#snippet copyLinkButton()}
  <Button
    variant="primary"
    size="large"
    onclick={() => void copy()}
    aria-label={translate('created.copy')}
    class="w-full"
  >
    <SwapIcon class="relative" from="copy" to="check" swapped={copied.shown} size="size-[18px]" />
    <span class="relative">
      <span class="sm:hidden">{translate('created.copy.short')}</span>
      <span class="hidden sm:inline">{translate('created.copy')}</span>
    </span>
  </Button>
{/snippet}

<div
  class="relative flex flex-col overflow-hidden text-left fade-swap starting:[translate:0_0.75rem] starting:opacity-0"
  out:collapse={{ duration: motionMs(400) }}
>
  <div
    class="relative flex min-h-0 flex-1 flex-col items-center justify-center px-5 py-9 text-center sm:px-10 md:py-12"
  >
    <div
      class="pointer-events-none absolute inset-0 bg-[radial-gradient(50%_60%_at_50%_24%,oklch(0.67_0.16_252/0.1),oklch(0.72_0.14_305/0.04)_55%,transparent_80%)]"
      aria-hidden="true"
    ></div>
    <span
      class="relative grid size-12 shrink-0 place-items-center transition-[opacity,scale] delay-60 duration-500 md:size-14 starting:scale-75 starting:opacity-0"
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
      {@attach () => {
        if (!sizing) {
          focus.focus('title');
        }
      }}
      tabindex="-1"
      class={[
        'relative mt-5 font-display text-panel font-light text-ink delay-120 outline-none md:text-panel-lg',
        RISE,
      ]}
    >
      {translate('created.title')}
    </h2>
    <p class={['relative mt-2 text-ui text-pretty text-ink-soft delay-120 md:text-body', RISE]}>
      {translate(passphrase === '' ? 'created.sub' : 'created.sub.passphrase')}
    </p>

    <div
      class={[
        'relative mt-7 grid w-full max-w-[720px] grid-cols-1 gap-y-3 delay-180 sm:grid-cols-[minmax(0,1fr)_auto] sm:gap-x-3 md:mt-8',
        RISE,
      ]}
    >
      <div
        class="relative col-span-full grid grid-cols-subgrid items-center gap-y-2.5 rounded-card well max-sm:p-2 sm:p-2 sm:pl-4"
      >
        <span
          class={[
            'pointer-events-none absolute inset-0 rounded-card shadow-[0_0_36px_-6px_oklch(0.56_0.19_258/0.9)] ring-1 ring-aegis-300/50 transition-opacity duration-500 max-sm:hidden',
            copied.shown || copyFailed ? 'opacity-100' : 'opacity-0',
          ]}
          aria-hidden="true"
        ></span>
        <div
          class="flex min-w-0 items-center gap-3 rounded-pane px-2 py-1 text-left outline-offset-3 outline-transparent transition not-forced-colors:outline-2 has-[input:focus-visible]:outline-2 has-[input:focus-visible]:outline-aegis-300 sm:p-0"
        >
          <Icon name="lock" class="size-4 shrink-0 text-aegis-300" />
          <div class="relative min-h-11 min-w-0 flex-1">
            <p
              translate="no"
              aria-hidden="true"
              class={[
                'pointer-events-none flex h-full min-h-11 min-w-0 items-center text-body-sm whitespace-nowrap text-ink-soft tabular-nums transition-opacity md:text-body',
                editing ? 'opacity-0' : 'opacity-100',
              ]}
            >
              {#each segments as segment (segment.id)}
                {#if segment.id === 'host'}
                  <span
                    class="min-w-0 overflow-hidden whitespace-nowrap max-[379px]:[mask-image:linear-gradient(90deg,black_70%,transparent)] sm:shrink-0"
                    >{segment.text}</span
                  >
                {:else}
                  <span class="shrink-0">
                    <span class="sm:hidden">{segment.short}</span>
                    <span class="hidden sm:inline">{segment.text}</span>
                  </span>
                {/if}
              {/each}
            </p>
            {#if !sizing}
              <label class="sr-only" for="{id}-link">{translate('created.link')}</label>
            {/if}
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
          <div class="relative grid grid-cols-2 gap-2.5">
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

      {#if passphrase !== ''}
        <div
          class="col-span-full grid grid-cols-subgrid items-center gap-y-2.5 rounded-card text-left well max-sm:p-2 sm:p-2 sm:pl-4"
        >
          <div
            class={[
              'flex min-h-11 min-w-0 items-center gap-3 rounded-pane px-2 sm:px-0',
              passphraseShown && 'max-sm:flex-wrap',
            ]}
          >
            <Icon name="key" class="size-4 shrink-0 text-nebula-300" />
            <span class="shrink-0 text-body-sm text-ink-mute">{translate('create.passphrase')}</span>
            <span
              translate="no"
              class={[
                'min-w-0 text-body font-medium text-ink',
                passphraseShown
                  ? [
                      'wrap-break-word whitespace-pre-wrap max-sm:order-last max-sm:basis-full max-sm:pb-2 max-sm:pl-7 sm:py-1.5',
                      CJK.test(passphrase) && 'break-keep',
                    ]
                  : 'truncate tracking-[0.18em]',
              ]}>{passphraseShown ? passphrase : PASSPHRASE_MASK}</span
            >
            <Button
              variant="plain"
              size="icon"
              aria-label={translate(passphraseShown ? 'created.passphrase.hide' : 'created.passphrase.show')}
              onclick={() => (passphraseShown = !passphraseShown)}
              class="ml-auto"
            >
              <SwapIcon from="eye" to="eyeOff" swapped={passphraseShown} />
            </Button>
          </div>
          <Button variant="tint" size="large" onclick={() => void copyPassphrase()} class="w-full">
            <SwapIcon from="copy" to="check" swapped={passphraseCopied.shown} />
            <span>{translate('created.passphrase.copy')}</span>
          </Button>
        </div>
      {/if}

      <div class="col-span-full mt-1 stack justify-items-center text-caption">
        <p class={['text-center text-balance text-ink-mute fade-swap', copyFailed && 'invisible opacity-0']}>
          {translate('created.once')}
        </p>
        <ErrorNote
          message={copyFailed ? translate('created.copy_failed') : ''}
          class={['text-left fade-swap', copyFailed ? 'opacity-100' : 'invisible opacity-0']}
        />
      </div>
      <span class="sr-only" role="status"
        >{copied.shown || passphraseCopied.shown ? translate('action.copied') : ''}</span
      >
    </div>
  </div>

  {#if trackingShown}
    <div
      transition:slide={{ duration: motionMs(400) }}
      onoutroend={() => {
        keepFocus(focus.get('title'));
        ontrackingleft();
      }}
      class="flex flex-col gap-3 border-t border-white/8 px-4 py-4 text-left sm:flex-row sm:items-center sm:gap-4"
    >
      <div class="flex min-w-0 flex-1 items-center gap-3">
        <IconTile icon="eye" tone="neutral" size="medium" />
        <div class="min-w-0">
          <p class="text-ui font-medium text-ink">{translate('created.track.title')}</p>
          <p class="text-caption text-pretty text-ink-mute">
            {translate(keeps ? 'created.track.note.kept' : 'created.track.note')}
          </p>
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
        <label
          class="flex h-11 min-w-0 flex-1 items-center rounded-control px-3.5 well outline-offset-3 outline-transparent transition not-forced-colors:outline-2 has-[input:focus-visible]:outline-2 has-[input:focus-visible]:outline-aegis-300 sm:w-56 sm:flex-none md:h-9"
        >
          <span class="sr-only">{translate('created.track.label')}</span>
          <input
            {@attach (field) => field.focus({ preventScroll: true })}
            type="text"
            bind:value={label}
            maxlength={MAX_LABEL_CHARACTERS}
            {...PRIVATE_TEXT}
            readonly={trackingLocked}
            placeholder={translate('created.track.placeholder')}
            class="h-full min-w-0 flex-1 bg-transparent text-caption text-ink outline-none placeholder:text-ink-mute"
          />
        </label>
        <Button variant="tint" size="field" type="submit" disabled={trackingLocked}>
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
      'flex flex-col gap-2.5 border-t border-white/8 p-3 delay-240 sm:flex-row sm:items-center sm:justify-between sm:gap-3 sm:pl-4',
      RISE,
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
    <div class="flex items-center gap-1.5 max-sm:flex-col max-sm:items-stretch">
      {#if trackingOffered}
        <Button variant="tint-soft" size="compact" onclick={() => (tracking = true)}>
          <Icon name="eye" class="size-4 shrink-0 text-aegis-300" animated />
          {translate('created.track.title')}
        </Button>
      {/if}
      <Button variant="ghost" size="compact" onclick={onanother} class="w-full sm:w-auto">
        <Icon name="plus" class="size-4 shrink-0" animated />
        {translate('created.another')}
      </Button>
    </div>
  </div>
</div>
