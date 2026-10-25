<script lang="ts">
  import { AutoCycle } from './auto-cycle.svelte.ts';
  import { getI18n } from '../../../shared/i18n/i18n.svelte.ts';
  import Icon from '../../../shared/ui/Icon.svelte';
  import Switch from '../../../shared/ui/Switch.svelte';

  const { t } = getI18n();

  const steps = [
    { icon: 'lock', key: 'how.step1', reveal: 'reveal-left' },
    { icon: 'server', key: 'how.step2', reveal: 'reveal-up reveal-step-1' },
    { icon: 'eye', key: 'how.step3', reveal: 'reveal-right reveal-step-2' },
  ] as const;

  const parts = [
    {
      id: 'host',
      text: 'https://nylzo.example/m/',
      server: true,
      tone: 'text-ink-soft',
      dot: 'bg-ink-mute',
      bar: 'text-ink-mute',
    },
    {
      id: 'id',
      text: 'Q3tN0xvR8kLp2WcY5aHf1g',
      server: true,
      tone: 'text-nebula-300',
      dot: 'bg-nebula-300',
      bar: 'text-nebula-300',
    },
    { id: 'meta', text: '#1.0.', server: false, tone: 'text-rose-300', dot: 'bg-rose-300', bar: 'text-rose-300' },
    {
      id: 'key',
      text: 'Vh2sXq9LmB4tRk7PzW1eNc6YuJ3oFa8DiG5bTn0KwEs',
      server: false,
      tone: 'text-aegis-300',
      dot: 'bg-aegis-400',
      bar: 'text-aegis-400',
    },
  ] as const;

  const SERVER = parts.length;
  const READ_BASE_MS = 1100;
  const READ_MS_PER_CHAR = 28;
  const READ_MIN_MS = 4100;
  const READ_MAX_MS = 6750;

  function body(index: number): string {
    const part = parts[index];
    return t(part ? `how.link.${part.id}.body` : 'how.link.server.body');
  }

  const cycle = new AutoCycle(
    () => parts.length + 1,
    (index) => Math.min(READ_MAX_MS, Math.max(READ_MIN_MS, READ_BASE_MS + body(index).length * READ_MS_PER_CHAR)),
  );

  const serverView = $derived(cycle.index === SERVER);
</script>

<section id="jak" class="px-4 py-28 sm:px-6 md:py-40" aria-labelledby="jak-title">
  <div class="mx-auto max-w-[1160px]">
    <div class="mx-auto max-w-[640px] reveal-up text-center">
      <p class="text-sm font-medium text-aegis-300">{t('how.eyebrow')}</p>
      <h2
        id="jak-title"
        class="mt-3 font-display text-[28px] leading-[1.2] font-light tracking-[-0.02em] text-balance md:text-[40px]"
      >
        {t('how.title')}
      </h2>
    </div>

    <div class="relative mt-16">
      <div
        class="pointer-events-none absolute top-[52px] right-[17%] left-[17%] hidden h-px overflow-hidden bg-linear-to-r from-transparent via-aegis-400/35 to-transparent md:block"
        aria-hidden="true"
      >
        <span
          class="absolute inset-y-0 left-0 w-1/4 animate-beam bg-linear-to-r from-transparent via-aegis-200 to-transparent"
        ></span>
      </div>
      <ol class="grid gap-4 md:grid-cols-3 md:gap-6">
        {#each steps as step (step.key)}
          <li
            class={[
              'glow-on-hover glass flex hover-lift flex-col items-center rounded-card px-6 pt-7 pb-8 text-center',
              step.reveal,
            ]}
          >
            <span
              class="relative grid size-12 place-items-center rounded-control bg-aegis-500/15 text-aegis-200 ring-1 ring-aegis-400/35"
            >
              <Icon name={step.icon} stroke={1.6} class="size-6" />
            </span>
            <h3 class="mt-6 text-lg font-medium">{t(`${step.key}.title`)}</h3>
            <p class="mt-2 max-w-[300px] text-[15px] leading-relaxed text-pretty text-ink-soft">
              {t(`${step.key}.text`)}
            </p>
          </li>
        {/each}
      </ol>
    </div>

    <div
      class="glass mt-6 reveal-up rounded-card p-5 md:p-8"
      role="group"
      aria-labelledby="jak-link-title"
      {...cycle.region}
    >
      <div class="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
        <div>
          <h3 id="jak-link-title" class="font-display text-xl font-light tracking-[-0.015em] md:text-2xl">
            {t('how.link.title')}
          </h3>
          <p class="mt-1 text-sm text-ink-mute">{t('how.link.sub')}</p>
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={serverView}
          onclick={() => cycle.select(serverView ? 0 : SERVER)}
          class="flex h-11 shrink-0 items-center gap-3 self-start rounded-control text-sm text-ink-soft hover:text-ink md:h-9 md:self-auto"
        >
          <Switch on={serverView} size="md" />
          {t('how.link.server')}
        </button>
      </div>

      <div class="relative mt-6 flex items-center gap-3 overflow-x-auto rounded-pane px-4 py-3.5 well">
        <Icon name="lock" class="size-4 shrink-0 text-aegis-300" />
        <p class="flex font-mono text-[13px] whitespace-nowrap md:text-[15px]">
          {#each parts as part, index (part.id)}
            {@const active = !serverView && cycle.index === index}
            {@const dimmed = serverView ? !part.server : !active}
            <button
              type="button"
              aria-pressed={active}
              onclick={() => cycle.select(index)}
              class={[
                'relative rounded-[6px] px-px pt-1 pb-1.5 transition-opacity duration-500',
                part.tone,
                dimmed ? 'opacity-60 hover:opacity-90' : 'opacity-100',
              ]}
            >
              <span class="sr-only">{t(`how.link.${part.id}.title`)}: </span>{part.text}
              <span
                class={[
                  'absolute inset-x-px bottom-0 h-[3px] rounded-full transition-opacity duration-500',
                  part.dot,
                  dimmed ? 'opacity-0' : 'opacity-100',
                ]}
              ></span>
            </button>
          {/each}
        </p>
      </div>

      <div class="mt-3 grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
        {#each parts as part, index (part.id)}
          {@const active = !serverView && cycle.index === index}
          {@const lit = active || (serverView && part.server)}
          <button
            type="button"
            aria-pressed={active}
            onclick={() => cycle.select(index)}
            class={[
              'relative overflow-hidden rounded-pane p-4 text-left ring-1 transition-opacity duration-500',
              lit ? 'bg-white/6 ring-white/20' : 'ring-white/8 hover:bg-white/4',
              serverView && !part.server ? 'opacity-60' : 'opacity-100',
            ]}
          >
            <progress
              class={[
                'pointer-events-none absolute inset-x-0 bottom-0 h-0.5 w-full progress-cycle',
                part.bar,
                cycle.running && lit ? 'opacity-100' : 'opacity-0',
              ]}
              max="1"
              value={cycle.running && lit ? cycle.progress : 0}
              aria-hidden="true"
            ></progress>
            <span class={['block h-[3px] w-7 rounded-full', part.dot]}></span>
            <span class="mt-3 block text-[15px] text-ink">{t(`how.link.${part.id}.title`)}</span>
            <span class="mt-1 block font-mono text-xs text-ink-mute">{t(`how.link.${part.id}.meta`)}</span>
          </button>
        {/each}
      </div>

      <div class="mt-5 stack">
        {#each [...parts.map((part) => part.id), 'server'] as id, index (id)}
          <p
            class={[
              'max-w-[760px] px-1 leading-relaxed text-pretty text-ink-soft transition-opacity duration-700 ease-silk',
              cycle.index === index ? 'opacity-100' : 'pointer-events-none opacity-0',
            ]}
            aria-hidden={cycle.index !== index}
          >
            {body(index)}
          </p>
        {/each}
      </div>
    </div>
  </div>
</section>
