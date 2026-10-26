<script lang="ts">
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Icon from '../../../shared/ui/Icon.svelte';

  const { translate } = getTranslator();
</script>

<section class="px-4 pt-12 pb-32 sm:px-6" aria-labelledby="cta-title">
  <div class="mx-auto flex max-w-[760px] reveal-zoom flex-col items-center text-center">
    <h2
      id="cta-title"
      class="font-display text-[30px] leading-[1.15] font-light tracking-[-0.025em] text-balance md:text-[48px]"
    >
      {translate('cta.title')}
    </h2>
    <p class="mt-5 text-[17px] leading-relaxed text-ink-soft">{translate('cta.text')}</p>
    <a
      href="#create"
      class="btn-primary mt-9 flex h-12 hover-lift items-center gap-2.5 rounded-control px-7 text-[15px] font-medium"
    >
      <span class="sheen" aria-hidden="true"></span>
      <Icon name="lock" stroke={1.9} class="size-[18px]" />
      {translate('cta.button')}
    </a>
  </div>
</section>
