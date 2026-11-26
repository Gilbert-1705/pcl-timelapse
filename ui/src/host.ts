import type { ModuleHost } from '@prusa/connect-ui/slots';

let held: ModuleHost | undefined;

/** Keeps the host the app hands over when it loads the module. */
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
    throw new Error('The timelapse extension was drawn before it was loaded.');
  }

  return held;
};
