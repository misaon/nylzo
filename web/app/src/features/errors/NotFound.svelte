<script lang="ts">
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import Button from '../../shared/ui/button/Button.svelte';
  import Icon from '../../shared/ui/icon/Icon.svelte';
  import IconTile from '../../shared/ui/tile/IconTile.svelte';
  import StatusNumeral from './StatusNumeral.svelte';

  const { translate } = getTranslator();
  const id = $props.id();
</script>

<svelte:head>
  <title>{translate('title.notFound')}</title>
</svelte:head>

<section class="px-4 pt-14 pb-20 sm:px-6 md:pt-20 md:pb-28" aria-labelledby="{id}-title">
  <div class="mx-auto flex max-w-[720px] flex-col items-center text-center">
    <p class="sr-only">{translate('error.notFound.label')}</p>
    <StatusNumeral first="4" last="4" tone="current" glyph="compass" />
    <h1
      id="{id}-title"
      class="mt-8 animate-rise font-display text-page font-light text-balance text-ink [animation-delay:80ms] sm:mt-10 md:text-page-lg"
    >
      {translate('error.notFound.title')}
    </h1>
    <p
      class="mx-auto mt-4 max-w-[520px] animate-rise text-lead text-pretty text-ink-soft [animation-delay:160ms] md:text-lead-lg"
    >
      {translate('error.notFound.lead')}
    </p>
    <div class="mt-9 flex w-full animate-rise justify-center [animation-delay:240ms]">
      <Button variant="primary" size="large" href="/" class="w-full sm:w-auto">
        <Icon name="pen" class="size-4 shrink-0" animated />
        <span class="relative">{translate('nav.write')}</span>
      </Button>
    </div>
    <div
      class="mt-10 flex w-full max-w-[540px] animate-rise items-center gap-3.5 rounded-pane bg-white/4 px-4 py-3.5 text-left shadow-[inset_0_1px_0_oklch(1_0_0/0.06)] ring-1 ring-white/10 [animation-delay:320ms]"
    >
      <IconTile icon="link" tone="neutral" size="medium" stroke={1.6} />
      <p class="text-body-sm text-pretty text-ink-soft">
        <span class="font-medium text-ink">{translate('error.notFound.link.title')}</span>
        {translate('error.notFound.link.text')}
      </p>
    </div>
  </div>
</section>
