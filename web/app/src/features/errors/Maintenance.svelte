<script lang="ts">
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import Button from '../../shared/ui/button/Button.svelte';
  import Icon from '../../shared/ui/icon/Icon.svelte';
  import { MAINTENANCE_RELOAD_SECONDS } from './status-page.ts';
  import StatusNote from './StatusNote.svelte';
  import StatusOrb from './StatusOrb.svelte';

  const { translate } = getTranslator();
  const id = $props.id();
</script>

<section class="w-full px-4 pt-14 pb-20 sm:px-6 md:pb-24" aria-labelledby="{id}-title">
  <div class="mx-auto flex max-w-[720px] flex-col items-center text-center">
    <div class="animate-rise">
      <StatusOrb tone="warning" glyph="wrench" orbit class="size-[104px] sm:size-[128px]" />
    </div>
    <p
      class="mt-12 inline-flex animate-rise items-center gap-2 rounded-full bg-[oklch(0.66_0.13_75/0.14)] px-3 py-1 text-caption font-medium text-[oklch(0.9_0.08_85)] ring-1 ring-[oklch(0.82_0.12_80/0.35)] [animation-delay:60ms]"
    >
      <span class="relative grid size-2 place-items-center" aria-hidden="true">
        <span class="absolute inset-0 rounded-full bg-[oklch(0.84_0.14_82)] motion-safe:animate-ping"></span>
        <span class="relative size-2 rounded-full bg-[oklch(0.84_0.14_82)]"></span>
      </span>
      {translate('maintenance.badge')}
    </p>
    <h1
      id="{id}-title"
      class="mt-5 animate-rise font-display text-page font-light text-balance text-ink [animation-delay:120ms] md:text-page-lg"
    >
      {translate('maintenance.title')}
    </h1>
    <p
      class="mx-auto mt-4 max-w-[520px] animate-rise text-lead text-pretty text-ink-soft [animation-delay:180ms] md:text-lead-lg"
    >
      {translate('maintenance.lead')}
    </p>
    <div class="mt-9 w-full max-w-[540px] animate-rise [animation-delay:240ms]">
      <div class="glass rounded-card p-4 text-left sm:p-5">
        <div class="flex items-center gap-4 max-sm:flex-col max-sm:items-stretch">
          <div class="min-w-0 flex-1">
            <p class="text-body-sm font-medium text-ink">
              {translate('maintenance.reload', { seconds: MAINTENANCE_RELOAD_SECONDS })}
            </p>
            <div
              class="mt-3 h-1.5 overflow-hidden rounded-full bg-white/8 ring-1 ring-white/6 motion-reduce:invisible"
              aria-hidden="true"
            >
              <div
                class="h-full origin-left rounded-full bg-linear-to-r from-[oklch(0.62_0.13_70)] to-[oklch(0.9_0.1_88)] shadow-[0_0_10px_oklch(0.82_0.14_80/0.8)] motion-safe:animate-retry"
              ></div>
            </div>
          </div>
          <Button variant="tint" href="">
            <Icon name="refresh" class="size-4 shrink-0" animated />
            <span>{translate('maintenance.retry')}</span>
          </Button>
        </div>
      </div>
    </div>
    <div class="mt-7 flex animate-rise flex-col items-center gap-2 [animation-delay:320ms]">
      <StatusNote icon="lock" text={translate('maintenance.encrypted')} />
      <StatusNote icon="clock" text={translate('maintenance.expiry')} />
    </div>
  </div>
</section>
