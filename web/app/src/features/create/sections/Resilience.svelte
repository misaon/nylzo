<script lang="ts">
  import type { Attachment } from 'svelte/attachments';

  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Icon from '../../../shared/ui/Icon.svelte';
  import Switch from '../../../shared/ui/Switch.svelte';
  import { type Attacker, ATTACKERS, countdown, judge, yearsAmount } from './brute-force.ts';

  const translator = getTranslator();
  const { translate } = translator;

  const TICK_MS = 100;
  const VISIBLE_THRESHOLD = 0.35;
  const facts = [
    { key: 'brute.fact.id', icon: 'hash', accent: true },
    { key: 'brute.fact.pass', icon: 'keyhole', accent: true },
    { key: 'brute.fact.quantum', icon: 'atom', accent: false },
  ] as const;

  let attacker = $state<Attacker>('laptop');
  let quantum = $state(false);
  let elapsed = $state(0);
  let swap = $state(0);

  const judgement = $derived(judge(attacker, quantum));
  const counter = $derived(countdown(attacker, quantum, elapsed));
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
  <div class="mx-auto max-w-[1160px]">
    <div class="mx-auto max-w-[640px] reveal-up text-center">
      <p class="text-sm font-medium text-aegis-300">{translate('brute.eyebrow')}</p>
      <h2
        id="brute-title"
        class="mt-3 font-display text-[28px] leading-[1.2] font-light tracking-[-0.02em] text-balance md:text-[40px]"
      >
        {translate('brute.title')}
      </h2>
      <p class="mx-auto mt-5 max-w-[520px] leading-relaxed text-pretty text-ink-soft">{translate('brute.sub')}</p>
    </div>

    <div class="glass mt-14 reveal-zoom overflow-hidden rounded-card">
      <div class="grid lg:grid-cols-[minmax(0,0.95fr)_minmax(0,1.05fr)]">
        <div class="flex flex-col gap-6 border-b border-white/8 p-6 md:p-10 lg:border-r lg:border-b-0">
          <div>
            <div class="flex items-baseline justify-between gap-4">
              <p id="brute-attacker" class="text-sm text-ink-mute">{translate('brute.attacker')}</p>
              <p class="text-xs text-ink-mute">{translate('brute.approx')}</p>
            </div>
            <div class="mt-4 grid gap-2.5" role="radiogroup" aria-labelledby="brute-attacker">
              {#each ATTACKERS as item (item.id)}
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
                    'flex w-full items-center gap-4 rounded-pane px-4 py-3.5 text-left ring-1',
                    selected ? 'bg-aegis-500/15 ring-aegis-400/50' : 'bg-white/4 ring-white/10 hover:bg-white/7',
                  ]}
                >
                  <span
                    class={[
                      'grid size-10 shrink-0 place-items-center rounded-control ring-1',
                      selected
                        ? 'bg-aegis-500/20 text-aegis-200 ring-aegis-400/40'
                        : 'bg-white/5 text-ink-soft ring-white/10',
                    ]}
                    aria-hidden="true"
                  >
                    <Icon name={item.id} stroke={1.6} class="size-5" />
                  </span>
                  <span class="min-w-0 flex-1">
                    <span class="flex flex-wrap items-center gap-x-2 gap-y-1 text-[15px] leading-5 text-ink">
                      {translate(`brute.${item.id}`)}
                      {#if item.hypothetical}
                        <span class="rounded-full bg-white/6 px-2 py-0.5 text-[11px] leading-4 text-ink-mute"
                          >{translate('brute.hypothetical')}</span
                        >
                      {/if}
                    </span>
                    <span class="mt-1 block text-sm text-ink-mute"
                      >{translate(quantum ? 'brute.speed.steps' : 'brute.speed.guesses', {
                        amount: translate(`brute.amount.${item.id}`),
                      })}</span
                    >
                  </span>
                </button>
              {/each}
            </div>
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={quantum}
            onclick={() => restart(() => (quantum = !quantum))}
            class="flex items-start gap-4 rounded-pane p-5 text-left well hover:bg-white/4"
          >
            <span class="mt-0.5"><Switch on={quantum} size="md" /></span>
            <span>
              <span class="block text-[15px] text-ink">{translate('brute.quantum')}</span>
              <span class="mt-1.5 block text-sm leading-relaxed text-pretty text-ink-mute"
                >{translate('brute.quantum.note')}</span
              >
            </span>
          </button>
        </div>

        <div class="flex flex-col p-6 md:p-10">
          <p class="text-sm text-ink-mute">{translate('brute.time')}</p>
          <div class="mt-4 min-h-[9.75rem] md:min-h-[8.5rem]" aria-live="polite">
            {#key swap}
              <div class={swap > 0 ? 'animate-wave' : ''}>
                <p
                  class="font-display text-[30px] leading-[1.15] font-light tracking-[-0.02em] text-balance text-aegis-200 md:text-[40px]"
                >
                  {translate(`brute.verdict.${judgement.verdict}`)}
                </p>
                <p class="mt-3 leading-relaxed text-pretty text-ink-soft">{subtitle}</p>
              </div>
            {/key}
          </div>

          <div
            class="mt-6 flex flex-1 flex-col rounded-pane bg-linear-to-br from-aegis-500/12 via-aegis-500/4 to-nebula-400/10 p-6 ring-1 ring-aegis-400/25"
            {@attach ticking}
          >
            {#key swap}
              <div class="flex items-center justify-between gap-4">
                <p class={['text-sm text-ink-mute', swap > 0 && 'animate-wave [animation-delay:80ms]']}>
                  {translate(quantum ? 'brute.left.steps' : 'brute.left.keys')}
                </p>
                <span
                  class="flex items-center gap-1.5 rounded-full bg-aegis-500/10 px-2.5 py-1 text-xs text-aegis-200 ring-1 ring-aegis-400/25"
                >
                  <span class="size-1.5 animate-pulse-core rounded-full bg-aegis-300" aria-hidden="true"
                  ></span>{translate('brute.live')}
                </span>
              </div>
              <div
                class={[
                  'mt-4 flex min-h-[6.5rem] flex-1 items-center',
                  swap > 0 && 'animate-wave [animation-delay:140ms]',
                ]}
              >
                <p class="font-mono text-[15px] leading-[1.65] text-ink-soft md:text-[24px]" aria-hidden="true">
                  {counter.head}<span class="text-aegis-300">{counter.tail}</span>
                </p>
              </div>
              <div
                class={[
                  'mt-4 flex items-baseline justify-between gap-4 border-t border-white/8 pt-4',
                  swap > 0 && 'animate-wave [animation-delay:200ms]',
                ]}
              >
                <p class="text-sm text-ink-mute">{translate(quantum ? 'brute.done.steps' : 'brute.done.keys')}</p>
                <p class="font-mono text-sm text-ink tabular-nums" aria-hidden="true">{counter.done}</p>
              </div>
            {/key}
          </div>
        </div>
      </div>

      <div class="border-t border-white/8 p-6 md:p-8">
        <ul class="grid gap-3 lg:grid-cols-3">
          {#each facts as fact (fact.key)}
            <li class="flex items-center gap-4 rounded-pane p-5 well">
              <span
                class={[
                  'grid size-10 shrink-0 place-items-center rounded-control ring-1',
                  fact.accent
                    ? 'bg-aegis-500/15 text-aegis-300 ring-aegis-400/30'
                    : 'bg-nebula-400/12 text-nebula-300 ring-nebula-300/30',
                ]}
                aria-hidden="true"
              >
                <Icon name={fact.icon} stroke={1.6} class="size-5" />
              </span>
              <span class="min-w-0">
                <span class="block text-[15px] text-ink">{translate(fact.key)}</span>
                <span class="mt-1 block text-sm text-ink-mute">{translate(`${fact.key}.note`)}</span>
              </span>
            </li>
          {/each}
        </ul>
      </div>
    </div>
  </div>
</section>
