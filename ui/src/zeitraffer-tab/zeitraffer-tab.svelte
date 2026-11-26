<script lang="ts">
  import { Badge, Button, ConfirmDialog, EmptyState } from '@prusa/connect-ui';
  import { formatBytes, formatNumber } from '@prusa/connect-ui/i18n';

  import {
    contentUrl,
    deleteRun,
    describeRun,
    formatStarted,
    listRuns,
    outcomeKey,
    previewUrl,
    readStatus,
    type RecorderStatus,
    type Run,
  } from '../api';
  import { heldHost } from '../host';

  interface Props {
    deviceId: string;
  }

  let { deviceId }: Props = $props();

  const host = heldHost();
  const t = host.t;

  let status = $state<RecorderStatus | undefined>(undefined);
  let runs = $state<Run[]>([]);
  let failure = $state('');
  let stamp = $state(Date.now());
  let playing = $state('');
  let loadedFolder = '';

  const refreshRuns = async (signal?: AbortSignal): Promise<void> => {
    if (!status?.folder || !status.this_printer) return;
    try {
      runs = await listRuns(host, status.storage, status.folder, signal);
      loadedFolder = status.folder;
    } catch (error) {
      if (!signal?.aborted) failure = t('tab.readFailed', { error: String(error) });
    }
  };

  $effect(() => {
    const controller = new AbortController();
    let lastVideo = '';

    const poll = async (): Promise<void> => {
      try {
        status = await readStatus(host, deviceId, controller.signal);
        stamp = Date.now();
        failure = '';
        if (status.folder && (status.folder !== loadedFolder || status.last_video !== lastVideo)) {
          lastVideo = status.last_video ?? '';
          await refreshRuns(controller.signal);
        }
      } catch (error) {
        if (!controller.signal.aborted) failure = String(error);
      }
    };

    void poll();
    const timer = setInterval(() => void poll(), 5000);

    return () => {
      clearInterval(timer);
      controller.abort();
    };
  });

  const remove = async (run: Run): Promise<void> => {
    if (!status) return;
    try {
      await deleteRun(host, status.storage, run.path);
      runs = runs.filter((r) => r.path !== run.path);
    } catch (error) {
      failure = t('tab.deleteFailed', { error: String(error) });
    }
  };

  const size = (bytes: number): string => (bytes > 0 ? formatBytes(bytes) : '');
</script>

<div class="flex flex-col gap-6">
  {#if failure}
    <p class="text-sm text-destructive">{failure}</p>
  {/if}

  {#if status === undefined}
    <p class="text-sm text-muted-foreground">{t('tab.loading')}</p>
  {:else if !status.configured}
    <EmptyState
      headingLevel="h2"
      heading={t('tab.noCamera.heading')}
      description={t('tab.noCamera.description')}
    />
  {:else if !status.this_printer && !status.device_id}
    <!-- The module answers without a printer when the person may not observe it. -->
    <EmptyState
      headingLevel="h2"
      heading={t('tab.hidden.heading')}
      description={t('tab.hidden.description')}
    />
  {:else if !status.this_printer}
    <EmptyState
      headingLevel="h2"
      heading={t('tab.otherPrinter.heading')}
      description={t('tab.otherPrinter.description', {
        printer: status.device_name ?? status.device_id ?? '',
      })}
    />
  {:else}
    <section class="flex flex-col gap-3">
      <div class="flex flex-wrap items-center gap-2">
        <h2 class="text-sm font-semibold">{t('tab.recording.heading')}</h2>
        {#if status.recording}
          <Badge variant="stateActive">{t('tab.recording.running')}</Badge>
        {:else if (status.rendering ?? 0) > 0}
          <Badge variant="info">{t('tab.recording.rendering')}</Badge>
        {:else}
          <Badge variant="stateIdle">{t('tab.recording.waiting')}</Badge>
        {/if}
      </div>

      {#if status.recording}
        <p class="text-sm">
          {t('tab.recording.progress', { job: status.job ?? '', layer: status.layer ?? 0 })}
          {#if status.z}{t('tab.recording.height', {
              z: formatNumber(status.z, { minimumFractionDigits: 2, maximumFractionDigits: 2 }),
            })}{/if}
          · {t('tab.recording.frames', { kept: status.kept ?? 0, inLayer: status.frames_in_layer ?? 0 })}
        </p>
      {/if}

      {#if status.ffmpeg_problem}
        <p class="text-sm text-warning">
          {t('tab.ffmpegMissing', { problem: status.ffmpeg_problem })}
        </p>
      {/if}

      {#if status.last_error}
        <p class="text-sm text-warning">{t('tab.lastError', { error: status.last_error })}</p>
      {/if}

      {#if status.recording || status.last_video}
        <img
          class="w-full max-w-xl rounded-md border border-border"
          src={previewUrl(host, stamp)}
          alt={t('tab.preview.alt')}
          onerror={(event) => ((event.currentTarget as HTMLImageElement).style.display = 'none')}
          onload={(event) => ((event.currentTarget as HTMLImageElement).style.display = '')}
        />
      {/if}
    </section>

    <section class="flex flex-col gap-3">
      <div class="flex items-center justify-between gap-2">
        <h2 class="text-sm font-semibold">{t('runs.heading')}</h2>
        <Button variant="ghost" size="sm" onclick={() => void refreshRuns()}>{t('runs.refresh')}</Button>
      </div>

      {#if runs.length === 0}
        <p class="text-sm text-muted-foreground">{t('runs.empty')}</p>
      {:else}
        <ul class="flex flex-col gap-4">
          {#each runs as run (run.path)}
            {@const described = describeRun(run.name)}
            <li class="flex flex-col gap-2 rounded-md border border-border p-3">
              <div class="flex flex-wrap items-center gap-2">
                <span class="text-sm font-medium">{described.title}</span>
                <span class="text-xs text-muted-foreground">
                  {run.started ? formatStarted(run.started) || described.when : described.when}
                </span>
                {#if outcomeKey(run.outcome)}
                  <Badge variant={run.outcome === 'finished' ? 'success' : 'warning'}>
                    {t(outcomeKey(run.outcome))}
                  </Badge>
                {/if}
                {#if run.layers}<span class="text-xs text-muted-foreground">{t('runs.layers', { count: run.layers })}</span>{/if}
                {#if run.size}<span class="text-xs text-muted-foreground">{size(run.size)}</span>{/if}
                {#if run.diagnosed}
                  <span class="text-xs text-muted-foreground">{t('runs.diagnosed', { count: run.diagnosed })}</span>
                {/if}
              </div>

              {#if playing === run.path}
                <!-- svelte-ignore a11y_media_has_caption -->
                <video
                  class="w-full max-w-xl rounded-md"
                  controls
                  autoplay
                  preload="metadata"
                  poster={contentUrl(status.storage, `${run.path}/vorschau.jpg`)}
                  src={contentUrl(status.storage, `${run.path}/zeitraffer.mp4`)}
                ></video>
              {:else}
                <button
                  type="button"
                  class="w-full max-w-xs cursor-pointer"
                  onclick={() => (playing = run.path)}
                  aria-label={t('runs.play.label')}
                >
                  <img
                    class="w-full rounded-md"
                    loading="lazy"
                    src={contentUrl(status.storage, `${run.path}/vorschau.jpg`)}
                    alt={t('runs.preview.alt', { title: described.title })}
                  />
                </button>
              {/if}

              <div class="flex flex-wrap gap-2">
                <Button variant="secondary" size="sm" onclick={() => (playing = run.path)}>{t('runs.play')}</Button>
                <a
                  class="text-sm text-link underline-offset-4 hover:underline self-center"
                  href={contentUrl(status.storage, `${run.path}/zeitraffer.mp4`)}
                  download={`${run.name}.mp4`}>{t('runs.download')}</a
                >
                <ConfirmDialog
                  title={t('runs.delete.title')}
                  description={t('runs.delete.description', { title: described.title, when: described.when })}
                  confirmLabel={t('runs.delete')}
                  variant="destructive"
                  onConfirm={() => void remove(run)}
                >
                  {#snippet trigger({ props })}
                    <Button {...props} variant="ghost" size="sm">{t('runs.delete')}</Button>
                  {/snippet}
                </ConfirmDialog>
              </div>
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  {/if}
</div>
