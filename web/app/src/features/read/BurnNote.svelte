<script lang="ts">
  import Icon from '../../shared/ui/icon/Icon.svelte';
  import { lingering } from '../../shared/ui/transition/lingering.ts';
  import type { ReadPage } from './read-page.svelte.ts';

  interface Props {
    readonly page: ReadPage;
  }

  const { page }: Props = $props();
  const keepBurnNote = lingering();
  const burnNote = $derived(keepBurnNote(page.burnNote));
</script>

<p
  class={[
    'mt-4 flex min-h-[3lh] items-start justify-center gap-2 px-1 text-center text-caption fade-swap max-sm:block max-sm:text-balance',
    page.burnFailed ? 'text-rose-200' : 'text-ink-soft',
    page.burnNote ? 'opacity-100' : 'invisible opacity-0',
  ]}
>
  <Icon
    name={page.burnFailed ? 'alert' : page.deleting ? 'key' : 'check'}
    stroke={1.7}
    class="size-4 shrink-0 max-sm:mr-1.5 max-sm:inline-block max-sm:align-[-3px]"
  />{` ${burnNote}`}
</p>
