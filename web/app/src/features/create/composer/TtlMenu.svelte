<script lang="ts">
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import type { Composer } from './composer.svelte.ts';
  import ComposerMenu from './ComposerMenu.svelte';

  interface Props {
    readonly composer: Composer;
  }

  const { composer }: Props = $props();
  const { translate } = getTranslator();
  const title = $derived(translate('compose.ttl.title'));
  const LENGTHS = ['w-1/4', 'w-1/2', 'w-3/4', 'w-full'] as const;

  function choose(seconds: number): void {
    composer.ttlSeconds = seconds;
    composer.closeMenu('ttl');
  }
</script>

<ComposerMenu
  {composer}
  menu="ttl"
  icon="clock"
  label={composer.ttlLabel(composer.ttlSeconds)}
  hint={title}
  {title}
  problem={composer.problemOf.ttl !== undefined}
  class="sm:w-64"
>
  <p class="px-3 pt-1.5 pb-1 text-caption text-ink-mute">{title}</p>
  <div class="grid gap-0.5">
    {#each composer.ttlOptions as seconds, index (seconds)}
      {const selected = $derived(composer.ttlSeconds === seconds)}
      <button
        type="button"
        aria-pressed={selected}
        onclick={() => choose(seconds)}
        class={[
          'flex h-11 w-full items-center gap-3 rounded-control px-3 text-ui transition md:h-10',
          selected
            ? 'bg-aegis-500/12 text-ink shadow-rim ring-1 ring-aegis-400/35'
            : 'text-ink-soft hover:bg-white/6 hover:text-ink',
        ]}
      >
        <span class="flex-1 text-left">{composer.ttlLabel(seconds)}</span>
        <span class="h-1 w-14 shrink-0 overflow-hidden rounded-full bg-white/10" aria-hidden="true">
          <span
            class={[
              'block h-full rounded-full',
              LENGTHS[index],
              selected ? 'bg-linear-to-r from-aegis-500 to-aegis-300 shadow-glow' : 'bg-white/25',
            ]}
          ></span>
        </span>
        {#if selected}
          <Icon name="check" class="size-4 shrink-0 text-aegis-200 drop-shadow-[0_0_6px_oklch(0.86_0.06_246/0.7)]" />
        {:else}
          <span class="size-4 shrink-0" aria-hidden="true"></span>
        {/if}
      </button>
    {/each}
  </div>
</ComposerMenu>
