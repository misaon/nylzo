<script lang="ts">
  import { AutoCycle } from '../../lib/autocycle.svelte.ts';
  import { getI18n } from '../../lib/i18n.svelte.ts';
  import Icon from '../ui/Icon.svelte';

  const { t } = getI18n();

  const VIEW_MS = 7000;
  const views = ['you', 'server', 'thief'] as const;
  const cycle = new AutoCycle(
    () => views.length,
    () => VIEW_MS,
  );
  const indicator = ['translate-x-0', 'translate-x-full', 'translate-x-[200%]'] as const;
  const delays = ['delay-[160ms]', 'delay-[220ms]', 'delay-[280ms]', 'delay-[340ms]'] as const;

  const facts = [
    { key: 'who.server.id', value: 'Q3tN0xvR8kLp2WcY5aHf1g' },
    { key: 'who.server.size' },
    { key: 'who.server.expires' },
    { key: 'who.server.opens' },
  ] as const;
  const lacks = ['who.server.lacks.text', 'who.server.lacks.name', 'who.server.lacks.key', 'who.server.lacks.ip'] as const;
  const finds = [
    { key: 'who.thief.key', safe: true },
    { key: 'who.thief.text', safe: true },
    { key: 'who.thief.ip', safe: true },
    { key: 'who.thief.meta', safe: false },
  ] as const;
  const cipherText =
    'AQEAPtYgjmUhBel31iEl2hpChYgCfrL1spNxnyVmihA_2O76UMFxFkM_R5Kjp1vRt-1fjORS_6ilI8ihN5KXSc7Tvo_hBKqFYY_kv5ZJr3J1TWDtkwtDDb-xHKas1VOqg6YYZYn9ZhyiA4uoRgnatmUdjAWtGSU8po-799NksnRH9ucAUsdMlHUvTCQCyEZDz_TddJ8HyS5SUkCnD8zRA9a9SkpXz9w3QlY7Zkuvqdt7s8Stqcbnr3yBdGBLEPH1qhT61qtc4xatws8phP9nhFyJfm5di4PzJ59FHz5r1pY4OjE2jBMptUsGr7CmY-uCu3ZR1zTOlUcR64cXQLioDnkHIfxIq2HZt_PlJhx2jIclHkCiHp6bR1IqfEouHgxzNNAL5wIScGebcy8F5n3_YNBDRzrZSgqbjG3uhkWKFLf6xuI5aHUQPFeNBTxaQWk8JzFalHlsZfYcMMDktXP_tKsf2rcDkdfrUnW5gcF';

  function panel(index: number): string {
    return cycle.index === index ? 'opacity-100 delay-[160ms] duration-300 ease-out' : 'pointer-events-none opacity-0 duration-200 ease-in';
  }

  function item(index: number, step: number): string[] {
    return cycle.index === index ? ['translate-y-0 opacity-100 duration-500 ease-silk', delays[step] ?? ''] : ['translate-y-1.5 opacity-0 duration-150 ease-in'];
  }

  function onkeydown(event: KeyboardEvent) {
    const move = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0;
    if (move !== 0) {
      event.preventDefault();
      const next = (cycle.index + move + views.length) % views.length;
      cycle.select(next);
      document.getElementById(`who-tab-${views[next]}`)?.focus();
    }
  }
</script>

<section id="bezpecnost" class="px-4 py-28 sm:px-6 md:py-40" aria-labelledby="who-title">
  <div class="mx-auto grid max-w-[1160px] items-start gap-10 lg:grid-cols-[minmax(0,0.85fr)_minmax(0,1.15fr)] lg:gap-16">
    <div class="reveal-left lg:sticky lg:top-28">
      <p class="text-sm font-medium text-aegis-300">{t('who.eyebrow')}</p>
      <h2 id="who-title" class="mt-3 font-display text-[28px] leading-[1.2] font-light tracking-[-0.02em] text-balance md:text-[40px]">{t('who.title')}</h2>
      <p class="mt-5 max-w-[440px] leading-relaxed text-pretty text-ink-soft">{t('who.sub')}</p>
    </div>

    <div
      class="glass reveal-right rounded-card p-2.5 md:p-3"
      role="group"
      aria-labelledby="who-title"
      {@attach cycle.observe}
      onmouseenter={cycle.pointerEnter}
      onmouseleave={cycle.pointerLeave}
      onfocusin={cycle.focusIn}
      onfocusout={cycle.focusOut}
    >
      <div class="relative grid grid-cols-3 rounded-control well p-0.5" role="tablist" aria-label={t('who.tabs')}>
        <span
          class={[
            'absolute inset-y-0.5 left-0.5 w-[calc((100%-0.25rem)/3)] overflow-hidden rounded-[8px] bg-aegis-500/25 ring-1 ring-aegis-400/50 transition-transform duration-500 ease-silk',
            indicator[cycle.index],
          ]}
          aria-hidden="true"
        >
          <progress
            class={['progress-tab absolute inset-0 h-full w-full transition-opacity duration-300', cycle.running ? 'opacity-100' : 'opacity-0']}
            max="1"
            value={cycle.progress}
          ></progress>
        </span>
        {#each views as view, index (view)}
          <button
            id="who-tab-{view}"
            type="button"
            role="tab"
            aria-selected={cycle.index === index}
            aria-controls="who-panel-{view}"
            tabindex={cycle.index === index ? 0 : -1}
            onclick={() => cycle.select(index)}
            {onkeydown}
            class={['relative z-10 h-11 rounded-[8px] px-2 text-[13px] sm:text-sm md:h-10', cycle.index === index ? 'text-ink' : 'text-ink-soft hover:text-ink']}
          >
            {t(`who.${view}`)}
          </button>
        {/each}
      </div>

      <div class="stack mt-3">
        <div id="who-panel-you" role="tabpanel" aria-labelledby="who-tab-you" inert={cycle.index !== 0} class={['flex flex-col gap-3 p-2 transition-opacity md:p-3', panel(0)]}>
          <div class={['flex items-center justify-between gap-3 transition-[opacity,translate]', item(0, 0)]}>
            <p class="flex items-center gap-3 text-[15px] font-medium text-ink">
              <span class="grid size-8 shrink-0 place-items-center rounded-[8px] bg-aegis-500/15 text-aegis-300 ring-1 ring-aegis-400/30">
                <Icon name="unlock" class="size-4" />
              </span>
              {t('who.you.title')}
            </p>
            <span
              class="flex items-center gap-1.5 rounded-full bg-aegis-500/10 px-2.5 py-1 text-[11px] font-medium tracking-[0.08em] whitespace-nowrap text-aegis-200 uppercase ring-1 ring-aegis-400/25"
            >
              <span class="size-1.5 animate-pulse-core rounded-full bg-aegis-300" aria-hidden="true"></span>{t('who.you.badge')}
            </span>
          </div>
          <div class={['well relative flex grow flex-col overflow-hidden rounded-pane p-4 transition-[opacity,translate]', item(0, 1)]}>
            <span class="pointer-events-none absolute inset-x-8 top-0 h-px bg-linear-to-r from-transparent via-aegis-300/70 to-transparent" aria-hidden="true"></span>
            <p class="text-[11px] font-medium tracking-[0.12em] text-ink-mute uppercase">{t('who.you.message')}</p>
            <p class="mt-3 text-[15px] leading-relaxed text-ink">
              {t('who.you.line1')}
              <span class="rounded-[6px] bg-aegis-500/12 px-1.5 py-0.5 font-mono text-[13px] whitespace-nowrap text-aegis-200 ring-1 ring-aegis-400/30">Lr7!vQ2-mT9#kd</span>
            </p>
            <p class="mt-1 text-[15px] leading-relaxed text-ink-soft">{t('who.you.line2')}</p>
            <p class="mt-auto flex items-center gap-2 border-t border-white/6 pt-3 text-[13px] text-ink-mute">
              <Icon name="check" class="size-4 shrink-0 text-aegis-300" />
              {t('who.you.deleted')}
            </p>
          </div>
          <div class={['well rounded-pane p-4 transition-[opacity,translate]', item(0, 2)]}>
            <p class="text-[11px] font-medium tracking-[0.12em] text-ink-mute uppercase">{t('who.you.attachment')}</p>
            <div class="mt-3 flex items-center gap-3">
              <span class="grid size-10 shrink-0 place-items-center rounded-control bg-nebula-400/12 text-nebula-300 ring-1 ring-nebula-300/30">
                <Icon name="file" stroke={1.7} class="size-5" />
              </span>
              <div class="min-w-0 flex-1">
                <p class="truncate text-[15px] text-ink">{t('who.you.file')}</p>
                <p class="font-mono text-[13px] text-ink-mute">{t('who.you.filemeta')}</p>
              </div>
              <span class="grid size-9 shrink-0 place-items-center rounded-control bg-white/5 text-ink-soft ring-1 ring-white/10" aria-hidden="true">
                <Icon name="download" class="size-4" />
              </span>
            </div>
          </div>
          <div class={['well rounded-pane p-4 transition-[opacity,translate]', item(0, 3)]}>
            <p class="text-[11px] font-medium tracking-[0.12em] text-ink-mute uppercase">{t('who.you.key')}</p>
            <p class="mt-3 text-[13px] leading-relaxed text-ink-soft">{t('who.you.keynote')}</p>
            <p class="mt-1.5 font-mono text-[13px] break-all text-ink-mute">
              nylzo.example/m/Q3tN0xvR8kLp2WcY5aHf1g<span class="rounded-[4px] bg-aegis-500/15 px-0.5 text-aegis-200">#1.0.Vh2sXq9LmB4tRk7PzW1eNc…</span>
            </p>
          </div>
        </div>

        <div id="who-panel-server" role="tabpanel" aria-labelledby="who-tab-server" inert={cycle.index !== 1} class={['flex flex-col gap-3 p-2 transition-opacity md:p-3', panel(1)]}>
          <div class={['flex items-center justify-between gap-3 transition-[opacity,translate]', item(1, 0)]}>
            <p class="flex items-center gap-3 text-[15px] font-medium text-ink">
              <span class="grid size-8 shrink-0 place-items-center rounded-[8px] bg-white/5 text-ink-soft ring-1 ring-white/12">
                <Icon name="database" stroke={1.7} class="size-4" />
              </span>
              {t('who.server.title')}
            </p>
            <span class="rounded-full bg-white/5 px-2.5 py-1 text-[11px] font-medium tracking-[0.08em] whitespace-nowrap text-ink-soft uppercase ring-1 ring-white/10">
              {t('who.server.badge')}
            </span>
          </div>
          <div class={['well relative flex grow flex-col overflow-hidden rounded-pane p-4 transition-[opacity,translate]', item(1, 1)]}>
            <div class="flex items-center justify-between gap-3">
              <p class="text-[11px] font-medium tracking-[0.12em] text-ink-mute uppercase">{t('who.server.data')}</p>
              <p class="font-mono text-[11px] text-aegis-300">AES-256-GCM</p>
            </div>
            <p
              class="mt-3 h-[44px] grow overflow-hidden font-mono text-[11px] leading-[1.6] break-all text-ink-mute/60 [mask-image:linear-gradient(to_bottom,black_35%,transparent)]"
              aria-hidden="true"
            >
              {cipherText}
            </p>
            <span
              class="pointer-events-none absolute inset-x-0 top-0 h-20 animate-scan bg-linear-to-b from-transparent via-aegis-400/10 to-transparent [animation-duration:5s]"
              aria-hidden="true"
            ></span>
          </div>
          <dl class={['grid grid-cols-2 gap-3 transition-[opacity,translate]', item(1, 2)]}>
            {#each facts as fact (fact.key)}
              <div class="well min-w-0 rounded-pane px-4 py-3">
                <dt class="text-[11px] font-medium tracking-[0.12em] text-ink-mute uppercase">{t(fact.key)}</dt>
                <dd class="mt-1 truncate font-mono text-[13px] text-ink">{'value' in fact ? fact.value : t(`${fact.key}.value`)}</dd>
                <dd class="mt-0.5 text-[13px] text-ink-mute">{t(`${fact.key}.note`)}</dd>
              </div>
            {/each}
          </dl>
          <div class={['well flex items-start gap-4 rounded-pane p-4 transition-[opacity,translate]', item(1, 3)]}>
            <p class="w-12 shrink-0 pt-0.5 text-[11px] font-medium tracking-[0.12em] text-ink-mute uppercase">{t('who.server.lacks')}</p>
            <ul class="grid flex-1 grid-cols-2 gap-x-4 gap-y-2 text-[13px] text-ink-soft">
              {#each lacks as lack (lack)}
                <li class="flex items-center gap-2"><Icon name="ban" class="size-4 shrink-0 text-aegis-300" />{t(lack)}</li>
              {/each}
            </ul>
          </div>
        </div>

        <div id="who-panel-thief" role="tabpanel" aria-labelledby="who-tab-thief" inert={cycle.index !== 2} class={['flex flex-col gap-3 p-2 transition-opacity md:p-3', panel(2)]}>
          <div class={['flex items-center justify-between gap-3 transition-[opacity,translate]', item(2, 0)]}>
            <p class="flex items-center gap-3 text-[15px] font-medium text-ink">
              <span class="grid size-8 shrink-0 place-items-center rounded-[8px] bg-nebula-400/12 text-nebula-300 ring-1 ring-nebula-300/30">
                <Icon name="copy" stroke={1.7} class="size-4" />
              </span>
              {t('who.thief.title')}
            </p>
            <span class="rounded-full bg-nebula-400/10 px-2.5 py-1 text-[11px] font-medium tracking-[0.08em] whitespace-nowrap text-nebula-300 uppercase ring-1 ring-nebula-300/25">
              {t('who.thief.badge')}
            </span>
          </div>
          <div class={['well flex grow flex-col rounded-pane p-4 transition-[opacity,translate]', item(2, 1)]}>
            <p class="text-[11px] font-medium tracking-[0.12em] text-ink-mute uppercase">{t('who.thief.finds')}</p>
            <ul class="mt-1 grid grow divide-y divide-white/6">
              {#each finds as find (find.key)}
                <li class="flex items-center gap-3 py-3 last:pb-0">
                  <div class="min-w-0 flex-1">
                    <p class="text-[15px] text-ink">{t(find.key)}</p>
                    <p class="text-[13px] text-ink-mute">{t(`${find.key}.note`)}</p>
                  </div>
                  <span
                    class={[
                      'shrink-0 rounded-full px-2.5 py-1 text-[11px] font-medium tracking-[0.08em] uppercase ring-1',
                      find.safe ? 'bg-aegis-500/10 text-aegis-200 ring-aegis-400/25' : 'bg-white/5 text-ink-soft ring-white/10',
                    ]}
                  >
                    {t(`${find.key}.result`)}
                  </span>
                </li>
              {/each}
            </ul>
          </div>
          <div class={['transition-[opacity,translate]', item(2, 2)]}>
            <a
              href="#odolnost"
              class="group flex items-center justify-between gap-3 rounded-pane bg-linear-to-r from-aegis-500/20 via-aegis-500/10 to-nebula-400/10 p-4 ring-1 ring-aegis-400/30 hover:ring-aegis-300/60"
            >
              <span>
                <span class="block text-[15px] text-ink">{t('who.thief.cta')}</span>
                <span class="mt-0.5 block text-[13px] text-ink-soft">{t('who.thief.cta.sub')}</span>
              </span>
              <Icon name="arrowRight" class="size-5 shrink-0 text-aegis-300 transition-transform duration-300 ease-silk group-hover:translate-x-0.5" />
            </a>
          </div>
        </div>
      </div>
    </div>
  </div>
</section>
