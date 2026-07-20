export type DesktopRuntimeInfo = {
  desktop: boolean;
  platform: string;
  arch: string;
  version: string;
  dataDir: string;
  configPath: string;
  database: string;
};

type DesktopBridge = {
  GetRuntimeInfo: () => Promise<DesktopRuntimeInfo>;
  PickDirectory: (title: string, initialPath: string, allowedRoot: string) => Promise<string>;
  PickFile: (title: string, initialPath: string, displayName: string, pattern: string) => Promise<string>;
  RevealPath: (path: string) => Promise<void>;
  OpenPath: (path: string) => Promise<void>;
  Notify: (title: string, body: string) => Promise<void>;
  PreviewRename: (requestId: string, input: DesktopRenamePreviewRequest) => Promise<void>;
  CancelRenamePreview: (requestId: string) => Promise<boolean>;
};

type DesktopRuntimeBridge = {
  EventsOn: (eventName: string, callback: (data: unknown) => void) => () => void;
};

export type DesktopRenamePreviewRequest = {
  path: string;
  template: string;
  matchPattern: string;
  bypassTmdbCache: boolean;
  language: string;
  releaseGroup: string;
};

export type DesktopRenamePreviewEvent = {
  requestId: string;
  type: 'start' | 'item' | 'done' | 'error';
  item?: unknown;
  count: number;
  total: number;
  error?: string;
};

declare global {
  interface Window {
    go?: {
      main?: {
        DesktopApp?: DesktopBridge;
      };
    };
    runtime?: DesktopRuntimeBridge;
  }
}

function bridge(): DesktopBridge | null {
  return window.go?.main?.DesktopApp ?? null;
}

export async function getRuntimeInfo(): Promise<DesktopRuntimeInfo> {
  const desktop = bridge();
  if (!desktop) {
    return {
      desktop: false,
      platform: 'web',
      arch: '',
      version: 'web',
      dataDir: '',
      configPath: '',
      database: ''
    };
  }
  return desktop.GetRuntimeInfo();
}

export async function pickDesktopDirectory(options: { title: string; initialPath: string; allowedRoot?: string }): Promise<{ handled: boolean; path: string }> {
  const desktop = bridge();
  if (!desktop) return { handled: false, path: '' };
  const path = await desktop.PickDirectory(options.title, options.initialPath, options.allowedRoot ?? '');
  return { handled: true, path };
}

export async function pickDesktopFile(options: { title: string; initialPath: string; displayName?: string; pattern?: string }): Promise<{ handled: boolean; path: string }> {
  const desktop = bridge();
  if (!desktop) return { handled: false, path: '' };
  const path = await desktop.PickFile(options.title, options.initialPath, options.displayName ?? '所有文件', options.pattern ?? '');
  return { handled: true, path };
}

export async function revealDesktopPath(path: string): Promise<boolean> {
  const desktop = bridge();
  if (!desktop) return false;
  await desktop.RevealPath(path);
  return true;
}

export async function openDesktopPath(path: string): Promise<boolean> {
  const desktop = bridge();
  if (!desktop) return false;
  await desktop.OpenPath(path);
  return true;
}

export async function notifyDesktop(title: string, body: string): Promise<void> {
  await bridge()?.Notify(title, body);
}

export async function previewDesktopRename(input: DesktopRenamePreviewRequest, onEvent: (event: DesktopRenamePreviewEvent) => void, signal?: AbortSignal): Promise<boolean> {
  const desktop = bridge();
  const events = window.runtime;
  if (!desktop || !events?.EventsOn) return false;

  const requestId = globalThis.crypto?.randomUUID?.() ?? `rename-${Date.now()}-${Math.random().toString(36).slice(2)}`;
  const unsubscribe = events.EventsOn('nyamedia:rename-preview', (data) => {
    const payload = (Array.isArray(data) ? data[0] : data) as DesktopRenamePreviewEvent | undefined;
    if (!signal?.aborted && payload?.requestId === requestId) onEvent(payload);
  });
  const preview = desktop.PreviewRename(requestId, input);
  const abort = () => { void desktop.CancelRenamePreview(requestId); };
  signal?.addEventListener('abort', abort, { once: true });
  if (signal?.aborted) abort();
  try {
    await preview;
    if (signal?.aborted) throw new DOMException('预览已取消', 'AbortError');
    return true;
  } catch (error) {
    if (signal?.aborted) throw new DOMException('预览已取消', 'AbortError');
    throw error;
  } finally {
    signal?.removeEventListener('abort', abort);
    unsubscribe();
  }
}
