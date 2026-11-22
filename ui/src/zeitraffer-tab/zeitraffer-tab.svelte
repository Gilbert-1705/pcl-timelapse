<script lang="ts">
  import { Badge, Button, ConfirmDialog, EmptyState } from '@prusa/connect-ui';

  import {
    contentUrl,
    deleteRun,
    describeRun,
    formatStarted,
    listRuns,
    outcomeLabel,
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
      if (!signal?.aborted) failure = `Die Zeitraffer konnten nicht gelesen werden: ${String(error)}`;
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
      failure = `Löschen fehlgeschlagen: ${String(error)}`;
    }
  };

  const megabytes = (bytes: number): string =>
    bytes > 0 ? `${(bytes / 1024 / 1024).toFixed(1)} MB` : '';
</script>

<div class="flex flex-col gap-6">
  {#if failure}
    <p class="text-sm text-destructive">{failure}</p>
  {/if}

  {#if status === undefined}
    <p class="text-sm text-muted-foreground">Lade …</p>
  {:else if !status.configured}
    <EmptyState
      headingLevel="h2"
      heading="Keine Kamera eingerichtet"
      description="Trage unter Einstellungen → Erweiterungen → Zeitraffer die Adresse der Kamera ein (camera_url, z. B. rtsp://192.0.2.50/live)."
    />
  {:else if !status.this_printer}
    <EmptyState
      headingLevel="h2"
      heading="Die Kamera filmt einen anderen Drucker"
      description={`Zeitraffer werden für ${status.device_name ?? status.device_id} aufgenommen.`}
    />
  {:else}
    <section class="flex flex-col gap-3">
      <div class="flex flex-wrap items-center gap-2">
        <h2 class="text-sm font-semibold">Aufnahme</h2>
        {#if status.recording}
          <Badge variant="stateActive">läuft</Badge>
        {:else if (status.rendering ?? 0) > 0}
          <Badge variant="info">Video wird erstellt</Badge>
        {:else}
          <Badge variant="stateIdle">wartet auf den nächsten Druck</Badge>
        {/if}
      </div>

      {#if status.recording}
        <p class="text-sm">
          {status.job} · Schicht {status.layer}
          {#if status.z}(Z {status.z.toFixed(2)} mm){/if}
          · {status.kept ?? 0} Bilder ausgewählt · {status.frames_in_layer ?? 0} Bilder in der laufenden Schicht
        </p>
      {/if}

      {#if status.ffmpeg_problem}
        <p class="text-sm text-warning">
          ffmpeg fehlt oder startet nicht, deshalb wird nichts aufgenommen. Installiere ffmpeg auf dem
          Server oder trage unter Einstellungen → Erweiterungen → Zeitraffer bei „ffmpeg“ den vollen Pfad
          ein. Details: {status.ffmpeg_problem}
        </p>
      {/if}

      {#if status.last_error}
        <p class="text-sm text-warning">Zuletzt: {status.last_error}</p>
      {/if}

      {#if status.recording || status.last_video}
        <img
          class="w-full max-w-xl rounded-md border border-border"
          src={previewUrl(host, stamp)}
          alt="Zuletzt ausgewähltes Bild"
          onerror={(event) => ((event.currentTarget as HTMLImageElement).style.display = 'none')}
          onload={(event) => ((event.currentTarget as HTMLImageElement).style.display = '')}
        />
      {/if}
    </section>

    <section class="flex flex-col gap-3">
      <div class="flex items-center justify-between gap-2">
        <h2 class="text-sm font-semibold">Fertige Zeitraffer</h2>
        <Button variant="ghost" size="sm" onclick={() => void refreshRuns()}>Aktualisieren</Button>
      </div>

      {#if runs.length === 0}
        <p class="text-sm text-muted-foreground">
          Noch keiner. Der nächste Druck wird automatisch aufgenommen.
        </p>
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
                {#if run.outcome}
                  <Badge variant={run.outcome === 'finished' ? 'success' : 'warning'}>
                    {outcomeLabel(run.outcome)}
                  </Badge>
                {/if}
                {#if run.layers}<span class="text-xs text-muted-foreground">{run.layers} Schichten</span>{/if}
                {#if run.size}<span class="text-xs text-muted-foreground">{megabytes(run.size)}</span>{/if}
                {#if run.diagnosed}
                  <span class="text-xs text-muted-foreground">Diagnose: {run.diagnosed} Schichten</span>
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
                  aria-label="Zeitraffer abspielen"
                >
                  <img
                    class="w-full rounded-md"
                    loading="lazy"
                    src={contentUrl(status.storage, `${run.path}/vorschau.jpg`)}
                    alt={`Vorschau ${described.title}`}
                  />
                </button>
              {/if}

              <div class="flex flex-wrap gap-2">
                <Button variant="secondary" size="sm" onclick={() => (playing = run.path)}>Abspielen</Button>
                <a
                  class="text-sm text-link underline-offset-4 hover:underline self-center"
                  href={contentUrl(status.storage, `${run.path}/zeitraffer.mp4`)}
                  download={`${run.name}.mp4`}>Herunterladen</a
                >
                <ConfirmDialog
                  title="Zeitraffer löschen?"
                  description={`„${described.title}“ vom ${described.when} wird mit Video und Vorschau gelöscht.`}
                  confirmLabel="Löschen"
                  variant="destructive"
                  onConfirm={() => void remove(run)}
                >
                  {#snippet trigger({ props })}
                    <Button {...props} variant="ghost" size="sm">Löschen</Button>
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
