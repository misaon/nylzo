<script lang="ts">
  import { formatWait } from '../../shared/language/format.ts';
  import Interpolated from '../../shared/language/Interpolated.svelte';
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import Button from '../../shared/ui/button/Button.svelte';
  import { privatePassphrase, submitOnEnter } from '../../shared/ui/field/private-fields.ts';
  import Icon from '../../shared/ui/icon/Icon.svelte';
  import SwapIcon from '../../shared/ui/icon/SwapIcon.svelte';
  import { INLINE_TARGET, TEXT_LINK } from '../../shared/ui/link/link-classes.ts';
  import ErrorNote from '../../shared/ui/notice/ErrorNote.svelte';
  import { lingering } from '../../shared/ui/transition/lingering.ts';
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

  let lastReady: typeof page.ready;
  // Hidden after the click, the panel keeps its last ready view, so the card height it sets stays the same.
  const ready = $derived.by(() => (lastReady = page.ready ?? (page.phase === 'ready' ? undefined : lastReady)));
  const loading = $derived(page.shown.view === 'loading');
  const revealable = $derived(
    page.ready !== undefined && !page.deleting && !page.askingDelete && page.phase === 'ready',
  );
  const passphraseError = $derived.by(() => {
    if (page.emptyPassphrase) {
      return translate(page.deleteIntent ? 'read.delete.passphrase' : 'open.passphrase.empty');
    }
    const notice = page.typedAgain ? undefined : ready?.notice;
    if (notice === undefined) {
      return '';
    }
    switch (notice.reason) {
      case 'lease_ended': {
        return '';
      }
      case 'malformed': {
        return translate('read.rejected.malformed');
      }
      case 'long': {
        return translate('read.rejected.long');
      }
      case 'wrong': {
        return notice.retryAfter > 0
          ? translate('read.rejected.wait', { time: formatWait(notice.retryAfter, translator.plural) })
          : translate('read.rejected.wrong', { attempts: translator.plural('count.attempt', notice.attemptsLeft) });
      }
    }
  });
  const alert = $derived.by(() => {
    if (ready?.deviceFailed) {
      return translate('read.device');
    }
    const notice = ready?.notice;
    if (!page.passphraseRequired && notice?.reason === 'wrong' && notice.retryAfter > 0) {
      return translate('read.wait', { time: formatWait(notice.retryAfter, translator.plural) });
    }
    return notice?.reason === 'lease_ended' ? translate('read.lease_ended') : '';
  });
  const passphraseAlert = $derived(passphraseError || alert);
  const keepAlert = lingering();
  const shownAlert = $derived(keepAlert(passphraseAlert));
  const subtitle = $derived.by(() => {
    if (loading) {
      return translate('open.loading.text');
    }
    return translate(ready?.resumable ? 'read.resumable' : 'open.ready.sub');
  });
</script>

{#snippet revealButton({ className }: { readonly className: string })}
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
    panelClasses(page.phase === 'ready'),
  ]}
  inert={page.phase !== 'ready'}
  data-paused={page.phase !== 'ready' || undefined}
>
  <span class="relative grid size-20 place-items-center" aria-hidden="true">
    <span
      class="absolute -inset-5 rounded-full bg-[radial-gradient(closest-side,oklch(0.56_0.19_258/0.35),transparent)]"
    ></span>
    <span class="absolute -inset-3 rounded-[28px] ring-1 ring-aegis-300/15"></span>
    <span class="absolute -inset-1.5 animate-pulse-core rounded-[25px] ring-1 ring-aegis-300/30"></span>
    <span class="relative grid size-20 place-items-center rounded-tile text-white tile-current">
      <Icon name="envelope" stroke={1.5} class="size-9" />
    </span>
    <span
      class="absolute -right-2 -bottom-2 grid size-8 place-items-center rounded-full text-white tile-current outline-2 outline-void-900"
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
              'flex min-w-0 flex-1 items-center gap-3 rounded-pane py-1 pr-1 pl-4 outline-offset-3 outline-transparent transition not-forced-colors:outline-2 has-[input:focus-visible]:outline-2 has-[input:focus-visible]:outline-aegis-300 max-sm:well sm:p-0',
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
              {...privatePassphrase(page.showPassphrase)}
              placeholder={translate('open.passphrase.placeholder')}
              aria-invalid={passphraseAlert !== ''}
              aria-describedby={passphraseAlert === '' ? undefined : `${id}-passphrase-alert`}
              oninput={page.typed}
              onkeydown={submitOnEnter}
              class="h-11 min-w-0 flex-1 bg-transparent text-body text-ink outline-none placeholder:text-ink-mute md:h-10"
            />
            <Button
              variant="ghost"
              size="icon"
              onclick={() => (page.showPassphrase = !page.showPassphrase)}
              aria-label={translate(page.showPassphrase ? 'open.passphrase.hide' : 'open.passphrase.show')}
            >
              <SwapIcon from="eye" to="eyeOff" swapped={page.showPassphrase} size="size-[18px]" animated />
            </Button>
          </div>
          {@render revealButton({ className: 'w-full sm:w-auto' })}
        </div>
        <!-- Holds the alert's height from the start, so a rejected passphrase moves nothing on the page. -->
        <div class="mt-2 -mb-3 flex min-h-[2lh] items-start justify-center px-2 text-left text-caption sm:min-h-[1lh]">
          <ErrorNote
            id="{id}-passphrase-alert"
            message={shownAlert}
            class={['fade-swap', passphraseAlert ? 'opacity-100' : 'invisible opacity-0']}
          />
        </div>
      </div>
    {:else}
      {@render revealButton({ className: '-mt-1 w-full max-w-[360px] sm:w-auto sm:min-w-[260px]' })}
      <ErrorNote message={alert} class={['max-w-[440px] text-left', !alert && 'sr-only']} />
    {/if}
    <p class="text-caption text-balance text-ink-mute">
      <Interpolated text={translate('read.terms')} placeholder="link"
        ><a href="/terms" target="_blank" rel="noreferrer" class={[TEXT_LINK, INLINE_TARGET]}
          >{translate('terms.agree.link')}<span class="sr-only"> {translate('link.new_tab')}</span></a
        ></Interpolated
      >
    </p>
  </form>
</div>
