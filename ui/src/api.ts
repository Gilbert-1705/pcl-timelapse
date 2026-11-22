import type { ModuleHost } from '@prusa/connect-ui/slots';

/** Was GET /status des Moduls antwortet. */
export interface RecorderStatus {
  configured: boolean;
  device_id?: string;
  device_name?: string;
  this_printer: boolean;
  storage: string;
  folder?: string;
  recording: boolean;
  job?: string;
  started?: string;
  layer?: number;
  z?: number;
  kept?: number;
  frames_in_layer?: number;
  last_frame_at?: string;
  rendering?: number;
  last_video?: string;
  last_error?: string;
  /** Erste Zeile von `ffmpeg -version`. */
  ffmpeg?: string;
  /** Warum ffmpeg nicht läuft. Ohne ffmpeg wird nichts aufgenommen. */
  ffmpeg_problem?: string;
}

/** Ein fertiger Zeitraffer: ein Ordner im Speicher. */
export interface Run {
  path: string;
  name: string;
  modified: string;
  size: number;
  outcome?: string;
  layers?: number;
  /** Druckstart aus info.json, ISO-Zeit. */
  started?: string;
  /** Schichten mit Diagnose-Bildern. */
  diagnosed?: number;
}

// Die App lädt dieses Bündel von /api/v1/modules/<namespace>/ui/ui/index.js.
// Die eigenen Routen liegen unter /api/v1/modules/<namespace>/. Aus der eigenen
// Adresse abgeleitet, weil ModuleHost.namespace erst nach v0.1.0-beta.1 kam.
const moduleBase = ((): string => {
  const path = new URL(import.meta.url).pathname;
  const cut = path.indexOf('/ui/');

  return cut > 0 ? path.slice(0, cut) : path.replace(/\/[^/]*$/, '');
})();

// eslint-disable-next-line @typescript-eslint/no-unused-vars
const base = (_host: ModuleHost): string => moduleBase;

export const readStatus = async (
  host: ModuleHost,
  deviceId: string,
  signal?: AbortSignal,
): Promise<RecorderStatus> => {
  const response = await fetch(
    `${base(host)}/status?device=${encodeURIComponent(deviceId)}`,
    { credentials: 'same-origin', signal: signal ?? null },
  );
  if (!response.ok) throw new Error(`Status: ${response.status}`);

  return (await response.json()) as RecorderStatus;
};

export const previewUrl = (host: ModuleHost, stamp: number): string =>
  `${base(host)}/preview?t=${stamp}`;

export const contentUrl = (storage: string, path: string): string =>
  `/api/v1/storages/${encodeURIComponent(storage)}/content?path=${encodeURIComponent(path)}`;

interface Entry {
  folder: boolean;
  name: string;
  path: string;
  modified: string;
  size?: number;
}

/** Die Zeitraffer eines Druckers, neueste zuerst. */
export const listRuns = async (
  host: ModuleHost,
  storage: string,
  folder: string,
  signal?: AbortSignal,
): Promise<Run[]> => {
  let entries: Entry[];

  try {
    const page = (await host.client.listStorageFiles(
      { path: { storage }, query: { path: folder } },
      signal,
    )) as unknown as { items?: Entry[] };
    entries = page.items ?? [];
  } catch (error) {
    // Ein Ordner, den es noch nicht gibt: noch kein Zeitraffer.
    // Ein Ordner, den es noch nicht gibt, antwortet 404 oder core.storage/INVALID_PATH.
    // Ohne instanceof: das Bündel und die App haben je eine eigene Kopie der Fehlerklasse.
    const failure = error as { status?: number; code?: string };
    if (failure.status === 404 || failure.code === 'core.storage/INVALID_PATH') return [];
    throw error;
  }

  const runs = entries
    .filter((entry) => entry.folder)
    .sort((a, b) => b.name.localeCompare(a.name))
    .map((entry) => ({
      path: entry.path,
      name: entry.name,
      modified: entry.modified,
      size: 0,
    }));

  await Promise.all(
    runs.slice(0, 30).map(async (run: Run) => {
      try {
        const response = await fetch(contentUrl(storage, `${run.path}/info.json`), {
          credentials: 'same-origin',
          signal: signal ?? null,
        });
        if (!response.ok) return;
        const info = (await response.json()) as {
          ergebnis?: string;
          schichten?: number;
          gestartet?: string;
          video_bytes?: number;
          diagnose?: number;
        };
        if (info.ergebnis !== undefined) run.outcome = info.ergebnis;
        if (info.schichten !== undefined) run.layers = info.schichten;
        if (info.gestartet !== undefined) run.started = info.gestartet;
        if (info.diagnose) run.diagnosed = info.diagnose;
        // Die Größe eines Ordners im Listing ist nicht die des Videos.
        run.size = info.video_bytes ?? (await videoSize(host, storage, run.path, signal));
      } catch {
        // Ohne info.json bleibt der Eintrag trotzdem in der Liste.
      }
    }),
  );

  return runs;
};

/** Größe von zeitraffer.mp4 in einem Lauf, für Läufe ohne video_bytes in info.json. */
const videoSize = async (
  host: ModuleHost,
  storage: string,
  folder: string,
  signal?: AbortSignal,
): Promise<number> => {
  try {
    const page = (await host.client.listStorageFiles(
      { path: { storage }, query: { path: folder } },
      signal,
    )) as unknown as { items?: Entry[] };
    return page.items?.find((entry) => entry.name === 'zeitraffer.mp4')?.size ?? 0;
  } catch {
    return 0;
  }
};

/** Datum und Uhrzeit in Ortszeit des Browsers, etwa „20.11.2026 23:44“. */
export const formatStarted = (iso: string): string => {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '';
  return date.toLocaleString('de-DE', { dateStyle: 'medium', timeStyle: 'short' });
};

export const deleteRun = (host: ModuleHost, storage: string, path: string): Promise<void> =>
  host.client.deleteStorageEntry({ path: { storage }, query: { path } });

/** Macht aus „2026-11-20_18-05_halter-v2“ Datum und Name. */
export const describeRun = (name: string): { when: string; title: string } => {
  const match = /^(\d{4})-(\d{2})-(\d{2})_(\d{2})-(\d{2})_(.*)$/.exec(name);
  if (!match) return { when: '', title: name };
  const [, y, mo, d, h, mi, rest] = match;

  return { when: `${d}.${mo}.${y} ${h}:${mi}`, title: (rest ?? '').replaceAll('-', ' ') };
};

export const outcomeLabel = (outcome?: string): string =>
  ({ finished: 'fertig', failed: 'fehlgeschlagen', cancelled: 'abgebrochen', unknown: 'unbekannt' })[
    outcome ?? ''
  ] ?? '';
