<script lang="ts">
  import type { Attachment } from 'svelte/attachments';

  import { getTranslator } from '../../../../shared/language/translator.svelte.ts';
  import Icon from '../../../../shared/ui/icon/Icon.svelte';
  import { reveal } from '../../../../shared/ui/reveal/reveal.ts';
  import SectionIntro from '../../../../shared/ui/section/SectionIntro.svelte';
  import Switch from '../../../../shared/ui/switch/Switch.svelte';
  import IconTile from '../../../../shared/ui/tile/IconTile.svelte';
  import { ATTACKERS, countdown, judge, yearsAmount } from './brute-force.ts';
  import type { Attacker } from './brute-force.ts';
  import { CELL_DELAYS } from './cell-delays.ts';

  const translator = getTranslator();
  const { translate } = translator;

  const TICK_MS = 100;
  const VISIBLE_THRESHOLD = 0.35;
  const LIVE_CELLS = 2;
  const facts = [
    { key: 'brute.fact.id', icon: 'hash', tone: 'current' },
    { key: 'brute.fact.pass', icon: 'keyhole', tone: 'current' },
    { key: 'brute.fact.quantum', icon: 'atom', tone: 'warning' },
  ] as const;
  const FACT_ORDER = ['[--reveal-index:0]', '[--reveal-index:1]', '[--reveal-index:2]'] as const;
  const ATTACKER_ORDER = [
    '[--reveal-index:0]',
    '[--reveal-index:1]',
    '[--reveal-index:2]',
    '[--reveal-index:3]',
    '[--reveal-index:4]',
  ] as const;

  let attacker = $state<Attacker>('laptop');
  let quantum = $state(false);
  let elapsed = $state(0);
  let swap = $state(0);

  const judgement = $derived(judge(attacker, quantum));
  const counter = $derived(countdown(attacker, quantum, elapsed));
  const cells = $derived(`${counter.head}${counter.tail}`.trim().split(' '));
  const live = $derived(Math.max(LIVE_CELLS, counter.tail.trim() === '' ? 0 : counter.tail.trim().split(' ').length));
  const years = $derived(yearsAmount(judgement.yearsLog10, translator.language));
  const subtitle = $derived(
    judgement.bigBang
      ? translate('brute.bigbang')
      : translate('brute.about', { time: translate(years.key, { amount: years.amount }) }),
  );

  function restart(change: () => void) {
    change();
    elapsed = 0;
    swap += 1;
  }

  const ticking: Attachment<HTMLElement> = (element) => {
    let visible = false;
    let timer: ReturnType<typeof setInterval> | undefined;
    const observer = new IntersectionObserver(
      (entries) => {
        const next = entries.some((entry) => entry.isIntersecting);
        if (next === visible) {
          return;
        }
        visible = next;
        clearInterval(timer);
        if (visible) {
          timer = setInterval(() => (elapsed += TICK_MS), TICK_MS);
        }
      },
      { threshold: VISIBLE_THRESHOLD },
    );
    observer.observe(element);
    return () => {
      observer.disconnect();
      clearInterval(timer);
    };
  };
</script>

<span id="odolnost" class="block" aria-hidden="true"></span>

<section id="resilience" class="px-4 py-28 sm:px-6 md:py-40" aria-labelledby="brute-title">
  <div class="mx-auto max-w-page">
    <div class="mx-auto max-w-[640px] text-center" {@attach reveal}>
      <SectionIntro id="brute-title" eyebrow={translate('brute.eyebrow')} title={translate('brute.title')} />
      <p
        class="mx-auto mt-5 max-w-[520px] reveal-focus text-lead leading-relaxed text-pretty text-ink-soft [--reveal-index:2] md:text-lead-lg"
      >
        {translate('brute.sub')}
      </p>
    </div>
    <div class="glass mt-14 reveal-rise overflow-hidden rounded-card [--reveal-scale:0.97]" {@attach reveal}>
      <div class="grid lg:grid-cols-[minmax(0,0.95fr)_minmax(0,1.05fr)]">
        <div class="flex flex-col gap-6 border-b border-white/8 p-6 md:p-10 lg:border-r lg:border-b-0" {@attach reveal}>
          <div>
            <div
              class="flex reveal-focus items-baseline justify-between gap-4 [--reveal-blur:4px] [--reveal-delay:150ms]"
            >
              <p id="brute-attacker" class="text-ui text-ink-mute">{translate('brute.attacker')}</p>
              <p class="text-micro text-ink-mute">{translate('brute.approx')}</p>
            </div>
            <div class="mt-4 grid gap-2" role="radiogroup" aria-labelledby="brute-attacker">
              {#each ATTACKERS as item, index (item.id)}
                {const selected = $derived(item.id === attacker)}
                <button
                  type="button"
                  role="radio"
                  aria-checked={selected}
                  onclick={() => {
                    if (!selected) {
                      restart(() => (attacker = item.id));
                    }
                  }}
                  class={[
                    'flex w-full reveal-focus items-center gap-4 rounded-pane px-4 py-3 text-left ring-1 transition [--reveal-blur:4px] [--reveal-delay:250ms] [--reveal-y:8px]',
                    ATTACKER_ORDER[index],
                    selected ? 'bg-aegis-500/12 ring-aegis-300/50' : 'bg-white/4 ring-white/12 hover:bg-white/6',
                  ]}
                >
                  <IconTile icon={item.id} tone={selected ? 'current' : 'neutral'} size="medium" />
                  <span class="min-w-0 flex-1">
                    <span class="flex flex-wrap items-center gap-x-2 gap-y-1 text-body text-ink">
                      {translate(`brute.${item.id}`)}
                      {#if item.hypothetical}
                        <span
                          class="inline-flex h-5 items-center gap-1.5 rounded-full bg-white/6 px-2 text-micro whitespace-nowrap text-ink-soft ring-1 ring-white/12"
                          >{translate('brute.hypothetical')}</span
                        >
                      {/if}
                    </span>
                    <span class="mt-1 block text-ui text-ink-mute"
                      >{translate(quantum ? 'brute.speed.steps' : 'brute.speed.guesses', {
                        amount: translate(`brute.amount.${item.id}`),
                      })}</span
                    >
                  </span>
                  {#if selected}
                    <Icon
                      name="check"
                      class="size-5 shrink-0 text-aegis-200 drop-shadow-[0_0_6px_oklch(0.86_0.06_246/0.7)]"
                      stroke={1.7}
                    />
                  {:else}
                    <span class="size-5 shrink-0" aria-hidden="true"></span>
                  {/if}
                </button>
              {/each}
            </div>
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={quantum}
            onclick={() => restart(() => (quantum = !quantum))}
            class="flex reveal-focus items-center gap-4 rounded-pane p-5 text-left well transition [--reveal-blur:4px] [--reveal-delay:250ms] [--reveal-index:5] [--reveal-y:8px] hover:bg-white/4"
          >
            <span class="mt-0.5"><Switch on={quantum} size="large" /></span>
            <span>
              <span class="block text-body text-ink">{translate('brute.quantum')}</span>
              <span class="mt-1.5 block text-caption text-pretty text-ink-mute">{translate('brute.quantum.note')}</span>
            </span>
          </button>
        </div>
        <div class="flex flex-col p-6 md:p-10" {@attach reveal}>
          <p class="reveal-focus text-ui text-ink-mute [--reveal-blur:4px] [--reveal-delay:250ms]">
            {translate('brute.time')}
          </p>
          <div class="mt-4 min-h-39 reveal-focus [--reveal-delay:360ms] md:min-h-34" aria-live="polite">
            {#key swap}
              <div class={swap > 0 ? 'animate-wave' : ''}>
                <p class="font-display text-section font-light text-balance text-aegis-200 md:text-section-lg">
                  {translate(`brute.verdict.${judgement.verdict}`)}
                </p>
                <p class="mt-3 leading-relaxed text-pretty text-ink-soft">{subtitle}</p>
              </div>
            {/key}
          </div>
          <div
            class="mt-6 flex flex-1 flex-col rounded-pane bg-linear-to-br from-aegis-500/12 via-aegis-500/4 to-nebula-400/10 p-6 ring-1 ring-aegis-400/35"
            {@attach ticking}
          >
            <div
              class="flex reveal-focus items-center justify-between gap-4 [--reveal-blur:4px] [--reveal-delay:460ms]"
            >
              <p class="text-ui text-ink-mute">{translate(quantum ? 'brute.left.steps' : 'brute.left.keys')}</p>
              <span
                class="inline-flex h-5 items-center gap-1.5 rounded-full bg-aegis-500/12 px-2 text-micro whitespace-nowrap text-aegis-200 ring-1 ring-aegis-400/35"
              >
                <span class="size-1.5 animate-pulse-core rounded-full bg-aegis-300" aria-hidden="true"></span>
                {translate('brute.live')}
              </span>
            </div>
            <div aria-hidden="true" class="mt-5 grid grid-cols-7 gap-1.5 md:my-auto md:grid-cols-9 md:gap-2 md:py-4">
              {#each cells as cell, index (index)}
                <span
                  class={[
                    'reveal-focus rounded-xs py-1.5 text-center text-caption tabular-nums ring-1 transition-colors [--reveal-blur:3px] [--reveal-duration:0.6s] [--reveal-y:6px] md:py-2 md:text-lead',
                    CELL_DELAYS[index],
                    index >= cells.length - live
                      ? 'bg-aegis-500/12 text-aegis-200 ring-aegis-400/35'
                      : 'bg-white/4 text-ink-soft ring-white/8',
                  ]}>{cell}</span
                >
              {/each}
            </div>
            <div
              class="mt-5 flex reveal-focus items-baseline justify-between gap-4 border-t border-white/8 pt-4 [--reveal-blur:3px] [--reveal-delay:1150ms] [--reveal-y:6px]"
            >
              <p class="text-ui text-ink-mute">{translate(quantum ? 'brute.done.steps' : 'brute.done.keys')}</p>
              <p class="text-ui text-ink tabular-nums" aria-hidden="true">{counter.done}</p>
            </div>
            <div
              aria-hidden="true"
              class="mt-3 h-1 reveal-focus overflow-hidden rounded-full bg-white/10 [--reveal-blur:3px] [--reveal-delay:1150ms] [--reveal-y:6px]"
            >
              <div class="h-full w-0.5 rounded-full bg-aegis-300 shadow-glow"></div>
            </div>
          </div>
        </div>
      </div>
      <div class="border-t border-white/8" {@attach reveal}>
        <ul role="list" class="grid divide-y divide-white/8 lg:grid-cols-3 lg:divide-x lg:divide-y-0">
          {#each facts as fact, index (fact.key)}
            <li
              class={[
                'flex reveal-focus items-center gap-4 p-6 [--reveal-blur:5px] [--reveal-step:110ms] md:p-8 lg:flex-col lg:gap-3 lg:text-center',
                FACT_ORDER[index],
              ]}
            >
              <IconTile
                icon={fact.icon}
                tone={fact.tone}
                size="medium"
                class={['reveal-pop [--reveal-delay:160ms] [--reveal-step:110ms]', FACT_ORDER[index]]}
              />
              <span class="min-w-0">
                <span class="block text-body text-ink">{translate(fact.key)}</span>
                <span class="mt-1 block text-ui text-ink-mute">{translate(`${fact.key}.note`)}</span>
              </span>
            </li>
          {/each}
        </ul>
      </div>
    </div>
  </div>
</section>
