<script lang="ts">
  import { readStatus, type RecorderStatus } from '../api';
  import { heldHost } from '../host';

  interface Props {
    deviceId: string;
  }

  let { deviceId }: Props = $props();

  const host = heldHost();
  const t = host.t;
  let status = $state<RecorderStatus | undefined>(undefined);

  $effect(() => {
    const controller = new AbortController();
    const poll = async (): Promise<void> => {
      try {
        status = await readStatus(host, deviceId, controller.signal);
      } catch {
        status = undefined;
      }
    };

    void poll();
    const timer = setInterval(() => void poll(), 10000);

    return () => {
      clearInterval(timer);
      controller.abort();
    };
  });
</script>

{#if status?.configured && status.this_printer}
  <p class="text-sm">
    {#if status.recording}
      {t('section.recording', { layer: status.layer ?? 0, kept: status.kept ?? 0 })}
    {:else if (status.rendering ?? 0) > 0}
      {t('section.rendering')}
    {:else}
      {t('section.ready')}
    {/if}
  </p>
{:else if status && !status.configured}
  <p class="text-sm text-muted-foreground">{t('section.noCamera')}</p>
{/if}
