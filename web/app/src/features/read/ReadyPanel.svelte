<script lang="ts">
  import type { ReadPage } from './read-page.svelte.ts';

  import { formatWait } from '../../shared/language/format.ts';
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import Icon from '../../shared/ui/Icon.svelte';
  import SwapIcon from '../../shared/ui/SwapIcon.svelte';
  import { PANEL_HIDDEN, PANEL_SHOWN } from './read-page.svelte.ts';

  interface Props {
    readonly page: ReadPage;
    readonly formId: string;
  }

  const { page, formId }: Props = $props();
  const translator = getTranslator();
  const { translate } = translator;
  const id = $props.id();

  const loading = $derived(page.shown.view === 'loading');
  const passphraseError = $derived.by(() => {
    if (page.emptyPassphrase) {
      return translate('open.passphrase.empty');
    }
    const rejected = page.typedAgain ? undefined : page.ready?.rejected;
    if (rejected === undefined) {
      return '';
    }
    if (rejected.reason === 'malformed') {
      return translate('read.rejected.malformed');
    }
    if (rejected.reason === 'wrong_or_changed') {
      return translate('read.rejected.wrong_or_changed');
    }
    if (rejected.retryAfter > 0) {
      return translate('read.rejected.wait', { time: formatWait(rejected.retryAfter, translator.language) });
    }
    return translate('read.rejected.wrong', { count: rejected.attemptsLeft });
  });
  const note = $derived.by(() => {
    if (page.ready?.deviceFailed) {
      return { alert: true, text: translate('read.device') };
    }
    if (page.ready?.leaseEnded) {
      return { alert: true, text: translate('read.lease_ended') };
    }
    return { alert: false, text: page.ready?.resumable ? translate('read.resumable') : '' };
  });
  const passphraseHint = $derived(note.text || translate('open.passphrase.hint'));
  const passphraseAlert = $derived(passphraseError || (note.alert ? note.text : ''));
</script>

<div
  {@attach page.focus.attach('readyArea')}
  class={[
    'flex flex-col items-center justify-center gap-7 px-5 py-12 text-center fade-swap sm:px-10 md:py-14',
    page.phase === 'ready' ? PANEL_SHOWN : PANEL_HIDDEN,
  ]}
  inert={page.phase !== 'ready'}
>
  <span class="relative grid size-20 place-items-center" aria-hidden="true">
    <span
      class="absolute -inset-5 rounded-full bg-[radial-gradient(closest-side,oklch(0.56_0.19_258/0.35),transparent)]"
    ></span>
    <span class="absolute -inset-3 rounded-[28px] ring-1 ring-aegis-300/15"></span>
    <span class="absolute -inset-1.5 animate-pulse-core rounded-[25px] ring-1 ring-aegis-300/30"></span>
    <span
      class="relative grid size-20 place-items-center rounded-[22px] bg-aegis-500/15 text-aegis-200 shadow-[0_0_44px_-8px_oklch(0.56_0.19_258/0.85)] ring-1 ring-aegis-400/40"
    >
      <Icon name="envelope" stroke={1.4} class="size-9" />
    </span>
    <span
      class="absolute -right-2 -bottom-2 grid size-8 place-items-center rounded-full bg-void-900 text-aegis-200 shadow-[0_0_16px_-4px_oklch(0.56_0.19_258/0.9)] ring-1 ring-aegis-400/50"
    >
      <Icon name="keyhole" stroke={2} class="size-4" />
    </span>
  </span>
  <div>
    <h3 class="font-display text-2xl font-light tracking-[-0.02em] md:text-[28px]">
      {translate(loading ? 'read.loading' : 'open.ready.title')}
    </h3>
    <p class="mt-2 text-[15px] text-pretty text-ink-mute">
      {translate(loading ? 'open.loading.text' : 'open.ready.sub')}
    </p>
  </div>
  <form id={formId} class="contents" novalidate onsubmit={page.reveal}>
    {#if page.passphraseRequired}
      <div class="w-full max-w-[440px] text-left">
        <label for="{id}-passphrase" class="sr-only">{translate('read.passphrase')}</label>
        <div class="relative rounded-pane">
          <div
            class="flex items-center gap-3 rounded-pane py-2 pr-2 pl-4 well has-[input:focus-visible]:outline-2 has-[input:focus-visible]:outline-offset-3 has-[input:focus-visible]:outline-aegis-300"
          >
            <Icon name="key" class="size-4 shrink-0 text-nebula-300" />
            <input
              id="{id}-passphrase"
              {@attach page.focus.attach('passphrase')}
              bind:value={page.passphrase}
              type={page.showPassphrase ? 'text' : 'password'}
              autocomplete="off"
              autocapitalize="off"
              autocorrect="off"
              spellcheck="false"
              data-1p-ignore=""
              data-lpignore="true"
              data-bwignore="true"
              data-form-type="other"
              placeholder={translate('open.passphrase.placeholder')}
              aria-invalid={passphraseAlert !== ''}
              aria-describedby="{id}-passphrase-hint {id}-passphrase-alert"
              oninput={page.typed}
              class="h-11 min-w-0 flex-1 bg-transparent text-[15px] text-ink outline-none placeholder:text-ink-mute md:h-10"
            />
            <button
              type="button"
              onclick={() => (page.showPassphrase = !page.showPassphrase)}
              aria-label={translate(page.showPassphrase ? 'open.passphrase.hide' : 'open.passphrase.show')}
              aria-pressed={page.showPassphrase}
              class="grid size-11 shrink-0 place-items-center rounded-[8px] text-ink-mute hover:bg-white/6 hover:text-ink md:size-9"
            >
              <SwapIcon from="eye" to="eyeOff" swapped={page.showPassphrase} size="size-[18px]" />
            </button>
          </div>
          <span
            class={[
              'pointer-events-none absolute inset-0 rounded-pane shadow-[0_0_0_1px_oklch(0.72_0.15_15/0.7),0_0_24px_-8px_oklch(0.65_0.2_15/0.7)] transition-opacity duration-500',
              passphraseAlert ? 'opacity-100' : 'opacity-0',
            ]}
            aria-hidden="true"
          ></span>
        </div>
        <div class="mt-3 stack px-1 text-[13px]">
          <p
            id="{id}-passphrase-hint"
            class={['flex items-center gap-2 text-ink-mute fade-swap', passphraseAlert ? 'opacity-0' : 'opacity-100']}
          >
            <Icon name="info" class="size-4 shrink-0" />
            {passphraseHint}
          </p>
          <p
            id="{id}-passphrase-alert"
            role="alert"
            class={['flex items-center gap-2 text-rose-300 fade-swap', passphraseAlert ? 'opacity-100' : 'opacity-0']}
          >
            <Icon name="alert" class="size-4 shrink-0" />
            {passphraseAlert}
          </p>
        </div>
      </div>
    {:else if note.text}
      <p
        role={note.alert ? 'alert' : undefined}
        class={['flex max-w-[440px] items-center gap-2 text-[13px]', note.alert ? 'text-rose-300' : 'text-ink-mute']}
      >
        <Icon name={note.alert ? 'alert' : 'info'} class="size-4 shrink-0" />
        {note.text}
      </p>
    {/if}
  </form>
</div>
