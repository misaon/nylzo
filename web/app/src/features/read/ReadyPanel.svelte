<script lang="ts">
  import { formatCount, formatWait } from '../../shared/language/format.ts';
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import Button from '../../shared/ui/button/Button.svelte';
  import Icon from '../../shared/ui/icon/Icon.svelte';
  import SwapIcon from '../../shared/ui/icon/SwapIcon.svelte';
  import { lingering } from '../../shared/ui/lingering.ts';
  import { PRIVATE_PASSPHRASE, submitOnEnter } from '../../shared/ui/private-fields.ts';
  import type { ReadPage } from './read-page.svelte.ts';
  import { panelClasses } from './read-page.svelte.ts';

  interface Props {
    readonly page: ReadPage;
    readonly formId: string;
  }

  const { page, formId }: Props = $props();
  const translator = getTranslator();
  const { translate } = translator;
  const id = $props.id();

  const loading = $derived(page.shown.view === 'loading');
  const revealable = $derived(
    page.ready !== undefined && !page.deleting && !page.askingDelete && page.phase === 'ready',
  );
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
    return translate('read.rejected.wrong', {
      attempts: formatCount(rejected.attemptsLeft, 'attempt', translator.language),
    });
  });
  const alert = $derived.by(() => {
    if (page.ready?.deviceFailed) {
      return translate('read.device');
    }
    return page.ready?.leaseEnded ? translate('read.lease_ended') : '';
  });
  const passphraseAlert = $derived(passphraseError || alert);
  const keepAlert = lingering();
  const shownAlert = $derived(keepAlert(passphraseAlert));
  const subtitle = $derived.by(() => {
    if (loading) {
      return translate('open.loading.text');
    }
    return translate(page.ready?.resumable ? 'read.resumable' : 'open.ready.sub');
  });
</script>

{#snippet revealButton(className: string)}
  <Button
    variant="primary"
    size="large"
    type="submit"
    form={formId}
    disabled={!revealable}
    class={[
      'transition-opacity duration-500',
      className,
      revealable ? 'opacity-100' : 'pointer-events-none opacity-40',
    ]}
  >
    <Icon name="unlock" class="size-[18px] shrink-0" />
    <span class="relative">{translate('read.reveal')}</span>
  </Button>
{/snippet}

<div
  {@attach page.focus.attach('readyArea')}
  class={[
    'flex flex-col items-center justify-center gap-7 px-5 py-12 text-center fade-swap sm:px-10 md:py-14',
    panelClasses(page.phase === 'ready', page.phase),
  ]}
  inert={page.phase !== 'ready'}
>
  <span class="relative grid size-20 place-items-center" aria-hidden="true">
    <span
      class="absolute -inset-5 rounded-full bg-[radial-gradient(closest-side,oklch(0.56_0.19_258/0.35),transparent)]"
    ></span>
    <span class="absolute -inset-3 rounded-[28px] ring-1 ring-aegis-300/15"></span>
    <span class="absolute -inset-1.5 animate-pulse-core rounded-[25px] ring-1 ring-aegis-300/30"></span>
    <span class="relative grid size-20 place-items-center rounded-tile text-white step-current">
      <Icon name="envelope" stroke={1.5} class="size-9" />
    </span>
    <span
      class="absolute -right-2 -bottom-2 grid size-8 place-items-center rounded-full text-white step-current outline-2 outline-void-900"
    >
      <Icon name="keyhole" class="size-4" />
    </span>
  </span>
  <div>
    <h3 class="font-display text-panel font-light text-balance md:text-panel-lg">
      {translate(loading ? 'read.loading' : 'open.ready.title')}
    </h3>
    <p class="mt-2.5 text-body text-pretty text-ink-soft max-sm:min-h-[2lh]">{subtitle}</p>
  </div>
  <form id={formId} class="contents" novalidate onsubmit={page.reveal}>
    {#if page.passphraseRequired}
      <div class="w-full max-w-[560px] text-left">
        <label for="{id}-passphrase" class="sr-only">{translate('read.passphrase')}</label>
        <div
          class={[
            'flex flex-col gap-2.5 transition sm:flex-row sm:items-center sm:gap-3 sm:rounded-card sm:p-2 sm:pl-4 sm:well',
            passphraseAlert &&
              'sm:shadow-[0_0_24px_-8px_oklch(0.65_0.2_15/0.7)] sm:outline-1 sm:-outline-offset-1 sm:outline-rose-300/70',
          ]}
        >
          <div
            class={[
              'flex min-w-0 flex-1 items-center gap-3 rounded-pane py-1 pr-1 pl-4 outline-2 outline-offset-3 outline-transparent transition has-[input:focus-visible]:outline-aegis-300 max-sm:well sm:p-0',
              passphraseAlert &&
                'max-sm:shadow-[0_0_24px_-8px_oklch(0.65_0.2_15/0.7)] max-sm:outline-1 max-sm:-outline-offset-1 max-sm:outline-rose-300/70',
            ]}
          >
            <Icon name="key" class="size-4 shrink-0 text-nebula-300" />
            <input
              id="{id}-passphrase"
              {@attach page.focus.attach('passphrase')}
              bind:value={page.passphrase}
              type={page.showPassphrase ? 'text' : 'password'}
              {...PRIVATE_PASSPHRASE}
              placeholder={translate('open.passphrase.placeholder')}
              aria-invalid={passphraseAlert !== ''}
              aria-describedby="{id}-passphrase-hint {id}-passphrase-alert"
              oninput={page.typed}
              onkeydown={submitOnEnter}
              class="h-11 min-w-0 flex-1 bg-transparent text-body text-ink outline-none placeholder:text-ink-mute md:h-10"
            />
            <button
              type="button"
              onclick={() => (page.showPassphrase = !page.showPassphrase)}
              aria-label={translate(page.showPassphrase ? 'open.passphrase.hide' : 'open.passphrase.show')}
              aria-pressed={page.showPassphrase}
              class="group/action grid size-11 shrink-0 place-items-center rounded-control text-ink-mute transition hover:bg-white/6 hover:text-ink md:size-9"
            >
              <SwapIcon from="eye" to="eyeOff" swapped={page.showPassphrase} size="size-[18px]" animated />
            </button>
          </div>
          {@render revealButton('w-full sm:w-auto')}
        </div>
        <div class="mt-3 stack px-2 text-caption">
          <p
            id="{id}-passphrase-hint"
            class={[
              'flex items-center gap-2 text-ink-mute fade-swap',
              passphraseAlert ? 'invisible opacity-0' : 'opacity-100',
            ]}
          >
            <Icon name="info" class="size-4 shrink-0" />
            {translate('open.passphrase.hint')}
          </p>
          <p
            id="{id}-passphrase-alert"
            role="alert"
            class={[
              'flex items-center gap-2 text-rose-300 fade-swap',
              passphraseAlert ? 'opacity-100' : 'invisible opacity-0',
            ]}
          >
            <Icon name="alert" class="size-4 shrink-0" />
            {shownAlert}
          </p>
        </div>
      </div>
    {:else}
      {@render revealButton('-mt-1 w-full max-w-[360px] sm:w-auto sm:min-w-[260px]')}
      {#if alert}
        <p role="alert" class="flex max-w-[440px] items-center gap-2 text-caption text-rose-300">
          <Icon name="alert" class="size-4 shrink-0" />
          {alert}
        </p>
      {/if}
    {/if}
  </form>
</div>
