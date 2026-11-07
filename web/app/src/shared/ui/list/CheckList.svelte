<script lang="ts">
  import type { MessageKey } from '../../language/czech.ts';
  import { getTranslator } from '../../language/translator.svelte.ts';
  import Icon from '../icon/Icon.svelte';

  interface Props {
    readonly items: readonly MessageKey[];
  }

  const { items }: Props = $props();
  const { translate } = getTranslator();
</script>

<ul role="list" class="mt-4 grid gap-2.5 text-body-sm text-pretty text-ink-soft">
  {#each items as item (item)}
    <li class="flex gap-2.5">
      <Icon name="check" class="mt-0.5 size-4 shrink-0 text-aegis-300" stroke={2} />
      <span>{translate(item)}</span>
    </li>
  {/each}
</ul>
