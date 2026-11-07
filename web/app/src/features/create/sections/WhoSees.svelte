<script lang="ts">
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import { reveal } from '../../../shared/ui/reveal/reveal.ts';
  import IconTile from '../../../shared/ui/tile/IconTile.svelte';

  const { translate } = getTranslator();
  const CIPHER_TEXT =
    'AQEAPtYgjmUhBel31iEl2hpChYgCfrL1spNxnyVmihA_2O76UMFxFkM_R5Kjp1vRt-1fjORS_6ilI8ihN5KXSc7Tvo_hBKqFYY_kv5ZJr3J1TWDtkwtDDb-xHKas1VOqg6YYZYn9ZhyiA4uoRgnatmUdjAWtGSU8po-799NksnRH9ucAUsdMlHUvTCQCyEZDz_TddJ8HyS5SUkCnD8zRA9a9SkpXz9w3QlY7Zkuvqdt7s8Stqcbnr3yBdGBLEPH1qhT61qtc4xatws8phP9nhFyJfm5di4PzJ59FHz5r1pY4OjE2jBMptUsGr7CmY-uCu3ZR1zTOlUcR64cXQLioDnkHIfxIq2HZt_PlJhx2jIclHkCiHp6bR1IqfEouHgxzNNAL5wIScGebcy8F5n3_YNBDRzrZSgqbjG3uhkWKFLf6xuI5aHUQPFeNBTxaQWk8JzFalHlsZfYcMMDktXP_tKsf2rcDkdfrUnW5gcF';
  const ARTICLE =
    'flex min-w-0 flex-col gap-3 p-4 md:row-span-5 md:grid md:grid-cols-[minmax(0,1fr)] md:grid-rows-subgrid md:p-5';
  const LABEL = 'text-eyebrow font-medium text-ink-mute uppercase';
</script>

<section id="security" class="px-4 py-28 sm:px-6 md:py-40" aria-labelledby="who-title">
  <div class="mx-auto max-w-[1160px]">
    <div class="mx-auto max-w-[640px] text-center" {@attach reveal}>
      <p class="rv-focus text-eyebrow text-aegis-300 uppercase [--rv-i:0]">{translate('who.eyebrow')}</p>
      <h2
        id="who-title"
        class="mt-3 rv-focus font-display text-section font-light text-balance [--rv-i:1] md:text-section-lg"
      >
        {translate('who.title')}
      </h2>
      <p
        class="mx-auto mt-5 max-w-[540px] rv-focus text-lead leading-relaxed text-pretty text-ink-soft [--rv-i:2] md:text-lead-lg"
      >
        {translate('who.lead')}
      </p>
    </div>
    <div
      class="mt-14 grid grid-cols-[minmax(0,1fr)] gap-3 md:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] md:grid-rows-[repeat(5,auto)] md:gap-x-5"
    >
      <article
        class={[
          'glass rv-rise rounded-card [--rv-delay:180ms] [--rv-time:1.1s] md:[--rv-x:40px] md:[--rv-y:0px]',
          ARTICLE,
        ]}
        aria-labelledby="who-you"
        {@attach reveal}
      >
        <div class="flex items-center justify-between gap-3 px-1">
          <h3 id="who-you" class="flex items-center gap-3 text-title-sm font-medium text-ink">
            <IconTile icon="unlock" tone="current" size="small" />
            {translate('who.you.title')}
          </h3>
          <span
            class="rounded-full bg-aegis-500/12 px-2.5 py-1 text-micro font-medium whitespace-nowrap text-aegis-200 ring-1 ring-aegis-400/35"
            >{translate('who.you.badge')}</span
          >
        </div>
        <div class="rounded-pane p-4 well motion-safe:animate-who-read-message">
          <p class={LABEL}>{translate('who.label.message')}</p>
          <p class="mt-2 text-body text-ink">
            {translate('who.you.message')}
            <span class="rounded-xs bg-aegis-500/12 px-1 text-caption whitespace-nowrap text-aegis-200"
              >Lr7!vQ2-mT9#kd</span
            >
          </p>
          <p class="mt-1 text-body text-ink-soft">{translate('who.you.message.note')}</p>
        </div>
        <div class="rounded-pane p-4 well motion-safe:animate-who-read-file">
          <p class={LABEL}>{translate('who.label.file')}</p>
          <div class="mt-2 flex items-center gap-3">
            <IconTile icon="file" tone="current" size="medium" />
            <div class="min-w-0">
              <p class="truncate text-body text-ink">smlouva-final.pdf</p>
              <p class="text-caption text-ink-mute tabular-nums">{translate('who.you.file.size')}</p>
            </div>
          </div>
        </div>
        <div class="rounded-pane p-4 well motion-safe:animate-who-read-key">
          <p class={LABEL}>{translate('who.label.key')}</p>
          <p class="mt-2 text-body text-ink">{translate('who.you.key')}</p>
          <p class="mt-1 truncate text-caption text-ink-mute">
            nylzo.example/r#2.0.Q3tN0xvR8kLp2WcY5aHf1g<span class="rounded-xs bg-aegis-500/12 px-1 text-aegis-200"
              >.Vh2sXq9LmB4tRk7PzW1eNc…</span
            >
          </p>
        </div>
        <p class="flex items-center gap-2 px-1 text-caption text-ink-mute">
          <Icon name="check" class="size-4 shrink-0 text-aegis-300" />
          {translate('how.deleted')}
        </p>
      </article>
      <div
        class="relative flex rv-pop items-center justify-center py-1 [--rv-time:0.8s] md:row-span-5 md:w-36 md:py-0"
        aria-hidden="true"
        {@attach reveal}
      >
        <span
          class="pointer-events-none absolute top-[calc(50%-14px)] right-[calc(50%+30px)] -left-5 hidden h-px bg-linear-to-r from-white/6 to-white/24 md:block"
        >
          <span class="absolute inset-0 opacity-0 motion-safe:animate-who-flow-in">
            <span
              class="absolute top-1/2 left-0 h-[3px] w-12 -translate-x-1/2 -translate-y-1/2 rounded-full bg-linear-to-r from-transparent via-ink to-transparent blur-[0.5px]"
            ></span>
          </span>
        </span>
        <span
          class="pointer-events-none absolute top-[calc(50%-14px)] -right-5 left-[calc(50%+30px)] hidden h-px bg-linear-to-r from-aegis-400/60 to-aegis-400/15 md:block"
        >
          <span class="absolute inset-0 opacity-0 motion-safe:animate-who-flow-out">
            <span
              class="absolute top-1/2 left-0 h-[3px] w-12 -translate-x-1/2 -translate-y-1/2 rounded-full bg-linear-to-r from-transparent via-aegis-200 to-transparent blur-[0.5px]"
            ></span>
          </span>
        </span>
        <div class="flex flex-row items-center gap-3 md:flex-col">
          <span class="relative grid size-11 shrink-0 place-items-center">
            <span class="absolute inset-0 rounded-full bg-aegis-500/20 opacity-30 blur-lg motion-safe:animate-who-halo"
            ></span>
            <span
              class="absolute inset-0 rounded-full opacity-0 ring-1 ring-aegis-300/50 motion-safe:animate-who-ripple"
            ></span>
            <span
              class="relative grid size-11 place-items-center rounded-full text-white step-current motion-safe:animate-who-lock"
            >
              <Icon name="lock" class="size-5" stroke={1.7} />
            </span>
          </span>
          <span class="text-micro whitespace-nowrap text-ink-mute">{translate('who.encrypts')}</span>
        </div>
      </div>
      <article
        class={[
          'rv-focus rounded-pane bg-white/4 shadow-[inset_0_1px_0_oklch(1_0_0/0.06)] ring-1 ring-white/8 [--rv-blur:14px] [--rv-delay:440ms] [--rv-time:1.3s] [--rv-y:16px] md:[--rv-x:-40px] md:[--rv-y:0px]',
          ARTICLE,
        ]}
        aria-labelledby="who-server"
        {@attach reveal}
      >
        <div class="flex items-center justify-between gap-3 px-1">
          <h3 id="who-server" class="flex items-center gap-3 text-title-sm font-medium text-ink">
            <IconTile icon="database" tone="neutral" size="small" />
            <span>
              {translate('who.server.title')}
              <span class="block text-micro font-normal text-ink-mute">{translate('who.server.subtitle')}</span>
            </span>
          </h3>
          <span
            class="rounded-full bg-white/6 px-2.5 py-1 text-micro font-medium whitespace-nowrap text-ink-soft ring-1 ring-white/12"
            >{translate('who.server.badge')}</span
          >
        </div>
        <div class="rounded-pane p-4 well motion-safe:animate-who-store-message">
          <p class={LABEL}>{translate('who.label.message')}</p>
          <p
            class="mt-2 h-[4.5rem] overflow-hidden [mask-image:linear-gradient(to_bottom,transparent,black_16%,black_50%,transparent)] text-caption break-all text-ink-mute/60 motion-safe:animate-who-arrive-message"
            aria-hidden="true"
          >
            <span
              class="block before:content-[attr(data-cipher)] motion-safe:animate-who-drift"
              data-cipher={CIPHER_TEXT}
            ></span>
          </p>
          <p class="sr-only">{translate('who.server.badge')}</p>
        </div>
        <div class="rounded-pane p-4 well motion-safe:animate-who-store-file">
          <p class={LABEL}>{translate('who.label.file')}</p>
          <div class="mt-2 flex items-center gap-3 motion-safe:animate-who-arrive-file">
            <span class="relative grid size-10 shrink-0 place-items-center rounded-control text-ink-soft tile-neutral">
              <Icon name="file" class="size-5" stroke={1.7} />
              <span
                class="absolute -right-1.5 -bottom-1.5 grid size-[18px] place-items-center rounded-full bg-void-900 text-aegis-300 ring-1 ring-aegis-300/50"
              >
                <Icon name="lock" class="size-3" stroke={2.2} />
              </span>
            </span>
            <div class="min-w-0">
              <p class="truncate text-body text-ink-soft">{translate('who.server.file')}</p>
              <p class="text-caption text-solar-300 tabular-nums">{translate('who.server.file.size')}</p>
            </div>
          </div>
        </div>
        <div class="flex flex-col rounded-pane border border-dashed border-white/20 p-4 motion-safe:animate-who-deny">
          <p class={LABEL}>{translate('who.label.key')}</p>
          <p class="mt-2 flex items-center gap-2 text-body text-ink-soft">
            <Icon name="ban" class="size-4 shrink-0 text-aegis-300" />
            {translate('who.server.key')}
          </p>
        </div>
        <p class="flex items-center gap-2 px-1 text-caption text-ink-mute">
          <Icon name="eye" class="size-4 shrink-0 text-solar-300" />
          {translate('who.server.sees')}
        </p>
      </article>
    </div>
    <div class="mt-8 flex justify-center" {@attach reveal}>
      <a
        href="#resilience"
        class="group inline-block min-h-11 rv-focus rounded-control px-3 py-2.5 text-center text-body text-balance text-aegis-300 transition duration-300 ease-silk hover:text-aegis-200"
      >
        {translate('who.try')}
        <span class="whitespace-nowrap"
          >{translate('who.try.last')}<Icon
            name="caretDown"
            class="ml-1.5 inline-block size-3.5 align-[-2px] transition duration-300 ease-silk group-hover:translate-y-0.5"
            stroke={2}
          /></span
        >
      </a>
    </div>
  </div>
</section>
