import type { ModuleHost } from '@prusa/connect-ui/slots';

let held: ModuleHost | undefined;

/** Hält den Host fest, den die App beim Laden des Moduls übergibt. */
export const holdHost = (host: ModuleHost): void => {
  if (host.signal.aborted) return;
  held = host;
  host.signal.addEventListener(
    'abort',
    () => {
      if (held === host) held = undefined;
    },
    { once: true },
  );
};

export const heldHost = (): ModuleHost => {
  if (!held) {
    throw new Error('Die Zeitraffer-Erweiterung wurde gezeichnet, bevor sie geladen war.');
  }

  return held;
};
