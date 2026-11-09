<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    readonly text: string;
    readonly placeholder: string;
    readonly children: Snippet;
  }

  const { text, placeholder, children }: Props = $props();
  const [before, after = ''] = $derived(text.split(`{${placeholder}}`, 2));
</script>

{before}{@render children()}{after}
