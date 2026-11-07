<script lang="ts">
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import Button from '../../shared/ui/button/Button.svelte';
  import Icon from '../../shared/ui/icon/Icon.svelte';
  import StatusNote from './StatusNote.svelte';
  import StatusNumeral from './StatusNumeral.svelte';

  const { translate } = getTranslator();
  const id = $props.id();
</script>

<section class="w-full px-4 pt-10 pb-20 sm:px-6 md:pb-24" aria-labelledby="{id}-title">
  <div class="mx-auto flex max-w-[720px] flex-col items-center text-center">
    <p class="sr-only">{translate('error.server.label')}</p>
    <StatusNumeral first="5" last="0" tone="danger" glyph="server" />
    <h1
      id="{id}-title"
      class="mt-8 animate-rise font-display text-page font-light text-balance text-ink [animation-delay:80ms] sm:mt-10 md:text-page-lg"
    >
      {translate('error.server.title')}
    </h1>
    <p
      class="mx-auto mt-4 max-w-[520px] animate-rise text-lead text-pretty text-ink-soft [animation-delay:160ms] md:text-lead-lg"
    >
      {translate('error.server.lead')}
    </p>
    <div class="mt-9 flex w-full animate-rise justify-center [animation-delay:240ms]">
      <Button variant="primary" size="large" href="" class="w-full sm:w-auto">
        <Icon name="refresh" class="size-4 shrink-0" animated />
        <span class="relative">{translate('error.server.reload')}</span>
      </Button>
    </div>
    <div class="mt-10 animate-rise [animation-delay:320ms]">
      <StatusNote icon="shieldCheck" text={translate('error.server.keys')} />
    </div>
  </div>
</section>
