<script lang="ts">
  import { getTranslator } from '../../../../shared/language/translator.svelte.ts';
  import { REPOSITORY_URL } from '../../../../shared/shell/repository.ts';
  import Button from '../../../../shared/ui/button/Button.svelte';
  import CopyButton from '../../../../shared/ui/button/CopyButton.svelte';
  import { FocusTargets } from '../../../../shared/ui/focus/focus.ts';
  import { arrowTarget } from '../../../../shared/ui/focus/roving-focus.ts';
  import Icon from '../../../../shared/ui/icon/Icon.svelte';
  import { reveal } from '../../../../shared/ui/reveal/reveal.ts';
  import SectionIntro from '../../../../shared/ui/section/SectionIntro.svelte';
  import IconTile from '../../../../shared/ui/tile/IconTile.svelte';
  import { installLines, installText, LINE_DELAYS, TOKEN_CLASSES } from './commands.ts';
  import type { InstallTab } from './commands.ts';

  const { translate } = getTranslator();

  const tabs: readonly InstallTab[] = ['cli', 'compose', 'binary', 'verify'];
  const indicator = ['translate-x-0', 'translate-x-full', 'translate-x-[200%]', 'translate-x-[300%]'] as const;
  const points = [
    { key: 'host.point.cli', revealIndex: '[--reveal-index:3]' },
    { key: 'host.point.compose', revealIndex: '[--reveal-index:4]' },
    { key: 'host.point.signed', revealIndex: '[--reveal-index:5]' },
    { key: 'host.point.mit', revealIndex: '[--reveal-index:6]' },
  ] as const;

  let tab = $state<InstallTab>('cli');
  const focus = new FocusTargets<InstallTab>();
  const index = $derived(tabs.indexOf(tab));
  const domain = $derived(translate('host.domain'));

  function onkeydown(event: KeyboardEvent) {
    const next = arrowTarget(event.key, index, tabs.length);
    const target = next === undefined ? undefined : tabs[next];
    if (target === undefined) {
      return;
    }
    event.preventDefault();
    tab = target;
    focus.focus(target);
  }
</script>

<span id="vlastni-server" class="block" aria-hidden="true"></span>

<section id="self-host" class="px-4 py-28 sm:px-6 md:py-40" aria-labelledby="host-title">
  <div class="mx-auto grid max-w-page items-center gap-10 lg:grid-cols-[minmax(0,0.85fr)_minmax(0,1.15fr)] lg:gap-16">
    <div {@attach reveal}>
      <SectionIntro id="host-title" eyebrow={translate('host.eyebrow')} title={translate('host.title')} />
      <p
        class="mt-5 max-w-[460px] reveal-focus text-lead leading-relaxed text-pretty text-ink-soft [--reveal-index:2] md:text-lead-lg"
      >
        {translate('host.sub')}
      </p>
      <ul role="list" class="mt-8 grid gap-4 text-body text-ink-soft">
        {#each points as point (point.key)}
          <li class={['flex reveal-focus items-center gap-3 leading-7 [--reveal-blur:5px]', point.revealIndex]}>
            <IconTile icon="check" tone="neutral" size="small" />
            {translate(point.key)}
          </li>
        {/each}
      </ul>
      <Button variant="ghost" href={`${REPOSITORY_URL}/wiki/Self-hosting`} class="mt-8 reveal-focus [--reveal-index:7]">
        {translate('host.guide')}
        <Icon name="external" animated />
      </Button>
    </div>
    <div
      class="glass min-w-0 reveal-rise rounded-card p-2.5 [--reveal-delay:200ms] md:p-3 lg:[--reveal-x:32px] lg:[--reveal-y:0px]"
      {@attach reveal}
    >
      <div
        class="flex reveal-focus flex-col gap-2 [--reveal-blur:4px] [--reveal-delay:420ms] sm:flex-row sm:items-center sm:justify-between"
      >
        <div
          class="relative grid grid-cols-4 rounded-pane bg-white/4 p-1 shadow-rim ring-1 ring-white/8"
          role="tablist"
          aria-label={translate('host.tabs')}
        >
          <span
            class={[
              'absolute inset-y-1 left-1 w-[calc((100%-0.5rem)/4)] rounded-control bg-white/10 shadow-rim ring-1 ring-white/12 transition-transform duration-500',
              indicator[index],
            ]}
            aria-hidden="true"
          ></span>
          {#each tabs as id (id)}
            {const selected = $derived(id === tab)}
            <button
              id="host-tab-{id}"
              type="button"
              role="tab"
              aria-selected={selected}
              aria-controls="host-panel"
              tabindex={selected ? 0 : -1}
              {@attach focus.attach(id)}
              onclick={() => (tab = id)}
              {onkeydown}
              class={[
                'relative z-10 h-11 rounded-control px-3.5 text-ui transition md:h-9',
                selected ? 'text-ink' : 'text-ink-mute hover:text-ink',
              ]}
            >
              {translate(`host.${id}`)}
            </button>
          {/each}
        </div>
        <CopyButton text={installText(tab, domain)} label={translate('host.copy.label')} class="max-sm:hidden" />
      </div>
      <div
        class="mt-2.5 overflow-hidden rounded-pane bg-void-950/85 shadow-[0_18px_40px_-18px_oklch(0.05_0.03_275/0.9)] ring-1 ring-white/12"
      >
        <div class="flex items-center gap-2 border-b border-white/8 bg-white/4 px-4 py-2.5" aria-hidden="true">
          <span class="size-2.5 rounded-full bg-[oklch(0.68_0.17_25/0.85)]"></span>
          <span class="size-2.5 rounded-full bg-[oklch(0.82_0.14_85/0.85)]"></span>
          <span class="size-2.5 rounded-full bg-[oklch(0.74_0.16_150/0.85)]"></span>
          <p class="flex-1 text-center text-micro text-ink-mute tabular-nums">{translate('host.terminal')}</p>
          <span class="w-[46px]"></span>
        </div>
        <div
          id="host-panel"
          role="tabpanel"
          aria-labelledby="host-tab-{tab}"
          tabindex="0"
          class="stack overflow-x-auto p-4 text-caption tabular-nums outline-2 -outline-offset-2 outline-transparent transition focus-visible:-outline-offset-2 focus-visible:outline-aegis-300 md:text-body-sm"
        >
          {#each tabs as id (id)}
            {const lines = $derived(installLines(id, domain))}
            <div
              class={[
                'transition-opacity',
                id === tab ? 'opacity-100 delay-180' : 'pointer-events-none opacity-0 ease-in',
              ]}
              aria-hidden={id !== tab}
            >
              {#each lines as tokens, row (row)}
                <p
                  class={[
                    'reveal-type pl-[1.15em] -indent-[1.15em] wrap-anywhere whitespace-pre-wrap',
                    LINE_DELAYS[row],
                  ]}
                >
                  <span class="text-ink-mute/60 select-none">$&nbsp;</span
                  >{#each tokens as token, position (position)}{position > 0 && !token.joined ? ' ' : ''}<span
                      class={TOKEN_CLASSES[token.kind]}>{token.text}</span
                    >{/each}
                </p>
              {/each}
              <p
                class={[
                  'reveal-type pl-[1.15em] -indent-[1.15em] wrap-anywhere whitespace-pre-wrap',
                  LINE_DELAYS[lines.length],
                ]}
                aria-hidden="true"
              >
                <span class="text-ink-mute/60 select-none">$&nbsp;</span><span
                  class="inline-block h-[1.05em] w-[0.6em] translate-y-[0.18em] animate-blink bg-aegis-300/80"
                ></span>
              </p>
            </div>
          {/each}
        </div>
      </div>
    </div>
  </div>
</section>
