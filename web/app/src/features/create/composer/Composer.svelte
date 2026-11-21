<script lang="ts">
  import { onDestroy } from 'svelte';

  import Interpolated from '../../../shared/language/Interpolated.svelte';
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import { getScreenWork } from '../../../shared/navigation/screen-work.ts';
  import { getSessionSentStore } from '../../../shared/sent-records/session-sent-store.ts';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import { pauseOffscreen } from '../../../shared/ui/reveal/offscreen.ts';
  import CreatedPanel from '../created/CreatedPanel.svelte';
  import { Composer } from './composer.svelte.ts';
  import ComposerBar from './ComposerBar.svelte';
  import ComposerNotes from './ComposerNotes.svelte';
  import MessageField from './MessageField.svelte';

  const translator = getTranslator();
  const { translate } = translator;
  const composer = new Composer(translator, getSessionSentStore());
  onDestroy(getScreenWork().watch(() => composer.phase === 'encrypting'));
  const id = $props.id();
  const noticeId = `${id}-notice`;
  const HERO_PROOFS = [
    { key: 'hero.proof.encrypted', icon: 'lock' },
    { key: 'hero.proof.free', icon: 'shieldCheck' },
    { key: 'hero.proof.open', icon: 'code' },
  ] as const;
  const PENDING_PASSPHRASE = 'pending';
</script>

<svelte:window
  onclick={composer.onWindowClick}
  onpagehide={composer.leave}
  onpageshow={composer.returned}
  onbeforeunload={composer.forgetPassphrase}
/>

<span id="napsat" class="block" aria-hidden="true"></span>

<section
  id="create"
  class="px-4 pt-10 sm:px-6 md:pt-16"
  aria-labelledby="{id}-title"
  {@attach composer.start}
  {@attach pauseOffscreen}
>
  <div class="relative mx-auto max-w-column text-center">
    <h1 class="animate-rise-visible font-display text-hero font-light text-balance md:text-hero-lg">
      <Interpolated text={translate('hero.title')} placeholder="accent"
        ><span class="text-aegis-300">{translate('hero.title.accent')}</span></Interpolated
      >
    </h1>
    <div
      class={[
        'pt-6 transition-opacity duration-300',
        composer.phase === 'compose' ? 'opacity-100' : 'pointer-events-none absolute inset-x-0 opacity-0',
      ]}
      inert={composer.phase !== 'compose'}
    >
      <p class="mx-auto max-w-[36rem] animate-rise text-body text-pretty text-ink-soft [animation-delay:80ms]">
        {translate('hero.lead')}
      </p>
    </div>
  </div>

  <div {@attach composer.glide.attach}>
    <div class="relative z-20 mx-auto mt-8 max-w-column animate-rise [animation-delay:240ms]">
      <div class="glow-under" aria-hidden="true"></div>
      <div class="glass rounded-card">
        <div class="ring-light" aria-hidden="true"><div class="ring-light-spin animate-spin-slow"></div></div>
        <h2 id="{id}-title" class="sr-only">{translate('create.title')}</h2>

        <div class="relative stack">
          <form
            class={[
              'flex flex-col text-left fade-swap',
              composer.phase === 'done' ? 'pointer-events-none translate-y-3 opacity-0' : 'translate-y-0 opacity-100',
            ]}
            inert={composer.phase === 'done'}
            onsubmit={(event) => void composer.submit(event)}
            autocomplete="off"
            novalidate
          >
            <MessageField {composer} {noticeId} />
            <ComposerBar {composer} {noticeId} />
          </form>

          <!-- Leaving destroys the panel without its outro, which a frozen page would never finish. -->
          {#if !composer.left}
            {#if composer.sealed}
              <CreatedPanel
                created={composer.sealed.created}
                opensMax={composer.sealed.opensMax}
                passphrase={composer.sealed.passphrase}
                trackable={composer.trackable}
                keeps={composer.keeps}
                remembered={composer.remembered}
                onremember={(label) => void composer.remember(label)}
                ontrackingleft={() => (composer.trackingPanelLeft = true)}
                onanother={() => void composer.another()}
              />
            {:else if composer.placeholder}
              <!-- Sized like the panel to come; the inner block keeps its fade-out from outliving the swap. -->
              <div class="invisible" inert aria-hidden="true">
                {#key composer.placeholder}
                  <CreatedPanel
                    sizing
                    created={composer.placeholder}
                    opensMax={composer.opensMax}
                    passphrase={composer.sealingWithPassphrase ? PENDING_PASSPHRASE : ''}
                    trackable={composer.trackable}
                    keeps={composer.keeps}
                    remembered="off"
                    onremember={(label) => void composer.remember(label)}
                    ontrackingleft={() => (composer.trackingPanelLeft = true)}
                    onanother={() => void composer.another()}
                  />
                {/key}
              </div>
            {/if}
          {/if}
        </div>
      </div>
    </div>

    <ComposerNotes {composer} {noticeId} />
    <ul
      role="list"
      aria-label={translate('hero.proof.label')}
      class={[
        'glass-soft mx-auto mt-5 hidden w-fit animate-rise overflow-hidden rounded-full shadow-[inset_0_1px_0_oklch(1_0_0/0.06)] transition-opacity duration-300 [animation-delay:280ms] [animation-fill-mode:backwards] md:flex',
        composer.phase === 'done' && 'pointer-events-none opacity-0',
      ]}
      inert={composer.phase === 'done'}
    >
      {#each HERO_PROOFS as proof (proof.key)}
        <li
          class="flex items-center gap-2.5 border-white/8 px-5 py-2.5 text-caption whitespace-nowrap text-ink-soft not-first:border-l"
        >
          <Icon name={proof.icon} class="size-4 shrink-0 text-aegis-300" />
          <span>{translate(proof.key)}</span>
        </li>
      {/each}
    </ul>
    <p class="sr-only" role="status" aria-live="polite">{composer.stepTitle}</p>
  </div>
</section>
