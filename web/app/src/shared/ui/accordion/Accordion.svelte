<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { Attachment } from 'svelte/attachments';
  import type { ClassValue } from 'svelte/elements';

  import Icon from '../icon/Icon.svelte';

  interface Props {
    readonly summary: string;
    readonly class?: ClassValue | undefined;
    readonly attachment?: Attachment<HTMLElement>;
    readonly children: Snippet;
  }

  const { summary, class: className, attachment, children }: Props = $props();
</script>

<details class={['group glass-soft accordion rounded-card', className]} {@attach attachment}>
  <summary
    class="flex cursor-pointer list-none items-center justify-between gap-4 p-6 text-title-sm text-ink transition select-none hover:text-aegis-200 [&::-webkit-details-marker]:hidden"
  >
    {summary}
    <Icon name="chevronDown" class="size-4 shrink-0 text-ink-mute transition duration-500 group-open:rotate-180" />
  </summary>
  <p class="px-6 pb-6 text-body-sm text-pretty text-ink-soft">{@render children()}</p>
</details>
