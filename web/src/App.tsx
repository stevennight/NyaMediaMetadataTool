import { useEffect, useRef, useState } from 'react';
import type { MouseEvent as ReactMouseEvent } from 'react';

type Health = {
  status: string;
  time: string;
};

type AppConfig = {
  server: { addr: string; timezone: string };
  database: { path: string };
  tools: {
    ffmpeg: string;
    ffprobe: string;
    mkvextract: string;
    mediainfo: string;
  };
  processing: {
    extensions: string[];
    concurrency: number;
    bifWidth: number;
    bifInterval: number;
    overwriteExisting: boolean;
    enableSubtitles: boolean;
    enableMediaInfo: boolean;
    enableNfo: boolean;
    enableBif: boolean;
    enableImageTakeover: boolean;
  };
  scraping: {
    enableTmdb: boolean;
    enablePeople: boolean;
    preferOriginalLanguagePoster: boolean;
    imageSources: string[];
    fanartApiKey: string;
    fanartBaseUrl: string;
    tmdbApiKey: string;
    tmdbToken: string;
    tmdbBaseUrl: string;
    language: string;
    fallbackLanguages: string[];
    region: string;
    proxy: string;
  };
  watchDirs: WatchDir[];
};

type WatchDir = { id: number; path: string; recursive: boolean; enabled: boolean };

type ToolStatus = {
  name: string;
  path: string;
  available: boolean;
  version: string;
  error: string;
  checkedAt: string;
};

type Task = {
  id: number;
  mediaFileId?: number;
  mediaPath: string;
  type: string;
  status: string;
  overwriteExisting: boolean;
  attempts: number;
  errorSummary: string;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  updatedAt?: string;
};

type TaskListResponse = {
  items: Task[];
  total: number;
  page: number;
  pageSize: number;
};

type Artifact = {
  id: number;
  type: string;
  path: string;
  source: string;
  createdAt: string;
};

type TaskLog = {
  id: number;
  level: string;
  message: string;
  detail: string;
  createdAt: string;
};

type TaskDetail = {
  task: Task;
  logs: TaskLog[];
  artifacts: Artifact[];
};

type RenamePreviewItem = {
  path: string;
  currentName: string;
  newName: string;
  newPath: string;
  renderedTarget: string;
  show: string;
  title: string;
  season: number;
  episode: number;
  year: string;
  tmdbShowId: number;
  tmdbEpisodeId: number;
  source: string;
  status: string;
  message: string;
  conflict: boolean;
  sanitizedTitle: string;
  manualName: boolean;
};

type TMDBSearchResult = {
  id: number;
  name: string;
  originalName: string;
  firstAirDate: string;
  overview: string;
};

type RenamePreviewStreamMessage = {
  type: 'item' | 'done' | 'error';
  item?: RenamePreviewItem;
  count?: number;
  error?: string;
};

type RenameHistoryMove = { from: string; to: string };
type RenameHistoryItem = { path: string; newPath: string; status: string; message: string; moves: RenameHistoryMove[] };
type RenameHistoryBatch = { id: string; createdAt: string; undone: boolean; undoneAt?: string; items: RenameHistoryItem[] };
type RenameUndoCheckItem = { from: string; to: string; ok: boolean; reason: string };
type RenameUndoCheckResult = { canUndo: boolean; batch: RenameHistoryBatch; items: RenameUndoCheckItem[] };

type DirectoryEntry = { name: string; path: string };
type DirectoryList = { path: string; parent: string; entries: DirectoryEntry[] };

type RescanScope = 'all' | 'dir' | 'path';
type RescanStrategy = 'missing' | 'force';
type BatchEpisodeMode = 'keep' | 'offset' | 'sequence';

type LanguageOption = { code: string; name: string };
type RegionOption = { code: string; name: string };
type PageKey = 'dashboard' | 'settings' | 'watchDirs' | 'tasks' | 'rename';

const pagePaths: Record<PageKey, string> = {
  dashboard: '/',
  settings: '/settings',
  watchDirs: '/watch-dirs',
  tasks: '/tasks',
  rename: '/rename'
};

function pageFromPath(pathname: string): PageKey {
  switch (pathname) {
    case '/settings':
      return 'settings';
    case '/watch-dirs':
      return 'watchDirs';
    case '/tasks':
      return 'tasks';
    case '/rename':
      return 'rename';
    default:
      return 'dashboard';
  }
}

const languageOptions: LanguageOption[] = [
  { code: 'zh-CN', name: '简体中文' },
  { code: 'zh-TW', name: '繁体中文' },
  { code: 'ja-JP', name: '日语' },
  { code: 'en-US', name: '英语（美国）' },
  { code: 'en-GB', name: '英语（英国）' },
  { code: 'ko-KR', name: '韩语' },
  { code: 'fr-FR', name: '法语' },
  { code: 'de-DE', name: '德语' },
  { code: 'es-ES', name: '西班牙语' },
  { code: 'it-IT', name: '意大利语' },
  { code: 'pt-BR', name: '葡萄牙语（巴西）' },
  { code: 'ru-RU', name: '俄语' },
  { code: 'th-TH', name: '泰语' },
  { code: 'vi-VN', name: '越南语' },
  { code: 'id-ID', name: '印尼语' }
];

const regionOptions: RegionOption[] = [
  { code: 'CN', name: '中国大陆' },
  { code: 'TW', name: '中国台湾' },
  { code: 'HK', name: '中国香港' },
  { code: 'JP', name: '日本' },
  { code: 'US', name: '美国' },
  { code: 'GB', name: '英国' },
  { code: 'KR', name: '韩国' },
  { code: 'FR', name: '法国' },
  { code: 'DE', name: '德国' },
  { code: 'ES', name: '西班牙' },
  { code: 'IT', name: '意大利' },
  { code: 'BR', name: '巴西' },
  { code: 'RU', name: '俄罗斯' },
  { code: 'TH', name: '泰国' }
];

const timeZoneOptions = ['Asia/Shanghai', 'Asia/Tokyo', 'UTC', 'America/Los_Angeles', 'America/New_York', 'Europe/London'];
const defaultRenameTemplate = '{show} - S{season:00}E{episode:00} - {title}';
const renamePlaceholders = ['{show}', '{season:00}', '{episode:00}', '{title}', '{year}'];
const renamePreferencesKey = 'nya.rename.preferences';
const renameTemplateHistoryLimit = 20;

type RenamePreferences = {
  path?: string;
  template?: string;
  language?: string;
  useTmdb?: boolean;
  templateHistory?: string[];
};

function readRenamePreferences(): RenamePreferences {
  try {
    const raw = window.localStorage.getItem(renamePreferencesKey);
    if (!raw) return {};
    const value = JSON.parse(raw) as RenamePreferences;
    return value && typeof value === 'object' ? value : {};
  } catch {
    return {};
  }
}

function writeRenamePreferences(value: RenamePreferences) {
  try {
    window.localStorage.setItem(renamePreferencesKey, JSON.stringify(value));
  } catch {
    // Ignore storage failures, for example private browsing quota limits.
  }
}

function splitRenameTargetPath(value: string) {
  const trimmed = value.trim();
  const separatorIndex = Math.max(trimmed.lastIndexOf('/'), trimmed.lastIndexOf('\\'));
  if (separatorIndex >= 0 && separatorIndex < trimmed.length - 1) {
    return {
      dir: trimmed.slice(0, separatorIndex + 1),
      name: trimmed.slice(separatorIndex + 1)
    };
  }
  return { dir: '', name: trimmed };
}

function RenameTargetPathDisplay(props: { value: string }) {
  const parts = splitRenameTargetPath(props.value);
  if (!parts.name) return <>-</>;
  if (!parts.dir) return <span className="target-path-name">{parts.name}</span>;

  return (
    <span className="target-path-dir" title={parts.dir}>
      <span className="target-path-dir-icon" aria-hidden="true" />
      <span className="target-path-name">{parts.name}</span>
    </span>
  );
}

function getRenameTargetDisplayValue(item: RenamePreviewItem) {
  const renderedTarget = item.renderedTarget || item.newName || item.newPath || '';
  if (splitRenameTargetPath(renderedTarget).dir) return item.newPath || renderedTarget;
  return item.newName || renderedTarget;
}

function getRenameTargetEditorValue(item: RenamePreviewItem) {
  return item.renderedTarget || item.newPath || item.newName || '';
}

export function App() {
  const [health, setHealth] = useState<Health | null>(null);
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [tools, setTools] = useState<ToolStatus[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [taskTotal, setTaskTotal] = useState(0);
  const [taskPage, setTaskPage] = useState(1);
  const [taskPageSize] = useState(20);
  const [taskPathFilter, setTaskPathFilter] = useState('');
  const [taskFromFilter, setTaskFromFilter] = useState('');
  const [taskToFilter, setTaskToFilter] = useState('');
  const [watchDirs, setWatchDirs] = useState<WatchDir[]>([]);
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [renamePath, setRenamePath] = useState(() => readRenamePreferences().path ?? '');
  const [renameTemplate, setRenameTemplate] = useState(() => readRenamePreferences().template ?? defaultRenameTemplate);
  const [renameUseTmdb, setRenameUseTmdb] = useState(() => readRenamePreferences().useTmdb ?? true);
  const [renameLanguage, setRenameLanguage] = useState(() => readRenamePreferences().language ?? 'zh-CN');
  const [renameLanguageInitialized, setRenameLanguageInitialized] = useState(() => Boolean(readRenamePreferences().language));
  const [renameTemplateHistory, setRenameTemplateHistory] = useState(() => asArray<string>(readRenamePreferences().templateHistory).filter(Boolean));
  const [renamePreview, setRenamePreview] = useState<RenamePreviewItem[]>([]);
  const [renamePreviewCount, setRenamePreviewCount] = useState(0);
  const [renameHistory, setRenameHistory] = useState<RenameHistoryBatch[]>([]);
  const [expandedHistoryIds, setExpandedHistoryIds] = useState<string[]>([]);
  const [undoCheckResult, setUndoCheckResult] = useState<RenameUndoCheckResult | null>(null);
  const [loadingRenameHistory, setLoadingRenameHistory] = useState(false);
  const [undoingHistoryId, setUndoingHistoryId] = useState('');
  const [selectedRenamePaths, setSelectedRenamePaths] = useState<string[]>([]);
  const [tmdbQuery, setTmdbQuery] = useState('');
  const [tmdbResults, setTmdbResults] = useState<TMDBSearchResult[]>([]);
  const [searchingTmdb, setSearchingTmdb] = useState(false);
  const [applyingRename, setApplyingRename] = useState(false);
  const [batchEpisodeOpen, setBatchEpisodeOpen] = useState(false);
  const [batchSeason, setBatchSeason] = useState(1);
  const [batchEpisodeMode, setBatchEpisodeMode] = useState<BatchEpisodeMode>('sequence');
  const [batchEpisodeOffset, setBatchEpisodeOffset] = useState(0);
  const [batchEpisodeStart, setBatchEpisodeStart] = useState(1);
  const [applyingBatchEpisode, setApplyingBatchEpisode] = useState(false);
  const [targetPathEditor, setTargetPathEditor] = useState<{ path: string; value: string } | null>(null);
  const [renameTemplateEditorOpen, setRenameTemplateEditorOpen] = useState(false);
  const [previewingRename, setPreviewingRename] = useState(false);
  const [directoryPicker, setDirectoryPicker] = useState<{ title: string; value: string; onSelect: (path: string) => void } | null>(null);
  const [newWatchDir, setNewWatchDir] = useState('');
  const [newWatchDirEnabled, setNewWatchDirEnabled] = useState(true);
  const [rescanOpen, setRescanOpen] = useState(false);
  const [rescanScope, setRescanScope] = useState<RescanScope>('all');
  const [rescanTarget, setRescanTarget] = useState('');
  const [rescanStrategy, setRescanStrategy] = useState<RescanStrategy>('missing');
  const [selectedTask, setSelectedTask] = useState<TaskDetail | null>(null);
  const [checkingTools, setCheckingTools] = useState(false);
  const [savingConfig, setSavingConfig] = useState(false);
  const [cancelingTasks, setCancelingTasks] = useState(false);
  const [notice, setNotice] = useState('');
  const [rescanning, setRescanning] = useState(false);
  const [error, setError] = useState<string>('');
  const [activePage, setActivePage] = useState<PageKey>(() => pageFromPath(window.location.pathname));
  const lastRenameSelectionIndexRef = useRef<number | null>(null);
  const displayTimezone = config?.server.timezone || 'Asia/Shanghai';

  useEffect(() => {
    async function load() {
      try {
        const [healthResponse, configResponse, toolsResponse, tasksResponse, dirsResponse, artifactsResponse] = await Promise.all([
          fetch('/api/health'),
          fetch('/api/config'),
          fetch('/api/tools/status'),
          fetch(`/api/tasks?page=1&pageSize=${taskPageSize}`),
          fetch('/api/watch-dirs'),
          fetch('/api/artifacts?limit=10')
        ]);
        setHealth(await healthResponse.json());
        setConfig(await configResponse.json());
        setTools(asArray<ToolStatus>(await toolsResponse.json()));
        applyTaskList(await tasksResponse.json());
        setWatchDirs(asArray<WatchDir>(await dirsResponse.json()));
        setArtifacts(asArray<Artifact>(await artifactsResponse.json()));
        await loadRenameHistory();
      } catch (err) {
        setError(err instanceof Error ? err.message : '加载失败');
      }
    }

    void load();
  }, [taskPageSize]);

  async function loadRenameHistory() {
    setLoadingRenameHistory(true);
    try {
      const response = await fetch('/api/rename/history');
      if (!response.ok) {
        return;
      }
      const result = await response.json();
      setRenameHistory(asArray<RenameHistoryBatch>(result.items));
    } finally {
      setLoadingRenameHistory(false);
    }
  }

  useEffect(() => {
    function handlePopState() {
      setActivePage(pageFromPath(window.location.pathname));
    }

    window.addEventListener('popstate', handlePopState);
    return () => window.removeEventListener('popstate', handlePopState);
  }, []);

  useEffect(() => {
    if (!renameLanguageInitialized && config?.scraping.language) {
      setRenameLanguage(config.scraping.language);
      setRenameLanguageInitialized(true);
    }
  }, [config?.scraping.language, renameLanguageInitialized]);

  function navigate(page: PageKey) {
    setActivePage(page);
    const path = pagePaths[page];
    if (window.location.pathname !== path) {
      window.history.pushState(null, '', path);
    }
  }

  function applyTaskList(value: TaskListResponse | Task[] | null | undefined) {
    if (Array.isArray(value)) {
      setTasks(value);
      setTaskTotal(value.length);
      setTaskPage(1);
      return;
    }
    setTasks(asArray<Task>(value?.items));
    setTaskTotal(value?.total ?? 0);
    setTaskPage(value?.page ?? 1);
  }

  async function loadTasks(page = taskPage) {
    const params = new URLSearchParams({ page: String(page), pageSize: String(taskPageSize) });
    if (taskPathFilter.trim()) params.set('path', taskPathFilter.trim());
    if (taskFromFilter) params.set('from', zonedInputToUTC(taskFromFilter, displayTimezone, false));
    if (taskToFilter) params.set('to', zonedInputToUTC(taskToFilter, displayTimezone, true));
    const response = await fetch(`/api/tasks?${params.toString()}`);
    if (!response.ok) {
      setError(await response.text());
      return;
    }
    applyTaskList(await response.json());
  }

  function resetTaskFilters() {
    setTaskPathFilter('');
    setTaskFromFilter('');
    setTaskToFilter('');
    void loadTasksWithoutFilters();
  }

  async function loadTasksWithoutFilters() {
    const response = await fetch(`/api/tasks?page=1&pageSize=${taskPageSize}`);
    if (!response.ok) {
      setError(await response.text());
      return;
    }
    applyTaskList(await response.json());
  }

  async function checkTools() {
    setCheckingTools(true);
    setError('');
    try {
      const response = await fetch('/api/tools/check', { method: 'POST' });
      setTools(asArray<ToolStatus>(await response.json()));
    } catch (err) {
      setError(err instanceof Error ? err.message : '工具检测失败');
    } finally {
      setCheckingTools(false);
    }
  }

  async function previewRename() {
    if (!renamePath.trim()) {
      setError('请输入目录或文件路径');
      return;
    }
    rememberRenamePreferences();
    setPreviewingRename(true);
    setError('');
    setNotice('');
    setRenamePreview([]);
    setRenamePreviewCount(0);
    setSelectedRenamePaths([]);
    try {
      const response = await fetch('/api/rename/preview/stream', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: renamePath.trim(), template: renameTemplate, useTmdb: renameUseTmdb, language: renameLanguage })
      });
      if (!response.ok) {
        setError(await response.text());
        return;
      }
      if (!response.body) {
        setError('当前浏览器不支持流式预览');
        return;
      }

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let pending = '';
      while (true) {
        const { value, done } = await reader.read();
        pending += decoder.decode(value, { stream: !done });
        const lines = pending.split('\n');
        pending = lines.pop() ?? '';
        for (const line of lines) {
          handleRenamePreviewMessage(line);
        }
        if (done) break;
      }
      if (pending.trim()) {
        handleRenamePreviewMessage(pending);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '生成预览失败');
    } finally {
      setPreviewingRename(false);
    }
  }

  function rememberRenamePreferences() {
    const template = renameTemplate.trim() || defaultRenameTemplate;
    const nextHistory = [template, ...renameTemplateHistory.filter((item) => item !== template)].slice(0, renameTemplateHistoryLimit);
    setRenameTemplateHistory(nextHistory);
    writeRenamePreferences({
      path: renamePath.trim(),
      template,
      language: renameLanguage,
      useTmdb: renameUseTmdb,
      templateHistory: nextHistory
    });
  }

  function handleRenamePreviewMessage(line: string) {
    if (!line.trim()) return;
    const message = JSON.parse(line) as RenamePreviewStreamMessage;
    if (message.type === 'item' && message.item) {
      setRenamePreview((items) => [...items, message.item as RenamePreviewItem]);
      setRenamePreviewCount(message.count ?? 0);
    } else if (message.type === 'error') {
      setError(message.error || '生成预览失败');
    } else if (message.type === 'done') {
      setRenamePreviewCount(message.count ?? 0);
      setNotice(`预览生成完成，共 ${message.count ?? 0} 个文件。`);
    }
  }

  function updateRenameItem(path: string, patch: Partial<RenamePreviewItem>) {
    setRenamePreview((items) => items.map((item) => item.path === path ? { ...item, ...patch } : item));
  }

  function replaceRenameItem(next: RenamePreviewItem) {
    setRenamePreview((items) => items.map((item) => item.path === next.path ? next : item));
  }

  function toggleRenameSelection(path: string, checked: boolean, shiftKey = false) {
    const index = renamePreview.findIndex((item) => item.path === path);
    setSelectedRenamePaths((paths) => {
      if (shiftKey && lastRenameSelectionIndexRef.current !== null && index >= 0) {
        const start = lastRenameSelectionIndexRef.current;
        if (start >= 0) {
          const [from, to] = start < index ? [start, index] : [index, start];
          const range = renamePreview.slice(from, to + 1).map((item) => item.path);
          return checked ? [...new Set([...paths, ...range])] : paths.filter((item) => !range.includes(item));
        }
      }
      return checked ? [...new Set([...paths, path])] : paths.filter((item) => item !== path);
    });
    if (index >= 0) lastRenameSelectionIndexRef.current = index;
  }

  function handleRenameRowClick(event: ReactMouseEvent<HTMLTableRowElement>, item: RenamePreviewItem, index: number) {
    const target = event.target as HTMLElement;
    if (target.closest('input, button, select, textarea, a')) return;
    const selected = selectedRenamePaths.includes(item.path);
    if (event.shiftKey && lastRenameSelectionIndexRef.current !== null) {
      const [from, to] = lastRenameSelectionIndexRef.current < index ? [lastRenameSelectionIndexRef.current, index] : [index, lastRenameSelectionIndexRef.current];
      const range = renamePreview.slice(from, to + 1).map((entry) => entry.path);
      setSelectedRenamePaths((paths) => selected ? paths.filter((path) => !range.includes(path)) : [...new Set([...paths, ...range])]);
      return;
    }
    setSelectedRenamePaths((paths) => selected ? paths.filter((path) => path !== item.path) : [...new Set([...paths, item.path])]);
    lastRenameSelectionIndexRef.current = index;
  }

  async function previewAdjustedRenameItem(item: RenamePreviewItem, options: { tmdbShowId?: number; show?: string; forceTmdb?: boolean; keepManualName?: boolean } = {}) {
    setError('');
    const response = await fetch('/api/rename/preview/item', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        path: item.path,
        template: renameTemplate,
        useTmdb: options.forceTmdb ?? false,
        language: renameLanguage,
        show: options.show ?? item.show,
        title: item.title,
        season: item.season,
        episode: item.episode,
        tmdbShowId: options.tmdbShowId ?? item.tmdbShowId,
        newName: (options.keepManualName ?? item.manualName) ? item.newName : ''
      })
    });
    if (!response.ok) {
      setError(await response.text());
      return null;
    }
    return await response.json() as RenamePreviewItem;
  }

  async function recalculateRenameItem(item: RenamePreviewItem, options: { tmdbShowId?: number; show?: string; forceTmdb?: boolean; keepManualName?: boolean } = {}) {
    const next = await previewAdjustedRenameItem(item, options);
    if (next) replaceRenameItem(next);
  }

  async function searchTmdbShows() {
    if (!tmdbQuery.trim()) return;
    setSearchingTmdb(true);
    setError('');
    try {
      const params = new URLSearchParams({ query: tmdbQuery.trim(), language: renameLanguage });
      const response = await fetch(`/api/tmdb/search-tv?${params.toString()}`);
      if (!response.ok) {
        setError(await response.text());
        return;
      }
      const result = await response.json();
      setTmdbResults(asArray<TMDBSearchResult>(result.items));
    } catch (err) {
      setError(err instanceof Error ? err.message : '搜索 TMDB 失败');
    } finally {
      setSearchingTmdb(false);
    }
  }

  async function applyTmdbShowToSelected(show: TMDBSearchResult) {
    const targets = renamePreview.filter((item) => selectedRenamePaths.includes(item.path));
    if (!targets.length) {
      setError('请先勾选要套用的文件');
      return;
    }
    setError('');
    for (const item of targets) {
      await recalculateRenameItem(item, { tmdbShowId: show.id, show: show.name || show.originalName, forceTmdb: true });
    }
  }

  function selectAllRenameItems() {
    setSelectedRenamePaths(renamePreview.map((item) => item.path));
  }

  function invertRenameSelection() {
    setSelectedRenamePaths(renamePreview.filter((item) => !selectedRenamePaths.includes(item.path)).map((item) => item.path));
  }

  function applyTargetPathEdit() {
    if (!targetPathEditor) return;
    updateRenameItem(targetPathEditor.path, { newName: targetPathEditor.value, newPath: targetPathEditor.value, renderedTarget: targetPathEditor.value, manualName: true });
    setTargetPathEditor(null);
  }

  function openBatchEpisodeDialog() {
    const first = renamePreview.find((item) => selectedRenamePaths.includes(item.path));
    setBatchSeason(first?.season ?? 1);
    setBatchEpisodeMode('sequence');
    setBatchEpisodeOffset(0);
    setBatchEpisodeStart(1);
    setBatchEpisodeOpen(true);
  }

  async function applyBatchEpisodeFix() {
    const targets = renamePreview.filter((item) => selectedRenamePaths.includes(item.path));
    if (!targets.length) {
      setError('请先勾选要批量修正的文件');
      return;
    }
    setApplyingBatchEpisode(true);
    setError('');
    try {
      for (let index = 0; index < targets.length; index++) {
        const item = targets[index];
        const episode = batchEpisodeMode === 'sequence'
          ? batchEpisodeStart + index
          : batchEpisodeMode === 'offset'
            ? item.episode + batchEpisodeOffset
            : item.episode;
        const adjusted = { ...item, season: batchSeason, episode: Math.max(0, episode), manualName: false };
        const next = await previewAdjustedRenameItem(adjusted, { forceTmdb: true, keepManualName: false });
        if (next) replaceRenameItem(next);
      }
      setBatchEpisodeOpen(false);
      setNotice(`已批量修正 ${targets.length} 个文件的季集并重新预览。`);
    } finally {
      setApplyingBatchEpisode(false);
    }
  }

  async function applySelectedRenames() {
    const targets = renamePreview.filter((item) => selectedRenamePaths.includes(item.path));
    if (!targets.length) {
      setError('请先勾选要重命名的文件');
      return;
    }
    if (!window.confirm(`确认重命名选中的 ${targets.length} 个文件？不会覆盖已存在的目标文件。`)) {
      return;
    }
    setApplyingRename(true);
    setError('');
    setNotice('');
    try {
      const response = await fetch('/api/rename/apply', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ items: targets.map((item) => ({ path: item.path, newName: item.newName, newPath: item.newPath })) })
      });
      if (!response.ok) {
        setError(await response.text());
        return;
      }
      const result = await response.json();
      const updates = asArray<RenamePreviewItem>(result.items);
      const updateByOriginalPath = new Map(targets.map((item, index) => [item.path, updates[index]]));
      setRenamePreview((items) => items.map((item) => updateByOriginalPath.get(item.path) ?? item));
      setSelectedRenamePaths([]);
      setNotice(`重命名完成：${updates.filter((item) => item.status === 'renamed').length} 成功，${updates.filter((item) => item.status === 'error').length} 失败。`);
      await loadRenameHistory();
    } catch (err) {
      setError(err instanceof Error ? err.message : '执行重命名失败');
    } finally {
      setApplyingRename(false);
    }
  }

  async function undoRenameBatch(id: string) {
    const checkResponse = await fetch(`/api/rename/history/${id}/undo-check`);
    if (!checkResponse.ok) {
      setError(await checkResponse.text());
      return;
    }
    const check = await checkResponse.json() as RenameUndoCheckResult;
    setUndoCheckResult(check);
    if (!check.canUndo) {
      setError('该批次存在不可撤销项，已停止撤销。请展开历史查看详情。');
      setExpandedHistoryIds((ids) => [...new Set([...ids, id])]);
      return;
    }
    if (!window.confirm(`确认撤销该批次的 ${check.items.length} 个文件移动？`)) {
      return;
    }
    setUndoingHistoryId(id);
    setError('');
    try {
      const response = await fetch(`/api/rename/history/${id}/undo`, { method: 'POST' });
      if (!response.ok) {
        setError(await response.text());
        return;
      }
      setNotice('已撤销最近一次重命名。');
      setUndoCheckResult(null);
      await loadRenameHistory();
    } catch (err) {
      setError(err instanceof Error ? err.message : '撤销失败');
    } finally {
      setUndoingHistoryId('');
    }
  }

  function toggleHistoryDetails(id: string) {
    setExpandedHistoryIds((ids) => ids.includes(id) ? ids.filter((item) => item !== id) : [...ids, id]);
  }

  async function addWatchDir() {
    if (!newWatchDir.trim()) return;
    setError('');
    const response = await fetch('/api/watch-dirs', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: newWatchDir.trim(), recursive: true, enabled: newWatchDirEnabled })
    });
    if (!response.ok) {
      setError(await response.text());
      return;
    }
    const created = await response.json();
    setWatchDirs((items) => [...items, created]);
    setNewWatchDir('');
  }

  async function updateWatchDir(dir: WatchDir, patch: Partial<WatchDir>) {
    setError('');
    const next = { ...dir, ...patch };
    const response = await fetch(`/api/watch-dirs/${dir.id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(next)
    });
    if (!response.ok) {
      setError(await response.text());
      return;
    }
    const updated = await response.json();
    setWatchDirs((items) => items.map((item) => item.id === dir.id ? updated : item));
    setNotice('目录配置已更新，自动监听变更需要重启服务后生效；补扫可立即手动执行。');
  }

  async function deleteWatchDir(id: number) {
    setError('');
    const response = await fetch(`/api/watch-dirs/${id}`, { method: 'DELETE' });
    if (!response.ok) {
      setError(await response.text());
      return;
    }
    setWatchDirs((items) => items.filter((item) => item.id !== id));
  }

  async function rescan() {
    setRescanning(true);
    setError('');
    try {
      const payload: Record<string, string | number> = { strategy: rescanStrategy };
      if (rescanScope === 'dir') {
        const selected = watchDirs.find((dir) => dir.path === rescanTarget);
        if (!selected) {
          setError('请选择媒体目录');
          return;
        }
        payload.watchDirId = selected.id;
      } else if (rescanScope === 'path') {
        if (!rescanTarget.trim()) {
          setError('请输入目录或文件路径');
          return;
        }
        payload.path = rescanTarget.trim();
      }
      const response = await fetch('/api/tasks/rescan', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      if (!response.ok) {
        setError(await response.text());
        return;
      }
      await loadTasks(1);
    } catch (err) {
      setError(err instanceof Error ? err.message : '补扫失败');
    } finally {
      setRescanning(false);
    }
  }

  function openRescanDialog(scope: RescanScope, target = '') {
    setRescanScope(scope);
    setRescanTarget(target);
    setRescanStrategy('missing');
    setRescanOpen(true);
  }

  async function loadTaskDetail(id: number) {
    setError('');
    const response = await fetch(`/api/tasks/${id}`);
    if (!response.ok) {
      setError(await response.text());
      return;
    }
    const detail = await response.json();
    setSelectedTask({
      ...detail,
      logs: asArray<TaskLog>(detail.logs),
      artifacts: asArray<Artifact>(detail.artifacts)
    });
  }

  async function cancelActiveTasks() {
    if (!window.confirm('确定取消所有待执行和执行中的任务吗？')) return;
    setCancelingTasks(true);
    setError('');
    setNotice('');
    try {
      const response = await fetch('/api/tasks/cancel-active', { method: 'POST' });
      if (!response.ok) {
        setError(await response.text());
        return;
      }
      const result = await response.json();
      setNotice(`已取消 ${result.count ?? 0} 个待执行/执行中任务。`);
      await loadTasks(taskPage);
    } catch (err) {
      setError(err instanceof Error ? err.message : '取消任务失败');
    } finally {
      setCancelingTasks(false);
    }
  }

  async function saveConfig() {
    if (!config) return;
    setSavingConfig(true);
    setError('');
    setNotice('');
    try {
      const response = await fetch('/api/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(config)
      });
      if (!response.ok) {
        setError(await response.text());
        return;
      }
      const result = await response.json();
      setConfig(result.config);
      setNotice(result.restartRequired ? '配置已保存，部分后台任务需要重启服务后生效。' : '配置已保存。');
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存配置失败');
    } finally {
      setSavingConfig(false);
    }
  }

  function updateConfig(mutator: (draft: AppConfig) => void) {
    setConfig((current) => {
      if (!current) return current;
      const next = structuredClone(current);
      mutator(next);
      return next;
    });
  }

  return (
    <main className="app-shell">
      <aside className="sidebar">
        <div>
          <p className="eyebrow">NyaMediaMetadataTool</p>
          <h1>媒体元数据管理后台</h1>
          <p className="summary">本地媒体伴生文件、NFO、BIF、字幕和刮削任务管理。</p>
        </div>
        <nav className="module-nav" aria-label="后台模块">
          <TabButton active={activePage === 'dashboard'} label="Dashboard" onClick={() => navigate('dashboard')} />
          <TabButton active={activePage === 'settings'} label="设置" onClick={() => navigate('settings')} />
          <TabButton active={activePage === 'watchDirs'} label="媒体目录" onClick={() => navigate('watchDirs')} />
          <TabButton active={activePage === 'tasks'} label="任务" onClick={() => navigate('tasks')} />
          <TabButton active={activePage === 'rename'} label="整理命名" onClick={() => navigate('rename')} />
        </nav>
        <div className="service-mini">
          <span>服务状态</span>
          <strong>{health?.status ?? 'loading'}</strong>
        </div>
      </aside>

      <section className="content-panel">
        {error && <section className="error-card">{error}</section>}
        {notice && <section className="notice-card">{notice}</section>}

        {activePage === 'dashboard' && (
        <section className="page-grid dashboard-grid">
          <Card title="当前配置">
            <Row label="监听地址" value={config?.server.addr ?? '-'} />
            <Row label="显示时区" value={displayTimezone} />
            <Row label="数据库" value={config?.database.path ?? '-'} />
            <Row label="并发数" value={String(config?.processing.concurrency ?? '-')} />
            <Row label="扩展名" value={config?.processing.extensions?.join(', ') ?? '-'} />
            <Row label="TMDB 地址" value={config?.scraping.tmdbBaseUrl ?? '-'} />
            <Row label="字幕提取" value={config?.processing.enableSubtitles ? '开启' : '关闭'} />
            <Row label="MediaInfo" value={config?.processing.enableMediaInfo ? '开启' : '关闭'} />
            <Row label="NFO" value={config?.processing.enableNfo ? '开启' : '关闭'} />
            <Row label="BIF" value={config?.processing.enableBif ? '开启' : '关闭'} />
            <Row label="接管图片" value={config?.processing.enableImageTakeover ? '开启' : '关闭'} />
          </Card>

          <Card title="工具状态" action={<button onClick={checkTools} disabled={checkingTools}>{checkingTools ? '检测中' : '一键检测'}</button>}>
            {tools.length ? tools.map((tool) => (
              <div className="tool" key={tool.name}>
                <div>
                  <strong>{tool.name}</strong>
                  <small>{tool.version || tool.error || '未检测'}</small>
                </div>
                <span className={tool.available ? 'pill ok' : 'pill bad'}>{tool.available ? '可用' : '不可用'}</span>
              </div>
            )) : <p className="muted">尚未检测工具状态。</p>}
          </Card>
        </section>
      )}

        {activePage === 'settings' && (
        <section className="page-grid settings-grid">
          <Card title="设置" action={<button onClick={saveConfig} disabled={savingConfig || !config}>{savingConfig ? '保存中' : '保存配置'}</button>}>
            {config ? (
              <div className="config-form settings-form">
                <section className="settings-section">
                  <h3>基础</h3>
                  <label>显示时区<input list="timezone-options" value={config.server.timezone} onChange={(event) => updateConfig((draft) => { draft.server.timezone = event.target.value; })} placeholder="Asia/Shanghai" /></label>
                  <datalist id="timezone-options">
                    {timeZoneOptions.map((timezone) => <option key={timezone} value={timezone} />)}
                  </datalist>
                  <label>ffmpeg<input value={config.tools.ffmpeg} onChange={(event) => updateConfig((draft) => { draft.tools.ffmpeg = event.target.value; })} /></label>
                  <label>ffprobe<input value={config.tools.ffprobe} onChange={(event) => updateConfig((draft) => { draft.tools.ffprobe = event.target.value; })} /></label>
                  <label>mkvextract<input value={config.tools.mkvextract} onChange={(event) => updateConfig((draft) => { draft.tools.mkvextract = event.target.value; })} /></label>
                  <label>mediainfo<input value={config.tools.mediainfo} onChange={(event) => updateConfig((draft) => { draft.tools.mediainfo = event.target.value; })} /></label>
                  <label>并发数<input type="number" value={config.processing.concurrency} onChange={(event) => updateConfig((draft) => { draft.processing.concurrency = Number(event.target.value); })} /></label>
                  <label>BIF 宽度<input type="number" value={config.processing.bifWidth} onChange={(event) => updateConfig((draft) => { draft.processing.bifWidth = Number(event.target.value); })} /></label>
                  <label>BIF 间隔秒<input type="number" value={config.processing.bifInterval} onChange={(event) => updateConfig((draft) => { draft.processing.bifInterval = Number(event.target.value); })} /></label>
                </section>
                <section className="settings-section">
                  <h3>处理开关</h3>
                  <Toggle label="覆盖已有文件" checked={config.processing.overwriteExisting} onChange={(value) => updateConfig((draft) => { draft.processing.overwriteExisting = value; })} />
                  <Toggle label="字幕提取" checked={config.processing.enableSubtitles} onChange={(value) => updateConfig((draft) => { draft.processing.enableSubtitles = value; })} />
                  <Toggle label="MediaInfo" checked={config.processing.enableMediaInfo} onChange={(value) => updateConfig((draft) => { draft.processing.enableMediaInfo = value; })} />
                  <Toggle label="NFO" checked={config.processing.enableNfo} onChange={(value) => updateConfig((draft) => { draft.processing.enableNfo = value; })} />
                  <Toggle label="BIF" checked={config.processing.enableBif} onChange={(value) => updateConfig((draft) => { draft.processing.enableBif = value; })} />
                  <Toggle label="接管剧集/季度图片" checked={config.processing.enableImageTakeover} onChange={(value) => updateConfig((draft) => { draft.processing.enableImageTakeover = value; })} />
                  <Toggle label="TMDB 刮削" checked={config.scraping.enableTmdb} onChange={(value) => updateConfig((draft) => { draft.scraping.enableTmdb = value; })} />
                  <Toggle label="刮削演员/职员" checked={config.scraping.enablePeople} onChange={(value) => updateConfig((draft) => { draft.scraping.enablePeople = value; })} />
                  <Toggle label="优先原语言海报" checked={config.scraping.preferOriginalLanguagePoster} onChange={(value) => updateConfig((draft) => { draft.scraping.preferOriginalLanguagePoster = value; })} />
                </section>
                <section className="settings-section settings-section-wide">
                  <h3>刮削</h3>
                  <label>Fanart API Key<input type="password" value={config.scraping.fanartApiKey} onChange={(event) => updateConfig((draft) => { draft.scraping.fanartApiKey = event.target.value; })} placeholder="用于 clearart/clearlogo" /></label>
                  <label>Fanart 地址<input value={config.scraping.fanartBaseUrl} onChange={(event) => updateConfig((draft) => { draft.scraping.fanartBaseUrl = event.target.value; })} placeholder="https://webservice.fanart.tv/v3" /></label>
                  <label>TMDB Token<input type="password" value={config.scraping.tmdbToken} onChange={(event) => updateConfig((draft) => { draft.scraping.tmdbToken = event.target.value; })} placeholder="Bearer token" /></label>
                  <label>TMDB API Key<input value={config.scraping.tmdbApiKey} onChange={(event) => updateConfig((draft) => { draft.scraping.tmdbApiKey = event.target.value; })} placeholder="可选，优先使用 Token" /></label>
                  <label>TMDB 地址<input value={config.scraping.tmdbBaseUrl} onChange={(event) => updateConfig((draft) => { draft.scraping.tmdbBaseUrl = event.target.value; })} placeholder="https://api.themoviedb.org/3" /></label>
                  <label>TMDB 代理<input value={config.scraping.proxy} onChange={(event) => updateConfig((draft) => { draft.scraping.proxy = event.target.value; })} placeholder="http://127.0.0.1:7890" /></label>
                  <SelectField label="刮削语言" value={config.scraping.language} options={languageOptions} onChange={(value) => updateConfig((draft) => { draft.scraping.language = value; })} />
                  <LanguageMultiPicker label="备用语言顺序" values={config.scraping.fallbackLanguages ?? []} onChange={(values) => updateConfig((draft) => { draft.scraping.fallbackLanguages = values; })} />
                  <SelectField label="刮削地区" value={config.scraping.region} options={regionOptions} onChange={(value) => updateConfig((draft) => { draft.scraping.region = value; })} />
                </section>
              </div>
            ) : <p className="muted">配置加载中。</p>}
          </Card>
        </section>
      )}

        {activePage === 'watchDirs' && (
        <section className="page-grid">
          <Card title="媒体目录" action={<button onClick={() => openRescanDialog('all')} disabled={rescanning}>{rescanning ? '补扫中' : '补扫'}</button>}>
            <div className="form-row watch-dir-form-row">
              <div className="watch-dir-create">
                <div className="path-input"><input value={newWatchDir} onChange={(event) => setNewWatchDir(event.target.value)} placeholder="D:\\Media\\Anime" /><button type="button" onClick={() => setDirectoryPicker({ title: '选择媒体目录', value: newWatchDir, onSelect: setNewWatchDir })}>选择</button></div>
                <Toggle label="自动监听并启动时扫描" checked={newWatchDirEnabled} onChange={setNewWatchDirEnabled} />
              </div>
              <button className="watch-dir-add-button" onClick={addWatchDir}>添加</button>
            </div>
            {watchDirs.length ? watchDirs.map((dir) => (
              <div className="dir-item" key={dir.id}>
                <div>
                  <strong>{dir.path}</strong>
                  <small>{dir.enabled ? '自动监听' : '仅手动补扫'} · {dir.recursive ? '递归' : '当前层'}</small>
                </div>
                <div className="inline-actions">
                  <button className="secondary" onClick={() => void updateWatchDir(dir, { enabled: !dir.enabled })}>{dir.enabled ? '关闭监听' : '开启监听'}</button>
                  <button onClick={() => openRescanDialog('dir', dir.path)} disabled={rescanning}>补扫</button>
                  <button className="danger" onClick={() => deleteWatchDir(dir.id)}>删除</button>
                </div>
              </div>
            )) : <p className="muted">尚未配置媒体目录。</p>}
          </Card>
        </section>
      )}

        {activePage === 'rename' && (
        <section className="page-grid rename-page-grid">
          <Card title="整理命名" action={<button onClick={previewRename} disabled={previewingRename}>{previewingRename ? `扫描中 ${renamePreviewCount}` : '生成预览'}</button>}>
            <div className="rename-controls">
              <label>目录或文件路径<div className="path-input"><input value={renamePath} onChange={(event) => setRenamePath(event.target.value)} placeholder="D:\\Media\\Anime\\Season 1" /><button type="button" onClick={() => setDirectoryPicker({ title: '选择整理目录', value: renamePath, onSelect: setRenamePath })}>选择</button></div></label>
              <label>命名模板
                <div className="template-input-row">
                  <button className="target-path-preview rename-template-preview" type="button" onClick={() => setRenameTemplateEditorOpen(true)}>{renameTemplate || defaultRenameTemplate}</button>
                  <select value="" onChange={(event) => { if (event.target.value) setRenameTemplate(event.target.value); }} disabled={!renameTemplateHistory.length} title="最近模板">
                    <option value="">最近模板</option>
                    {renameTemplateHistory.map((template) => <option key={template} value={template}>{template}</option>)}
                  </select>
                </div>
              </label>
              <SelectField label="查询语言" value={renameLanguage} options={languageOptions} onChange={setRenameLanguage} />
              <Toggle label="缺少 NFO 时查询 TMDB" checked={renameUseTmdb} onChange={setRenameUseTmdb} />
            </div>
            <p className="muted">查询语言用于缺少 NFO 或 NFO 语言不匹配时查询 TMDB 元数据。模板可填写文件名或完整路径；{'{season:00}'} / {'{episode:000}'} 这类全 0 格式可控制补零位数。预览确认后可勾选文件执行重命名，并同步同基名附属文件。</p>
          </Card>

          <Card title="重命名预览">
            <div className="rename-match-bar">
              <div className="inline-actions rename-bulk-actions">
                <button className="secondary" type="button" onClick={selectAllRenameItems} disabled={!renamePreview.length}>全选</button>
                <button className="secondary" type="button" onClick={invertRenameSelection} disabled={!renamePreview.length}>反选</button>
                <button className="secondary" type="button" onClick={openBatchEpisodeDialog} disabled={!selectedRenamePaths.length}>批量修正季集</button>
                <button type="button" onClick={applySelectedRenames} disabled={applyingRename || !selectedRenamePaths.length}>{applyingRename ? '重命名中' : `执行选中重命名 (${selectedRenamePaths.length})`}</button>
              </div>
              <div className="path-input">
                <input value={tmdbQuery} onChange={(event) => setTmdbQuery(event.target.value)} placeholder="搜索 TMDB 剧集，例如 Frieren" />
                <button type="button" onClick={searchTmdbShows} disabled={searchingTmdb}>{searchingTmdb ? '搜索中' : '搜索剧集'}</button>
              </div>
              {tmdbResults.length ? (
                <div className="tmdb-results">
                  {tmdbResults.map((show) => (
                    <button type="button" key={show.id} onClick={() => applyTmdbShowToSelected(show)} title="套用到勾选项并按各自行季集重新获取标题">
                      {show.name || show.originalName} <small>{show.firstAirDate?.slice(0, 4) || '----'} · #{show.id}</small>
                    </button>
                  ))}
                </div>
              ) : <p className="muted">勾选文件后搜索剧集，点击候选即可套用到选中项并重新预览。</p>}
            </div>
            <div className="task-table-wrap">
              <table className="task-table rename-table">
                <thead>
                  <tr>
                    <th>选择</th>
                    <th>状态</th>
                    <th>来源</th>
                    <th>识别结果</th>
                    <th>原文件名</th>
                    <th>新文件名</th>
                    <th>说明</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {renamePreview.length ? renamePreview.map((item, index) => (
                    <tr className={selectedRenamePaths.includes(item.path) ? 'rename-row selected' : 'rename-row'} key={item.path} onClick={(event) => handleRenameRowClick(event, item, index)} title="点击行选择，Shift+点击连续选择">
                      <td><span className={selectedRenamePaths.includes(item.path) ? 'rename-row-index selected' : 'rename-row-index'} aria-hidden="true"><strong>{index + 1}</strong></span></td>
                      <td><span className={`pill ${item.status === 'error' ? 'bad' : item.status === 'ok' ? 'ok' : ''}`}>{item.status}</span></td>
                      <td>{item.source || '-'}</td>
                      <td className="rename-edit-cell">
                        <input value={item.show || ''} onChange={(event) => updateRenameItem(item.path, { show: event.target.value })} placeholder="剧名" />
                        <div className="rename-episode-edit">
                          <input type="number" min="0" value={item.season ?? 0} onChange={(event) => updateRenameItem(item.path, { season: Number(event.target.value) })} onKeyDown={(event) => { if (event.key === 'Enter') void recalculateRenameItem(item, { forceTmdb: true }); }} title="季，回车重新查 TMDB" />
                          <input type="number" min="0" value={item.episode ?? 0} onChange={(event) => updateRenameItem(item.path, { episode: Number(event.target.value) })} onKeyDown={(event) => { if (event.key === 'Enter') void recalculateRenameItem(item, { forceTmdb: true }); }} title="集，回车重新查 TMDB" />
                        </div>
                        <input value={item.title || ''} onChange={(event) => updateRenameItem(item.path, { title: event.target.value })} placeholder="标题" />
                        {item.tmdbShowId ? <small>TMDB #{item.tmdbShowId}</small> : null}
                      </td>
                      <td className="path-cell">{item.currentName}</td>
                      <td className="rename-target-cell">
                        <button className="target-path-preview" type="button" title={getRenameTargetDisplayValue(item)} onClick={() => setTargetPathEditor({ path: item.path, value: getRenameTargetEditorValue(item) })}>
                          <RenameTargetPathDisplay value={getRenameTargetDisplayValue(item)} />
                        </button>
                      </td>
                      <td className="path-cell">{item.conflict ? '目标文件已存在' : item.message || '-'}</td>
                      <td>
                        <div className="inline-actions rename-row-actions">
                          <button className="secondary" type="button" onClick={() => recalculateRenameItem({ ...item, manualName: false }, { keepManualName: false })}>按模板</button>
                          <button type="button" onClick={() => recalculateRenameItem(item, { forceTmdb: true })}>查 TMDB</button>
                        </div>
                      </td>
                    </tr>
                  )) : (
                    <tr><td colSpan={8} className="empty-cell">尚未生成预览。</td></tr>
                  )}
                </tbody>
              </table>
            </div>
          </Card>

          <Card title="重命名历史" action={<button className="secondary" onClick={() => void loadRenameHistory()} disabled={loadingRenameHistory}>{loadingRenameHistory ? '刷新中' : '刷新历史'}</button>}>
            {renameHistory.length ? renameHistory.map((batch) => (
              <div className="history-item" key={batch.id}>
                <div className="history-summary">
                  <button className="secondary" type="button" onClick={() => toggleHistoryDetails(batch.id)}>{expandedHistoryIds.includes(batch.id) ? '收起' : '详情'}</button>
                  <div>
                    <strong>{formatStoredTime(batch.createdAt, displayTimezone)}</strong>
                    <small>{batch.items.length} 项 · {batch.id}{batch.undone ? ` · 已撤销 ${batch.undoneAt ? formatStoredTime(batch.undoneAt, displayTimezone) : ''}` : ''}</small>
                  </div>
                  <div className="inline-actions">
                    <button className="secondary" onClick={() => void undoRenameBatch(batch.id)} disabled={batch.undone || undoingHistoryId === batch.id}>{batch.undone ? '已撤销' : undoingHistoryId === batch.id ? '撤销中' : '撤销'}</button>
                  </div>
                </div>
                {expandedHistoryIds.includes(batch.id) && <HistoryDetails batch={batch} undoCheck={undoCheckResult?.batch?.id === batch.id ? undoCheckResult : null} />}
              </div>
            )) : <p className="muted">暂无重命名历史。</p>}
          </Card>
        </section>
      )}

        {activePage === 'tasks' && (
        <section className="page-grid task-page-grid">
          <Card title="任务列表" action={<button className="danger" onClick={cancelActiveTasks} disabled={cancelingTasks}>{cancelingTasks ? '取消中' : '取消待执行/执行中'}</button>}>
            <div className="task-filters">
              <label>路径<input value={taskPathFilter} onChange={(event) => setTaskPathFilter(event.target.value)} placeholder="输入路径关键字" /></label>
              <label>开始时间（{displayTimezone}）<input type="datetime-local" value={taskFromFilter} onChange={(event) => setTaskFromFilter(event.target.value)} /></label>
              <label>结束时间（{displayTimezone}）<input type="datetime-local" value={taskToFilter} onChange={(event) => setTaskToFilter(event.target.value)} /></label>
              <div className="filter-actions">
                <button onClick={() => loadTasks(1)}>过滤</button>
                <button className="secondary" onClick={resetTaskFilters}>重置</button>
              </div>
            </div>
            <div className="task-table-wrap">
              <table className="task-table">
                <thead>
                  <tr>
                    <th>ID</th>
                    <th>状态</th>
                    <th>类型</th>
                    <th>路径</th>
                    <th>创建时间</th>
                    <th>错误</th>
                  </tr>
                </thead>
                <tbody>
                  {tasks.length ? tasks.map((task) => (
                    <tr key={task.id} onClick={() => loadTaskDetail(task.id)}>
                      <td>#{task.id}</td>
                      <td><span className={`pill ${task.status === 'completed' ? 'ok' : task.status === 'failed' || task.status === 'canceled' ? 'bad' : ''}`}>{task.status}</span></td>
                      <td>{task.type}</td>
                      <td className="path-cell">{task.mediaPath || '-'}</td>
                      <td>{formatStoredTime(task.createdAt, displayTimezone)}</td>
                      <td className="path-cell">{task.errorSummary || '-'}</td>
                    </tr>
                  )) : (
                    <tr><td colSpan={6} className="empty-cell">暂无任务。</td></tr>
                  )}
                </tbody>
              </table>
            </div>
            <div className="pagination-bar">
              <span>共 {taskTotal} 条，第 {taskPage} / {Math.max(1, Math.ceil(taskTotal / taskPageSize))} 页</span>
              <div className="inline-actions">
                <button className="secondary" disabled={taskPage <= 1} onClick={() => loadTasks(taskPage - 1)}>上一页</button>
                <button className="secondary" disabled={taskPage >= Math.ceil(taskTotal / taskPageSize)} onClick={() => loadTasks(taskPage + 1)}>下一页</button>
              </div>
            </div>
          </Card>

          <Card title="最近产物">
            {artifacts.length ? artifacts.map((artifact) => <ArtifactRow key={artifact.id} artifact={artifact} timezone={displayTimezone} />) : <p className="muted">暂无产物。</p>}
          </Card>
          {selectedTask && <TaskDetailModal detail={selectedTask} timezone={displayTimezone} onClose={() => setSelectedTask(null)} />}
        </section>
      )}
      {rescanOpen && <RescanModal scope={rescanScope} target={rescanTarget} strategy={rescanStrategy} directories={watchDirs} rescanning={rescanning} onClose={() => setRescanOpen(false)} onScopeChange={setRescanScope} onTargetChange={setRescanTarget} onStrategyChange={setRescanStrategy} onBrowsePath={() => setDirectoryPicker({ title: '选择补扫目录', value: rescanTarget, onSelect: setRescanTarget })} onSubmit={() => void rescan()} />}
      {batchEpisodeOpen && <BatchEpisodeModal count={selectedRenamePaths.length} season={batchSeason} mode={batchEpisodeMode} offset={batchEpisodeOffset} start={batchEpisodeStart} applying={applyingBatchEpisode} onClose={() => setBatchEpisodeOpen(false)} onSeasonChange={setBatchSeason} onModeChange={setBatchEpisodeMode} onOffsetChange={setBatchEpisodeOffset} onStartChange={setBatchEpisodeStart} onSubmit={() => void applyBatchEpisodeFix()} />}
      {renameTemplateEditorOpen && <RenameTemplateEditorModal value={renameTemplate} placeholders={renamePlaceholders} onChange={setRenameTemplate} onClose={() => setRenameTemplateEditorOpen(false)} />}
      {targetPathEditor && <TargetPathEditorModal value={targetPathEditor.value} onChange={(value) => setTargetPathEditor({ ...targetPathEditor, value })} onClose={() => setTargetPathEditor(null)} onSubmit={applyTargetPathEdit} />}
      {directoryPicker && <DirectoryPicker title={directoryPicker.title} initialPath={directoryPicker.value} onClose={() => setDirectoryPicker(null)} onSelect={(path) => { directoryPicker.onSelect(path); setDirectoryPicker(null); }} />}
      </section>
    </main>
  );
}

function TabButton(props: { active: boolean; label: string; onClick: () => void }) {
  return <button className={props.active ? 'tab-button active' : 'tab-button'} onClick={props.onClick}>{props.label}</button>;
}

function Card(props: { title: string; action?: React.ReactNode; children: React.ReactNode }) {
  return (
    <section className="card">
      <div className="card-header">
        <h2>{props.title}</h2>
        {props.action}
      </div>
      {props.children}
    </section>
  );
}

function Row(props: { label: string; value: string }) {
  return (
    <div className="row">
      <span>{props.label}</span>
      <strong>{props.value}</strong>
    </div>
  );
}

function ArtifactRow(props: { artifact: Artifact; timezone: string }) {
  return <Row label={`${props.artifact.type} · ${formatStoredTime(props.artifact.createdAt, props.timezone)}`} value={props.artifact.path} />;
}

function TaskDetailModal(props: { detail: TaskDetail; timezone: string; onClose: () => void }) {
  return (
    <div className="modal-backdrop" onClick={props.onClose}>
      <section className="modal-card" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <h2>任务详情</h2>
          <button className="secondary" onClick={props.onClose}>关闭</button>
        </div>
        <Row label="任务" value={`${props.detail.task.type} #${props.detail.task.id}`} />
        {props.detail.task.mediaPath && <Row label="文件" value={props.detail.task.mediaPath} />}
        <Row label="覆盖已有" value={props.detail.task.overwriteExisting ? '是' : '否'} />
        <Row label="状态" value={props.detail.task.status} />
        <Row label="尝试次数" value={String(props.detail.task.attempts)} />
        <Row label="创建时间" value={formatStoredTime(props.detail.task.createdAt, props.timezone)} />
        {props.detail.task.startedAt && <Row label="开始时间" value={formatStoredTime(props.detail.task.startedAt, props.timezone)} />}
        {props.detail.task.finishedAt && <Row label="结束时间" value={formatStoredTime(props.detail.task.finishedAt, props.timezone)} />}
        {props.detail.task.errorSummary && <Row label="错误" value={props.detail.task.errorSummary} />}
        <h3>日志</h3>
        {asArray<TaskLog>(props.detail.logs).length ? asArray<TaskLog>(props.detail.logs).map((log) => (
          <div className="log-line" key={log.id}>
            <span className={`pill ${log.level === 'error' ? 'bad' : 'ok'}`}>{log.level}</span>
            <div>
              <strong>{log.message}</strong>
              <small>{log.detail || formatStoredTime(log.createdAt, props.timezone)}</small>
            </div>
          </div>
        )) : <p className="muted">暂无日志。</p>}
        <h3>产物</h3>
        {asArray<Artifact>(props.detail.artifacts).length ? asArray<Artifact>(props.detail.artifacts).map((artifact) => (
          <ArtifactRow key={artifact.id} artifact={artifact} timezone={props.timezone} />
        )) : <p className="muted">暂无产物。</p>}
      </section>
    </div>
  );
}

function RescanModal(props: {
  scope: RescanScope;
  target: string;
  strategy: RescanStrategy;
  directories: WatchDir[];
  rescanning: boolean;
  onClose: () => void;
  onScopeChange: (value: RescanScope) => void;
  onTargetChange: (value: string) => void;
  onStrategyChange: (value: RescanStrategy) => void;
  onBrowsePath: () => void;
  onSubmit: () => void;
}) {
  return (
    <div className="modal-backdrop" onClick={props.onClose}>
      <section className="modal-card" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <h2>补扫</h2>
          <button className="secondary" onClick={props.onClose}>关闭</button>
        </div>
        <div className="task-filters rescan-modal-grid">
          <label>
            范围
            <select value={props.scope} onChange={(event) => props.onScopeChange(event.target.value as RescanScope)}>
              <option value="all">全部媒体目录</option>
              <option value="dir">指定媒体目录</option>
              <option value="path">指定路径</option>
            </select>
          </label>
          {props.scope === 'dir' && (
            <label>
              媒体目录
              <select value={props.target} onChange={(event) => props.onTargetChange(event.target.value)}>
                <option value="">请选择</option>
                {props.directories.map((dir) => <option key={dir.id} value={dir.path}>{dir.path}</option>)}
              </select>
            </label>
          )}
          {props.scope === 'path' && (
            <label>
              路径
              <div className="path-input"><input value={props.target} onChange={(event) => props.onTargetChange(event.target.value)} placeholder="D:\\Media\\Anime\\S01" /><button type="button" onClick={props.onBrowsePath}>选择</button></div>
            </label>
          )}
          <label>
            策略
            <select value={props.strategy} onChange={(event) => props.onStrategyChange(event.target.value as RescanStrategy)}>
              <option value="missing">只补缺失</option>
              <option value="force">强制重建</option>
            </select>
          </label>
        </div>
        <div className="inline-actions modal-actions">
          <button className="secondary" onClick={props.onClose}>取消</button>
          <button onClick={props.onSubmit} disabled={props.rescanning}>{props.rescanning ? '补扫中' : '开始补扫'}</button>
        </div>
      </section>
    </div>
  );
}

function BatchEpisodeModal(props: {
  count: number;
  season: number;
  mode: BatchEpisodeMode;
  offset: number;
  start: number;
  applying: boolean;
  onClose: () => void;
  onSeasonChange: (value: number) => void;
  onModeChange: (value: BatchEpisodeMode) => void;
  onOffsetChange: (value: number) => void;
  onStartChange: (value: number) => void;
  onSubmit: () => void;
}) {
  return (
    <div className="modal-backdrop" onClick={props.onClose}>
      <section className="modal-card batch-episode-modal" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <h2>批量修正季集</h2>
          <button className="secondary" onClick={props.onClose}>关闭</button>
        </div>
        <p className="muted">将应用到当前勾选的 {props.count} 个文件，并按修正后的季集重新查询 TMDB 预览。</p>
        <div className="config-form batch-episode-form">
          <label>目标季<input type="number" min="0" value={props.season} onChange={(event) => props.onSeasonChange(Number(event.target.value))} /></label>
          <div className="batch-mode-list">
            <label><input type="radio" checked={props.mode === 'keep'} onChange={() => props.onModeChange('keep')} /> 保留当前集数</label>
            <label><input type="radio" checked={props.mode === 'offset'} onChange={() => props.onModeChange('offset')} /> 当前集数偏移</label>
            {props.mode === 'offset' && <input type="number" value={props.offset} onChange={(event) => props.onOffsetChange(Number(event.target.value))} placeholder="例如 -12" />}
            <label><input type="radio" checked={props.mode === 'sequence'} onChange={() => props.onModeChange('sequence')} /> 按列表顺序重排</label>
            {props.mode === 'sequence' && <input type="number" min="0" value={props.start} onChange={(event) => props.onStartChange(Number(event.target.value))} placeholder="起始集" />}
          </div>
        </div>
        <div className="inline-actions modal-actions">
          <button className="secondary" onClick={props.onClose}>取消</button>
          <button onClick={props.onSubmit} disabled={props.applying}>{props.applying ? '应用中' : '应用并查 TMDB'}</button>
        </div>
      </section>
    </div>
  );
}

function HistoryDetails(props: { batch: RenameHistoryBatch; undoCheck: RenameUndoCheckResult | null }) {
  const failedChecks = new Map((props.undoCheck?.items ?? []).filter((item) => !item.ok).map((item) => [`${item.from}\n${item.to}`, item.reason]));
  return (
    <div className="history-details">
      {props.batch.items.map((item, itemIndex) => (
        <div className="history-detail-item" key={`${item.path}-${itemIndex}`}>
          <strong>{item.status}</strong>
          <small>{item.message || '-'}</small>
          <div className="history-moves">
            {item.moves.map((move, moveIndex) => {
              const reason = failedChecks.get(`${move.from}\n${move.to}`);
              return (
                <div className={reason ? 'history-move bad' : 'history-move'} key={`${move.from}-${moveIndex}`}>
                  <span>{move.from}</span>
                  <span>{move.to}</span>
                  {reason && <em>{reason}</em>}
                </div>
              );
            })}
          </div>
        </div>
      ))}
    </div>
  );
}

function RenameTemplateEditorModal(props: { value: string; placeholders: string[]; onChange: (value: string) => void; onClose: () => void }) {
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);

  useEffect(() => {
    const textarea = textareaRef.current;
    if (!textarea) return;
    textarea.focus();
    textarea.setSelectionRange(props.value.length, props.value.length);
  }, []);

  function insertPlaceholder(placeholder: string) {
    const textarea = textareaRef.current;
    if (!textarea) {
      props.onChange(props.value + placeholder);
      return;
    }
    const start = textarea.selectionStart ?? props.value.length;
    const end = textarea.selectionEnd ?? start;
    const next = props.value.slice(0, start) + placeholder + props.value.slice(end);
    props.onChange(next);
    requestAnimationFrame(() => {
      textarea.focus();
      const cursor = start + placeholder.length;
      textarea.setSelectionRange(cursor, cursor);
    });
  }

  return (
    <div className="modal-backdrop" onClick={props.onClose}>
      <section className="modal-card rename-template-modal" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <h2>编辑命名模板</h2>
          <button className="secondary" onClick={props.onClose}>关闭</button>
        </div>
        <textarea ref={textareaRef} value={props.value} onChange={(event) => props.onChange(event.target.value)} placeholder={defaultRenameTemplate} autoFocus />
        <div className="placeholder-bar modal-placeholder-bar">
          <span>插入占位符：</span>
          {props.placeholders.map((placeholder) => <button className="secondary" type="button" key={placeholder} onClick={() => insertPlaceholder(placeholder)}>{placeholder}</button>)}
        </div>
        <p className="muted">可填写文件名、相对路径或完整路径；{'{season:00}'} / {'{episode:000}'} 可控制补零位数。</p>
        <div className="inline-actions modal-actions">
          <button onClick={props.onClose}>完成</button>
        </div>
      </section>
    </div>
  );
}

function TargetPathEditorModal(props: { value: string; onChange: (value: string) => void; onClose: () => void; onSubmit: () => void }) {
  return (
    <div className="modal-backdrop" onClick={props.onClose}>
      <section className="modal-card target-path-modal" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <h2>编辑目标路径</h2>
          <button className="secondary" onClick={props.onClose}>关闭</button>
        </div>
        <textarea value={props.value} onChange={(event) => props.onChange(event.target.value)} autoFocus />
        <p className="muted">可以填写文件名、相对路径或完整路径。执行前仍会检查目标冲突。</p>
        <div className="inline-actions modal-actions">
          <button className="secondary" onClick={props.onClose}>取消</button>
          <button onClick={props.onSubmit}>应用</button>
        </div>
      </section>
    </div>
  );
}

function DirectoryPicker(props: { title: string; initialPath: string; onSelect: (path: string) => void; onClose: () => void }) {
  const [currentPath, setCurrentPath] = useState(props.initialPath);
  const [data, setData] = useState<DirectoryList>({ path: '', parent: '', entries: [] });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    void load(currentPath);
  }, []);

  async function load(path: string) {
    setLoading(true);
    setError('');
    try {
      const params = new URLSearchParams();
      if (path.trim()) params.set('path', path.trim());
      const response = await fetch(`/api/fs/directories?${params.toString()}`);
      if (!response.ok) {
        setError(await response.text());
        return;
      }
      const result = await response.json();
      setData({ ...result, entries: asArray<DirectoryEntry>(result.entries) });
      setCurrentPath(result.path || path);
    } catch (err) {
      setError(err instanceof Error ? err.message : '读取目录失败');
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="modal-backdrop" onClick={props.onClose}>
      <section className="modal-card" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <h2>{props.title}</h2>
          <button className="secondary" onClick={props.onClose}>关闭</button>
        </div>
        <div className="form-row">
          <input value={currentPath} onChange={(event) => setCurrentPath(event.target.value)} placeholder="选择磁盘或输入路径" />
          <button onClick={() => load(currentPath)} disabled={loading}>{loading ? '读取中' : '打开'}</button>
        </div>
        {error && <section className="error-card directory-error">{error}</section>}
        <div className="directory-list">
          {data.parent && <button className="directory-item" onClick={() => load(data.parent)}>..</button>}
          {data.entries.map((entry) => <button className="directory-item" key={entry.path} onClick={() => load(entry.path)}>{entry.name}</button>)}
          {!data.entries.length && !data.parent && <p className="muted">没有可显示的目录。</p>}
        </div>
        <div className="inline-actions modal-actions">
          <button className="secondary" onClick={props.onClose}>取消</button>
          <button onClick={() => props.onSelect(currentPath)} disabled={!currentPath.trim()}>选择当前目录</button>
        </div>
      </section>
    </div>
  );
}

function Flag(props: { label: string; enabled?: boolean }) {
  return <Row label={props.label} value={props.enabled ? '开启' : '关闭'} />;
}

function Toggle(props: { label: string; checked: boolean; onChange: (value: boolean) => void }) {
  return (
    <label className="toggle-row">
      <span>{props.label}</span>
      <input type="checkbox" checked={props.checked} onChange={(event) => props.onChange(event.target.checked)} />
    </label>
  );
}

function SelectField(props: { label: string; value: string; options: Array<{ code: string; name: string }>; onChange: (value: string) => void }) {
  return (
    <label>
      {props.label}
      <select value={props.value} onChange={(event) => props.onChange(event.target.value)}>
        {props.options.map((option) => (
          <option key={option.code} value={option.code}>{option.name} ({option.code})</option>
        ))}
      </select>
    </label>
  );
}

function LanguagePicker(props: { label: string; value: string; onChange: (value: string) => void }) {
  const [query, setQuery] = useState('');
  const options = filterLanguages(query);
  const current = languageLabel(props.value);
  return (
    <label className="language-picker">
      <span>{props.label}</span>
      <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={`当前：${current}`} />
      <div className="language-options">
        {options.map((option) => (
          <button
            className={option.code === props.value ? 'language-option selected' : 'language-option'}
            key={option.code}
            type="button"
            onClick={() => {
              props.onChange(option.code);
              setQuery('');
            }}
          >
            {option.name} <small>{option.code}</small>
          </button>
        ))}
      </div>
    </label>
  );
}

function LanguageMultiPicker(props: { label: string; values: string[]; onChange: (values: string[]) => void }) {
  const [query, setQuery] = useState('');
  const [open, setOpen] = useState(false);
  const [dragging, setDragging] = useState<string | null>(null);
  const selected = props.values.filter(Boolean);
  const selectedSet = new Set(selected.map((value) => value.toLowerCase()));
  const options = filterLanguages(query).filter((option) => !selectedSet.has(option.code.toLowerCase()));

  function remove(code: string) {
    props.onChange(selected.filter((value) => value !== code));
  }

  function add(code: string) {
    props.onChange([...selected, code]);
    setQuery('');
  }

  function move(dragCode: string, targetCode: string) {
    if (dragCode === targetCode) return;
    const next = selected.filter((code) => code !== dragCode);
    const targetIndex = next.indexOf(targetCode);
    if (targetIndex < 0) return;
    next.splice(targetIndex, 0, dragCode);
    props.onChange(next);
  }

  return (
    <div className="language-picker">
      <span>{props.label}</span>
      <div className="selected-languages sortable-languages">
        {selected.length ? selected.map((code, index) => (
          <button
            className={dragging === code ? 'language-chip dragging' : 'language-chip'}
            draggable
            key={code}
            type="button"
            onClick={() => remove(code)}
            onDragStart={(event) => {
              setDragging(code);
              event.dataTransfer.effectAllowed = 'move';
              event.dataTransfer.setData('text/plain', code);
            }}
            onDragOver={(event) => event.preventDefault()}
            onDrop={(event) => {
              event.preventDefault();
              move(event.dataTransfer.getData('text/plain') || dragging || '', code);
              setDragging(null);
            }}
            onDragEnd={() => setDragging(null)}
            title="拖拽排序，点击移除"
          >
            <span className="drag-handle">::</span>{index + 1}. {languageLabel(code)}
          </button>
        )) : <small className="muted">未选择备用语言。</small>}
      </div>
      <button className="language-dropdown-trigger" type="button" onClick={() => setOpen((value) => !value)}>
        {open ? '收起语言列表' : '选择备用语言'}
      </button>
      {open && (
        <div className="language-dropdown">
          <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索语言" />
          <div className="language-options dropdown-options">
            {options.length ? options.map((option) => (
              <button className="language-option" key={option.code} type="button" onClick={() => add(option.code)}>
                {option.name} <small>{option.code}</small>
              </button>
            )) : <small className="muted">没有可添加的语言。</small>}
          </div>
        </div>
      )}
    </div>
  );
}

function asArray<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : [];
}

function filterLanguages(query: string): LanguageOption[] {
  const normalized = query.trim().toLowerCase();
  if (!normalized) return languageOptions;
  return languageOptions.filter((option) => `${option.code} ${option.name}`.toLowerCase().includes(normalized));
}

function formatStoredTime(value: string, timezone: string): string {
  const date = parseStoredTime(value);
  if (!date) return value || '-';
  try {
    return formatDateInTimeZone(date, timezone);
  } catch {
    return value;
  }
}

function zonedInputToUTC(value: string, timezone: string, endOfMinute: boolean): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(value);
  if (!match) return value;
  const parts = {
    year: Number(match[1]),
    month: Number(match[2]),
    day: Number(match[3]),
    hour: Number(match[4]),
    minute: Number(match[5]),
    second: endOfMinute ? 59 : 0
  };
  let utcMillis = Date.UTC(parts.year, parts.month - 1, parts.day, parts.hour, parts.minute, parts.second);
  utcMillis -= getTimeZoneOffset(new Date(utcMillis), timezone);
  utcMillis = Date.UTC(parts.year, parts.month - 1, parts.day, parts.hour, parts.minute, parts.second) - getTimeZoneOffset(new Date(utcMillis), timezone);
  return formatUTCForStore(new Date(utcMillis));
}

function parseStoredTime(value: string): Date | null {
  if (!value) return null;
  const normalized = value.includes('T') ? value : `${value.replace(' ', 'T')}Z`;
  const date = new Date(normalized.endsWith('Z') || /[+-]\d{2}:?\d{2}$/.test(normalized) ? normalized : `${normalized}Z`);
  return Number.isNaN(date.getTime()) ? null : date;
}

function formatDateInTimeZone(date: Date, timezone: string): string {
  const formatter = new Intl.DateTimeFormat('en-CA', {
    timeZone: timezone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23'
  });
  const parts = Object.fromEntries(formatter.formatToParts(date).map((part) => [part.type, part.value]));
  return `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}:${parts.second}`;
}

function getTimeZoneOffset(date: Date, timezone: string): number {
  const formatter = new Intl.DateTimeFormat('en-CA', {
    timeZone: timezone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23'
  });
  const parts = Object.fromEntries(formatter.formatToParts(date).map((part) => [part.type, part.value]));
  const zonedMillis = Date.UTC(
    Number(parts.year),
    Number(parts.month) - 1,
    Number(parts.day),
    Number(parts.hour),
    Number(parts.minute),
    Number(parts.second)
  );
  return zonedMillis - date.getTime();
}

function formatUTCForStore(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, '0');
  return `${date.getUTCFullYear()}-${pad(date.getUTCMonth() + 1)}-${pad(date.getUTCDate())} ${pad(date.getUTCHours())}:${pad(date.getUTCMinutes())}:${pad(date.getUTCSeconds())}`;
}

function languageLabel(code: string): string {
  const found = languageOptions.find((option) => option.code.toLowerCase() === code.toLowerCase());
  return found ? `${found.name} (${found.code})` : code;
}
