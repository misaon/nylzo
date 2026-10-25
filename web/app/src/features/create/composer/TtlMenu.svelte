<script lang="ts">
  import type { Composer } from './composer.svelte.ts';

  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Icon from '../../../shared/ui/Icon.svelte';
  import Popover from '../../../shared/ui/Popover.svelte';
  import MenuChip from './MenuChip.svelte';

  interface Props {
    readonly composer: Composer;
  }

  const { composer }: Props = $props();
  const { translate } = getTranslator();
  const id = $props.id();

  function choose(seconds: number): void {
    composer.ttlSeconds = seconds;
    composer.closeMenu('ttl');
  }
</script>

<div class="sm:relative" data-menu>
  <MenuChip
    {composer}
    menu="ttl"
    popoverId="{id}-ttl"
    icon="clock"
    label={composer.ttlLabel(composer.ttlSeconds)}
    ariaLabel={translate('compose.ttl.label', { ttl: composer.ttlLabel(composer.ttlSeconds) })}
    problem={composer.problemOf.ttl !== undefined}
  />
  <Popover
    id="{id}-ttl"
    label={translate('compose.ttl.title')}
    open={composer.menu === 'ttl'}
    onclose={() => composer.closeMenu('ttl')}
    class="inset-x-0 top-full mt-2 p-1 sm:inset-x-auto sm:left-0 sm:w-56"
  >
    <p class="px-2.5 pt-1.5 pb-1 text-[11px] text-ink-mute">{translate('compose.ttl.title')}</p>
    {#each composer.ttlOptions as seconds (seconds)}
      <button
        type="button"
        aria-pressed={composer.ttlSeconds === seconds}
        onclick={() => choose(seconds)}
        class={[
          'flex h-11 w-full items-center justify-between rounded-[7px] px-2.5 text-[13px] hover:bg-white/6 md:h-8',
          composer.ttlSeconds === seconds ? 'text-ink' : 'text-ink-soft',
        ]}
      >
        {composer.ttlLabel(seconds)}
        <Icon
          name="check"
          stroke={2.2}
          class={[
            'size-3.5 text-aegis-300 transition-opacity duration-300',
            composer.ttlSeconds === seconds ? 'opacity-100' : 'opacity-0',
          ]}
        />
      </button>
    {/each}
    <p class="px-2.5 pt-1.5 pb-1.5 text-[11px] leading-relaxed text-ink-mute">
      {translate('compose.ttl.note')}
    </p>
  </Popover>
</div>
