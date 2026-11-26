import { formatDateTime } from '@prusa/connect-ui/i18n';
import type { ModuleHost } from '@prusa/connect-ui/slots';

/** What the module's GET /status answers. */
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
  /** First line of `ffmpeg -version`. */
  ffmpeg?: string;
  /** Why ffmpeg does not run. Without ffmpeg nothing is recorded. */
  ffmpeg_problem?: string;
}

/** A finished timelapse: one folder in the storage. */
export interface Run {
  path: string;
  name: string;
  modified: string;
  size: number;
  outcome?: string;
  layers?: number;
  /** Print start from info.json, ISO time. */
  started?: string;
  /** Layers with diagnosis frames. */
  diagnosed?: number;
}

// The app loads this bundle from /api/v1/modules/<namespace>/ui/ui/index.js and
// the module's routes are under /api/v1/modules/<namespace>/. Derived from the
// bundle's own address, because ModuleHost.namespace came after v0.1.0-beta.1.
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

/** A printer's timelapses, newest first. */
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
    // A folder that does not exist yet means no timelapse yet. It answers 404
    // or core.storage/INVALID_PATH. No instanceof: the bundle and the app each
    // hold their own copy of the error class.
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
        // A folder's size in the listing is not the video's.
        run.size = info.video_bytes ?? (await videoSize(host, storage, run.path, signal));
      } catch {
        // Without info.json the entry still stays in the list.
      }
    }),
  );

  return runs;
};

/** Size of zeitraffer.mp4 in a run, for runs without video_bytes in info.json. */
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

/** A print start from info.json, in the reader's language and clock. */
export const formatStarted = (iso: string): string => {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '';
  return formatDateTime(date);
};

export const deleteRun = (host: ModuleHost, storage: string, path: string): Promise<void> =>
  host.client.deleteStorageEntry({ path: { storage }, query: { path } });

/** Splits „2026-11-20_18-05_holder-v2“ into the start, in the reader's language, and the name. */
export const describeRun = (name: string): { when: string; title: string } => {
  const match = /^(\d{4})-(\d{2})-(\d{2})_(\d{2})-(\d{2})_(.*)$/.exec(name);
  if (!match) return { when: '', title: name };
  const [, y, mo, d, h, mi, rest] = match;
  const started = new Date(Number(y), Number(mo) - 1, Number(d), Number(h), Number(mi));

  return { when: formatDateTime(started), title: (rest ?? '').replaceAll('-', ' ') };
};

/** The catalogue key of a run's outcome, or '' for none. */
export const outcomeKey = (outcome?: string): string =>
  ['finished', 'failed', 'cancelled', 'unknown'].includes(outcome ?? '') ? `runs.outcome.${outcome}` : '';
