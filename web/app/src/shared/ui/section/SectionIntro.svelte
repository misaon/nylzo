<script lang="ts">
  interface Props {
    readonly id: string;
    readonly eyebrow: string;
    readonly title: string;
  }

  const { id, eyebrow, title }: Props = $props();
</script>

<p class="reveal-focus text-eyebrow text-aegis-300 uppercase [--reveal-index:0]">{eyebrow}</p>
<h2
  {id}
  class="mt-3 reveal-focus font-display text-section font-light text-balance [--reveal-index:1] md:text-section-lg"
>
  {title}
</h2>
