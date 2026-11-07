<script lang="ts">
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Button from '../../../shared/ui/button/Button.svelte';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import { reveal } from '../../../shared/ui/reveal/reveal.ts';

  const { translate } = getTranslator();
</script>

<section class="px-4 pt-12 pb-32 sm:px-6" aria-labelledby="cta-title">
  <div class="mx-auto flex max-w-[760px] flex-col items-center text-center" {@attach reveal}>
    <h2
      id="cta-title"
      class="reveal-focus font-display text-hero font-light text-balance [--reveal-blur:14px] [--reveal-duration:1.2s] [--reveal-y:18px] md:text-hero-lg"
    >
      {translate('cta.title')}
    </h2>
    <p class="mt-5 reveal-focus text-lead text-ink-soft [--reveal-delay:220ms] md:text-lead-lg">
      {translate('cta.text')}
    </p>
    <Button variant="primary" size="large" href="#create" class="mt-9 reveal-pop [--reveal-delay:440ms]">
      <Icon name="lock" class="size-3.5 opacity-70" stroke={2} animated />
      {translate('cta.button')}
    </Button>
  </div>
</section>
