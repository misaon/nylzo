<script lang="ts">
  import Interpolated from '../../../shared/language/Interpolated.svelte';
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import CreatedPanel from '../created/CreatedPanel.svelte';
  import { Composer } from './composer.svelte.ts';
  import ComposerBar from './ComposerBar.svelte';
  import ComposerNotes from './ComposerNotes.svelte';
  import MessageField from './MessageField.svelte';

  const translator = getTranslator();
  const { translate } = translator;
  const composer = new Composer(translator);
  const id = $props.id();
  const noticeId = `${id}-notice`;
  const HERO_STEPS = [
    { key: 'hero.step.write', icon: 'write', badge: 'text-aegis-200 ring-1 ring-aegis-300/30', line: '', before: '' },
    {
      key: 'hero.step.send',
      icon: 'share',
      badge: 'text-aegis-200 ring-1 ring-aegis-300/30',
      line: 'bg-aegis-300/40',
      before: 'before:bg-aegis-300/40',
    },
    {
      key: 'hero.step.vanish',
      icon: 'burst',
      badge: 'border border-dashed border-nebula-300/55 text-nebula-300',
      line: 'bg-linear-to-r from-aegis-300/45 to-nebula-300/10',
      before: 'before:bg-linear-to-r before:from-aegis-300/45 before:to-nebula-300/10',
    },
  ] as const;
  const HERO_PROOFS = ['hero.proof.encrypted', 'hero.proof.cipher', 'hero.proof.tls', 'hero.proof.open'] as const;
</script>

<svelte:window
  onclick={composer.onWindowClick}
  onpagehide={composer.leave}
  onpageshow={composer.returned}
  onbeforeunload={composer.forgetPassphrase}
/>

<span id="napsat" class="block" aria-hidden="true"></span>

<section id="create" class="px-4 pt-10 sm:px-6 md:pt-16" aria-labelledby="{id}-title" {@attach composer.start}>
  <div class="mx-auto max-w-column text-center">
    <p class="mb-3 animate-rise text-eyebrow text-aegis-300 uppercase">{translate('hero.eyebrow')}</p>
    <h1 class="animate-rise-visible font-display text-hero font-light text-balance md:text-hero-lg">
      <Interpolated text={translate('hero.title')} placeholder="accent"
        ><span class="text-aegis-300">{translate('hero.title.accent')}</span></Interpolated
      >
    </h1>
    <div
      class={[
        'transition-[opacity,height,padding] duration-500 [interpolate-size:allow-keywords]',
        composer.phase === 'done' ? 'h-0 overflow-y-clip pt-0 opacity-0' : 'h-auto pt-6 opacity-100',
      ]}
      inert={composer.phase === 'done'}
    >
      <ol
        role="list"
        class="mx-auto grid w-full max-w-[360px] animate-rise grid-cols-3 [animation-delay:160ms] sm:flex sm:w-fit sm:max-w-none sm:items-center"
        aria-label={translate('hero.steps')}
      >
        {#each HERO_STEPS as step, index (step.key)}
          {#if index > 0}
            <li aria-hidden="true" class={['hidden h-px w-10 sm:mx-3 sm:block', step.line]}></li>
          {/if}
          <li
            class={[
              'relative flex flex-col items-center gap-2 text-center sm:flex-row sm:gap-2.5 sm:text-left',
              index > 0 && 'before:absolute before:top-4 before:right-1/2 before:h-px before:w-full sm:before:hidden',
              index > 0 && step.before,
            ]}
          >
            <span class={['glass-soft z-10 grid size-8 shrink-0 place-items-center rounded-full', step.badge]}>
              <Icon name={step.icon} />
            </span>
            <span class="text-caption text-ink-soft sm:text-ui">{translate(step.key)}</span>
          </li>
        {/each}
      </ol>
    </div>
  </div>

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
              canRemember={composer.canRemember}
              remembered={composer.remembered}
              onremember={(label) => void composer.remember(label)}
              onanother={() => void composer.another()}
            />
          {/if}
        {/if}
      </div>
    </div>
  </div>

  <ComposerNotes {composer} {noticeId} />
  <ul
    role="list"
    aria-label={translate('hero.proof.label')}
    class="mx-auto mt-6 flex max-w-column animate-rise flex-wrap justify-center gap-2 [animation-delay:280ms]"
  >
    {#each HERO_PROOFS as proof (proof)}
      <li
        class="inline-flex items-center gap-2 rounded-full bg-white/4 px-3 py-1.5 text-caption text-ink-soft ring-1 ring-white/10"
      >
        <Icon name="check" class="size-3.5 shrink-0 text-aegis-300" stroke={2} />
        {translate(proof)}
      </li>
    {/each}
  </ul>
  <p class="sr-only" role="status" aria-live="polite">{composer.stepTitle}</p>

  <a
    href="#how-it-works"
    aria-label={translate('nav.how')}
    class="group mx-auto mt-12 flex w-fit animate-rise rounded-control p-3 [animation-delay:320ms]"
  >
    <span
      class="hidden h-10 w-6 justify-center rounded-full border border-ink-mute/60 pt-2 transition-colors group-hover:border-ink-soft md:flex"
      aria-hidden="true"
    >
      <span class="h-2 w-0.5 rounded-full bg-aegis-300 motion-safe:animate-wheel"></span>
    </span>
    <Icon
      name="chevronDown"
      class="size-5 text-ink-mute transition-colors group-hover:text-ink motion-safe:animate-bounce md:hidden"
      stroke={1.7}
    />
  </a>
</section>
