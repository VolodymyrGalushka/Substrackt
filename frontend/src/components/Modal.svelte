<script lang="ts">
  import type { Snippet } from "svelte";
  let {
    open = false,
    title = "",
    onclose,
    children,
  }: { open?: boolean; title?: string; onclose: () => void; children: Snippet } = $props();
</script>

{#if open}
  <div class="modal-overlay" role="presentation" onclick={onclose}>
    <div
      class="modal"
      role="dialog"
      aria-modal="true"
      aria-label={title}
      tabindex="-1"
      onclick={(event) => event.stopPropagation()}
      onkeydown={(event) => event.stopPropagation()}
    >
      <div class="modal-head">
        <h2>{title}</h2>
        <button class="modal-close" onclick={onclose} aria-label="Close">×</button>
      </div>
      {@render children()}
    </div>
  </div>
{/if}
