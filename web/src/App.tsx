import { useEffect, useRef, useState } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent, MouseEvent as ReactMouseEvent, ReactNode } from 'react';
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  CircleGauge,
  CloudUpload,
  Database,
  FileCheck2,
  Film,
  FolderCog,
  FolderOpen,
  History,
  LayoutDashboard,
  ListTodo,
  Monitor,
  Moon,
  RefreshCw,
  Save,
  SearchCheck,
  Settings,
  SlidersHorizontal,
  Sun,
  Tags,
  UploadCloud,
  WandSparkles,
  X
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { getRuntimeInfo, notifyDesktop, pickDesktopDirectory, pickDesktopFile, previewDesktopRename, revealDesktopPath } from './desktop';
import type { DesktopRuntimeInfo } from './desktop';
import { applyThemeMode, readThemeMode } from './theme';
import type { ThemeMode } from './theme';

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
    bifHwAccel: string;
    strategy: RescanStrategy;
    enableSubtitles: boolean;
    enableMediaInfo: boolean;
    enableNfo: boolean;
    enableBif: boolean;
    enableImageTakeover: boolean;
  };
  upload: {
    enabled: boolean;
    concurrency: number;
    quietPeriod: number;
    maxAttempts: number;
    includeTypes: string[];
  };
  renaming: {
    concurrency: number;
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
    tmdbImageBaseUrl: string;
    tmdbRequestTimeoutSeconds: number;
    language: string;
    fallbackLanguages: string[];
    region: string;
    proxy: string;
  };
  watchDirs: WatchDir[];
};

type OutputProcessingConfig = {
  strategy: RescanStrategy;
  bifWidth: number;
  bifInterval: number;
  bifHwAccel: string;
  enableSubtitles: boolean;
  enableMediaInfo: boolean;
  enableNfo: boolean;
  enableBif: boolean;
  enableImageTakeover: boolean;
};

type WatchDir = { id: number; path: string; recursive: boolean; enabled: boolean; watchEnabled: boolean; scanOnStart: boolean; useGlobalProcessing: boolean; processing: OutputProcessingConfig };

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

type UploadProvider = {
  id: number;
  name: string;
  type: string;
  enabled: boolean;
  remoteRoot: string;
  userAgent: string;
  collisionPolicy: 'replace' | 'skip' | 'fail';
  hasCookie: boolean;
  routes: UploadProviderRoute[];
  createdAt: string;
  updatedAt: string;
};

type UploadProviderRoute = {
  id?: number;
  providerId?: number;
  watchDirId?: number;
  enabled: boolean;
  remoteRoot: string;
  collisionPolicy: 'replace' | 'skip' | 'fail';
  includeTypes: string[];
};

type UploadProviderDescriptor = {
  type: string;
  name: string;
  implemented: boolean;
  secretKeys: string[];
};

type UploadSummary = {
  collecting: number;
  pending: number;
  running: number;
  completed: number;
  failed: number;
};

type UploadBatch = {
  id: number;
  watchDirId?: number;
  seriesKey: string;
  seriesPath: string;
  status: string;
  revision: number;
  readyAt: string;
  fileCount: number;
  targetCount: number;
  completedTargets: number;
  failedTargets: number;
  createdAt: string;
  updatedAt: string;
};

type UploadBatchFile = {
  id: number;
  batchId: number;
  localPath: string;
  relativePath: string;
  fileType: string;
  size: number;
  modifiedAt: string;
};

type UploadBatchTarget = {
  id: number;
  batchId: number;
  providerId: number;
  providerName: string;
  providerType: string;
  remoteRoot: string;
  status: string;
  attempts: number;
  errorSummary: string;
  availableAt: string;
  startedAt?: string;
  finishedAt?: string;
};

type UploadTransfer = {
  id: number;
  batchTargetId: number;
  batchFileId: number;
  localPath: string;
  relativePath: string;
  fileType: string;
  remotePath: string;
  status: string;
  attempts: number;
  bytesTotal: number;
  bytesTransferred: number;
  errorSummary: string;
};

type UploadBatchDetail = {
  batch: UploadBatch;
  files: UploadBatchFile[];
  targets: UploadBatchTarget[];
  transfers: UploadTransfer[];
};

type UploadBatchListResponse = {
  items: UploadBatch[];
  total: number;
  page: number;
  pageSize: number;
};

type CookieAuthStatus = {
  sessionId: string;
  providerId: number;
  terminal: string;
  state: string;
  message: string;
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
};

type RenamePreviewItem = {
  path: string;
  currentName: string;
  newName: string;
  newPath: string;
  renderedTarget: string;
  show: string;
  showOriginal: string;
  title: string;
  season: number;
  episode: number;
  year: string;
  tmdbShowId: number;
  tmdbEpisodeId: number;
  source: string;
  identitySource: string;
  metadataSource: string;
  variables: Record<string, string>;
  status: string;
  message: string;
  conflict: boolean;
  sanitizedTitle: string;
  manualName: boolean;
  releaseGroup: string;
};

type TMDBSearchResult = {
  id: number;
  name: string;
  originalName: string;
  firstAirDate: string;
  overview: string;
};

type TmdbEpisodeDetail = {
  showId: number;
  episodeId: number;
  showName: string;
  showOriginalName: string;
  showFirstAirDate: string;
  showOverview: string;
  showStatus: string;
  showVoteAverage: number;
  showGenres: string[];
  showPosterUrl: string;
  season: number;
  episode: number;
  title: string;
  overview: string;
  airDate: string;
  voteAverage: number;
  stillUrl: string;
};

type RenamePreviewStreamMessage = {
  type: 'start' | 'item' | 'done' | 'error';
  item?: RenamePreviewItem;
  count?: number;
  total?: number;
  error?: string;
};

type RenameHistoryMove = { from: string; to: string };
type RenameHistoryItem = { path: string; newPath: string; status: string; message: string; moves: RenameHistoryMove[] };
type RenameHistoryBatch = { id: string; createdAt: string; undone: boolean; undoneAt?: string; items: RenameHistoryItem[] };
type RenameUndoCheckItem = { from: string; to: string; ok: boolean; reason: string };
type RenameUndoCheckResult = { canUndo: boolean; batch: RenameHistoryBatch; items: RenameUndoCheckItem[] };

type DirectoryEntry = { name: string; path: string };
type DirectoryList = { path: string; parent: string; entries: DirectoryEntry[] };

type EmbyAPIKey = { id: number; title: string; note: string; createdAt?: string; updatedAt?: string };

type AuditLocalEpisode = {
  season: number;
  episode: number;
  path: string;
  nfoPath?: string;
  title?: string;
  plot?: string;
  thumb?: string;
  hasImage: boolean;
  providerIds?: Record<string, string>;
};

type AuditSeasonReport = {
  season: number;
  expectedCount?: number;
  expectedSource?: string;
  expectedEpisodes?: number[];
  existingCount: number;
  existingEpisodes: number[];
  missingEpisodes?: number[];
  note?: string;
};

type AuditComparisonIssue = {
  severity: string;
  season?: number;
  episode?: number;
  field: string;
  local?: string;
  emby?: string;
  detail?: string;
};

type AuditReport = {
  root: string;
  showTitle?: string;
  tmdbShowId?: number;
  localEpisodes: AuditLocalEpisode[];
  seasonReports: AuditSeasonReport[];
  artifactIssues?: AuditComparisonIssue[];
  embyComparisons?: AuditComparisonIssue[];
  warnings?: string[];
};

type FileAuditIssue = {
  severity: string;
  type: string;
  path: string;
  local?: string;
  remote?: string;
  detail?: string;
};

type FileAuditReport = {
  localRoot: string;
  remoteRoot: string;
  localCount: number;
  remoteCount: number;
  issues?: FileAuditIssue[];
};

type RescanScope = 'all' | 'dir' | 'path';
type RescanStrategy = 'missing' | 'force';
type BatchEpisodeMode = 'keep' | 'offset' | 'sequence';

type LanguageOption = { code: string; name: string };
type RegionOption = { code: string; name: string };
type SelectOption = { code: string; name: string };
type PageKey = 'dashboard' | 'settings' | 'watchDirs' | 'tasks' | 'uploads' | 'rename' | 'audit';
type SettingsTab = 'basic' | 'processing' | 'uploads' | 'scraping' | 'sources';

function defaultOutputProcessing(): OutputProcessingConfig {
  return {
    strategy: 'missing',
    bifWidth: 320,
    bifInterval: 10,
    bifHwAccel: 'cpu',
    enableSubtitles: true,
    enableMediaInfo: true,
    enableNfo: true,
    enableBif: true,
    enableImageTakeover: false
  };
}

function outputProcessingFromConfig(config: AppConfig | null): OutputProcessingConfig {
  if (!config) return defaultOutputProcessing();
  return {
    strategy: config.processing.strategy || 'missing',
    bifWidth: config.processing.bifWidth,
    bifInterval: config.processing.bifInterval,
    bifHwAccel: config.processing.bifHwAccel || 'cpu',
    enableSubtitles: config.processing.enableSubtitles,
    enableMediaInfo: config.processing.enableMediaInfo,
    enableNfo: config.processing.enableNfo,
    enableBif: config.processing.enableBif,
    enableImageTakeover: config.processing.enableImageTakeover
  };
}
type TaskStatusFilter = 'all' | 'pending' | 'running' | 'completed' | 'failed' | 'ignored' | 'canceled';
type UploadStatusFilter = 'all' | 'collecting' | 'pending' | 'running' | 'completed' | 'partial' | 'failed' | 'canceled';
type AuditTab = 'missing' | 'emby' | 'files';
type ConfirmationRequest = {
  title: string;
  message: string;
  confirmLabel: string;
  tone?: 'default' | 'danger';
  onConfirm: () => void | Promise<void>;
};

const pagePaths: Record<PageKey, string> = {
  dashboard: '/',
  settings: '/settings',
  watchDirs: '/watch-dirs',
  tasks: '/tasks',
  uploads: '/uploads',
  rename: '/rename',
  audit: '/audit'
};

const pageMeta: Record<PageKey, { title: string; description: string; icon: LucideIcon }> = {
  dashboard: { title: '运行概览', description: '查看处理队列、媒体目录和本地工具的实时状态。', icon: LayoutDashboard },
  rename: { title: '整理命名', description: '预览、核对并安全应用剧集文件命名。', icon: Tags },
  audit: { title: '剧集核对', description: '检查缺集、伴生文件以及 Emby 和远端文件差异。', icon: SearchCheck },
  watchDirs: { title: '媒体目录', description: '管理自动监听、扫描范围和目录级处理策略。', icon: FolderCog },
  tasks: { title: '任务队列', description: '跟踪处理进度、诊断失败并管理重试。', icon: ListTodo },
  uploads: { title: '上传管理', description: '管理发布目标、批次进度和远端传输。', icon: CloudUpload },
  settings: { title: '应用设置', description: '配置工具、处理策略、刮削来源和上传行为。', icon: Settings }
};

const workspacePages: PageKey[] = ['dashboard', 'rename', 'audit'];
const automationPages: PageKey[] = ['watchDirs', 'tasks', 'uploads'];

const settingsTabOptions: Array<{ value: SettingsTab; label: string }> = [
  { value: 'basic', label: '基础' },
  { value: 'processing', label: '处理' },
  { value: 'uploads', label: '上传' },
  { value: 'scraping', label: '刮削' },
  { value: 'sources', label: '数据源' }
];

const themeOptions: Array<{ value: ThemeMode; label: string; icon: LucideIcon }> = [
  { value: 'system', label: '跟随系统', icon: Monitor },
  { value: 'light', label: '浅色', icon: Sun },
  { value: 'dark', label: '深色', icon: Moon }
];

function pageFromPath(pathname: string): PageKey {
  switch (pathname) {
    case '/settings':
      return 'settings';
    case '/watch-dirs':
      return 'watchDirs';
    case '/tasks':
      return 'tasks';
    case '/uploads':
      return 'uploads';
    case '/rename':
      return 'rename';
    case '/audit':
      return 'audit';
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
const bifHwAccelOptions: SelectOption[] = [
  { code: 'cpu', name: 'CPU（最稳定）' },
  { code: 'auto', name: '自动识别并回退' },
  { code: 'nvidia', name: 'NVIDIA CUDA' },
  { code: 'amd', name: 'AMD（D3D11VA/DXVA2/VAAPI）' },
  { code: 'intel', name: 'Intel QSV' },
  { code: 'd3d11va', name: 'Windows D3D11VA' },
  { code: 'dxva2', name: 'Windows DXVA2' },
  { code: 'vaapi', name: 'Linux VAAPI' },
  { code: 'videotoolbox', name: 'macOS VideoToolbox' }
];
const imageSourceOptions: SelectOption[] = [
  { code: 'tmdb', name: 'TMDB' },
  { code: 'fanart', name: 'Fanart' }
];
const commonVideoExtensions = ['.mkv', '.mp4', '.ts', '.m2ts', '.mts', '.mov', '.m4v', '.avi', '.wmv', '.flv', '.webm', '.rmvb', '.rm', '.mpg', '.mpeg', '.vob', '.asf'];
const uploadTypeOptions: { value: string; label: string }[] = [
  { value: 'video', label: 'Video' },
  { value: 'mediainfo', label: 'MediaInfo' },
  { value: 'subtitle', label: 'Subtitle' },
  { value: 'nfo', label: 'Episode NFO' },
  { value: 'thumb', label: 'Episode Thumb' },
  { value: 'tvshow-nfo', label: 'TV Show NFO' },
  { value: 'season-nfo', label: 'Season NFO' },
  { value: 'bif', label: 'BIF' },
  { value: 'poster', label: 'Poster' },
  { value: 'fanart', label: 'Fanart' },
  { value: 'clearlogo', label: 'Clearlogo' },
  { value: 'clearart', label: 'Clearart' },
  { value: 'season-poster', label: 'Season Poster' }
];
const taskStatusFilters: { value: TaskStatusFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'pending', label: 'Pending' },
  { value: 'running', label: 'Running' },
  { value: 'completed', label: 'Completed' },
  { value: 'failed', label: 'Failed' },
  { value: 'ignored', label: 'Ignored' },
  { value: 'canceled', label: 'Canceled' }
];
const uploadStatusFilters: { value: UploadStatusFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'collecting', label: 'Collecting' },
  { value: 'pending', label: 'Pending' },
  { value: 'running', label: 'Running' },
  { value: 'completed', label: 'Completed' },
  { value: 'partial', label: 'Partial' },
  { value: 'failed', label: 'Failed' },
  { value: 'canceled', label: 'Canceled' }
];
const taskListRefreshIntervalMs = 5000;
const taskDetailRefreshIntervalMs = 5000;
const uploadListRefreshIntervalMs = 5000;
const desktopNotificationPollIntervalMs = 10000;
const defaultRenameTemplate = '{show} - S{season:00}E{episode:00} - {title}';
const auditPreferencesKey = 'nya.audit.preferences';
const renamePlaceholders = [
  '{show}',
  '{showOriginal}',
  '{title}',
  '{releaseGroup}',
  '{tmid}',
  '{season}',
  '{episode}',
  '{year}'
];
const renamePreferencesKey = 'nya.rename.preferences';
const renameTemplateHistoryLimit = 20;

function previewWorkerCount(configured: number) {
  if (configured < 1) return 1;
  if (configured > 8) return 8;
  return configured;
}

function renameIdentitySourceLabel(source: string) {
  if (source === 'nfo') return 'NFO';
  if (source === 'pattern') return '自定义规则';
  if (source === 'filename') return '文件名 / 目录';
  return source || '-';
}

function renameMetadataSourceLabel(source: string) {
  if (source === 'tmdb') return 'TMDB 已获取';
  if (source === 'tmdb-error') return 'TMDB 查询失败';
  if (source === 'tmdb-unavailable') return 'TMDB 不可用';
  return source || '-';
}

async function runWithConcurrency<T>(items: T[], concurrency: number, worker: (item: T, index: number) => Promise<void>) {
  let nextIndex = 0;
  const workers = Array.from({ length: Math.min(concurrency, items.length) }, async () => {
    while (nextIndex < items.length) {
      const index = nextIndex++;
      await worker(items[index], index);
    }
  });
  await Promise.all(workers);
}

type RenamePreferences = {
  path?: string;
  template?: string;
  matchPattern?: string;
  language?: string;
  releaseGroup?: string;
  templateHistory?: string[];
};

type AuditPreferences = {
  root?: string;
  tmdbId?: string;
  includeSeasonZero?: boolean;
  embyItemUrl?: string;
  embyApiKeyId?: string;
  fileLocalRoot?: string;
  fileRemoteRoot?: string;
  sftpAddr?: string;
  sftpUser?: string;
  sftpKeyPath?: string;
  sftpKnownHostsPath?: string;
  sftpInsecureIgnoreHost?: boolean;
  allowStrmProxy?: boolean;
  compareSize?: boolean;
  compareMd5?: boolean;
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

function readAuditPreferences(): AuditPreferences {
  try {
    const raw = window.localStorage.getItem(auditPreferencesKey);
    if (!raw) return {};
    const value = JSON.parse(raw) as AuditPreferences;
    return value && typeof value === 'object' ? value : {};
  } catch {
    return {};
  }
}

function writeAuditPreferences(value: AuditPreferences) {
  try {
    window.localStorage.setItem(auditPreferencesKey, JSON.stringify(value));
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

function taskStatusPillClass(status: string) {
  switch (status) {
    case 'completed':
      return 'pill ok';
    case 'failed':
      return 'pill bad';
    case 'ignored':
      return 'pill ignored';
    case 'canceled':
      return 'pill warn';
    case 'running':
      return 'pill running';
    case 'pending':
      return 'pill pending';
    default:
      return 'pill';
  }
}

function uploadStatusPillClass(status: string) {
  switch (status) {
    case 'completed':
      return 'pill ok';
    case 'partial':
    case 'failed':
      return 'pill bad';
    case 'canceled':
      return 'pill warn';
    case 'running':
      return 'pill running';
    case 'collecting':
      return 'pill pending';
    case 'pending':
    case 'waiting':
      return 'pill pending';
    default:
      return 'pill';
  }
}

function logLevelPillClass(level: string) {
  switch (level) {
    case 'error':
      return 'pill bad';
    case 'warning':
    case 'warn':
      return 'pill warn';
    case 'debug':
      return 'pill pending';
    default:
      return 'pill ok';
  }
}

function normalizeTaskDetail(detail: TaskDetail) {
  return {
    ...detail,
    logs: asArray<TaskLog>(detail.logs),
    artifacts: asArray<Artifact>(detail.artifacts)
  };
}

export function App() {
  const [health, setHealth] = useState<Health | null>(null);
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [savedConfig, setSavedConfig] = useState<AppConfig | null>(null);
  const [runtimeInfo, setRuntimeInfo] = useState<DesktopRuntimeInfo | null>(null);
  const [themeMode, setThemeMode] = useState<ThemeMode>(() => readThemeMode());
  const [initialLoading, setInitialLoading] = useState(true);
  const [connectionOnline, setConnectionOnline] = useState(false);
  const [healthCheckedAt, setHealthCheckedAt] = useState<Date | null>(null);
  const [restartRequired, setRestartRequired] = useState(false);
  const [tools, setTools] = useState<ToolStatus[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [uploadSummary, setUploadSummary] = useState<UploadSummary>({ collecting: 0, pending: 0, running: 0, completed: 0, failed: 0 });
  const [uploadBatches, setUploadBatches] = useState<UploadBatch[]>([]);
  const [uploadProviders, setUploadProviders] = useState<UploadProvider[]>([]);
  const [uploadProviderTypes, setUploadProviderTypes] = useState<UploadProviderDescriptor[]>([]);
  const [settingsTab, setSettingsTab] = useState<SettingsTab>('basic');
  const [taskTotal, setTaskTotal] = useState(0);
  const [taskPage, setTaskPage] = useState(1);
  const [taskPageSize] = useState(20);
  const [taskStatusFilter, setTaskStatusFilter] = useState<TaskStatusFilter>('all');
  const [taskPathFilter, setTaskPathFilter] = useState('');
  const [taskFromFilter, setTaskFromFilter] = useState('');
  const [taskToFilter, setTaskToFilter] = useState('');
  const [uploadTotal, setUploadTotal] = useState(0);
  const [uploadPage, setUploadPage] = useState(1);
  const [uploadStatusFilter, setUploadStatusFilter] = useState<UploadStatusFilter>('all');
  const [uploadPathFilter, setUploadPathFilter] = useState('');
  const [selectedUploadBatch, setSelectedUploadBatch] = useState<UploadBatchDetail | null>(null);
  const [uploadProviderModal, setUploadProviderModal] = useState<UploadProvider | null>(null);
  const [newUploadProviderOpen, setNewUploadProviderOpen] = useState(false);
  const [uploadCookieProvider, setUploadCookieProvider] = useState<UploadProvider | null>(null);
  const [uploadCookieValue, setUploadCookieValue] = useState('');
  const [cookieAuth, setCookieAuth] = useState<CookieAuthStatus | null>(null);
  const [savingUploadProvider, setSavingUploadProvider] = useState(false);
  const [checkingUploadProviderID, setCheckingUploadProviderID] = useState<number | null>(null);
  const [uploadTargetActionID, setUploadTargetActionID] = useState<number | null>(null);
  const [watchDirs, setWatchDirs] = useState<WatchDir[]>([]);
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [renamePath, setRenamePath] = useState(() => readRenamePreferences().path ?? '');
  const [renameTemplate, setRenameTemplate] = useState(() => readRenamePreferences().template ?? defaultRenameTemplate);
  const [renameMatchPattern, setRenameMatchPattern] = useState(() => readRenamePreferences().matchPattern ?? '');
  const [renameReleaseGroup, setRenameReleaseGroup] = useState(() => readRenamePreferences().releaseGroup ?? '');
  const [renameLanguage, setRenameLanguage] = useState(() => readRenamePreferences().language ?? 'zh-CN');
  const [renameLanguageInitialized, setRenameLanguageInitialized] = useState(() => Boolean(readRenamePreferences().language));
  const [renameTemplateHistory, setRenameTemplateHistory] = useState(() => asArray<string>(readRenamePreferences().templateHistory).filter(Boolean));
  const [renameTemplateHistoryOpen, setRenameTemplateHistoryOpen] = useState(false);
  const [renamePreview, setRenamePreview] = useState<RenamePreviewItem[]>([]);
  const [renamePreviewCount, setRenamePreviewCount] = useState(0);
  const [renamePreviewTotal, setRenamePreviewTotal] = useState(0);
  const [renameHistory, setRenameHistory] = useState<RenameHistoryBatch[]>([]);
  const [renameHistoryOpen, setRenameHistoryOpen] = useState(false);
  const [selectedHistoryBatch, setSelectedHistoryBatch] = useState<RenameHistoryBatch | null>(null);
  const [undoCheckResult, setUndoCheckResult] = useState<RenameUndoCheckResult | null>(null);
  const [loadingRenameHistory, setLoadingRenameHistory] = useState(false);
  const [undoingHistoryId, setUndoingHistoryId] = useState('');
  const [selectedRenamePaths, setSelectedRenamePaths] = useState<string[]>([]);
  const [tmdbQuery, setTmdbQuery] = useState('');
  const [tmdbResults, setTmdbResults] = useState<TMDBSearchResult[]>([]);
  const [tmdbMatchOpen, setTmdbMatchOpen] = useState(false);
  const [auditTmdbMatchOpen, setAuditTmdbMatchOpen] = useState(false);
  const [tmdbEpisodeDetail, setTmdbEpisodeDetail] = useState<TmdbEpisodeDetail | null>(null);
  const [loadingTmdbEpisodeDetail, setLoadingTmdbEpisodeDetail] = useState(false);
  const [searchingTmdb, setSearchingTmdb] = useState(false);
  const [applyingTmdbShowId, setApplyingTmdbShowId] = useState<number | null>(null);
  const [tmdbApplyProgress, setTmdbApplyProgress] = useState(0);
  const [tmdbApplyTotal, setTmdbApplyTotal] = useState(0);
  const [recalculatingRenamePaths, setRecalculatingRenamePaths] = useState<string[]>([]);
  const [applyingRename, setApplyingRename] = useState(false);
  const [batchEpisodeOpen, setBatchEpisodeOpen] = useState(false);
  const [batchSeason, setBatchSeason] = useState(1);
  const [batchEpisodeMode, setBatchEpisodeMode] = useState<BatchEpisodeMode>('sequence');
  const [batchEpisodeOffset, setBatchEpisodeOffset] = useState(0);
  const [batchEpisodeStart, setBatchEpisodeStart] = useState(1);
  const [applyingBatchEpisode, setApplyingBatchEpisode] = useState(false);
  const [batchEpisodeProgress, setBatchEpisodeProgress] = useState(0);
  const [targetPathEditor, setTargetPathEditor] = useState<{ path: string; value: string } | null>(null);
  const [renameTemplateEditorOpen, setRenameTemplateEditorOpen] = useState(false);
  const [previewingRename, setPreviewingRename] = useState(false);
  const [directoryPicker, setDirectoryPicker] = useState<{ title: string; value: string; rootPath?: string; onSelect: (path: string) => void } | null>(null);
  const [newWatchDir, setNewWatchDir] = useState('');
  const [newWatchDirWatchEnabled, setNewWatchDirWatchEnabled] = useState(true);
  const [newWatchDirUseGlobalProcessing, setNewWatchDirUseGlobalProcessing] = useState(true);
  const [newWatchDirProcessing, setNewWatchDirProcessing] = useState<OutputProcessingConfig>(() => defaultOutputProcessing());
  const [addWatchDirOpen, setAddWatchDirOpen] = useState(false);
  const [editingWatchDir, setEditingWatchDir] = useState<WatchDir | null>(null);
  const [editingWatchDirPath, setEditingWatchDirPath] = useState('');
  const [editingWatchDirWatchEnabled, setEditingWatchDirWatchEnabled] = useState(true);
  const [editingWatchDirUseGlobalProcessing, setEditingWatchDirUseGlobalProcessing] = useState(true);
  const [editingWatchDirProcessing, setEditingWatchDirProcessing] = useState<OutputProcessingConfig>(() => defaultOutputProcessing());
  const [rescanOpen, setRescanOpen] = useState(false);
  const [rescanScope, setRescanScope] = useState<RescanScope>('all');
  const [rescanTarget, setRescanTarget] = useState('');
  const [rescanWatchDirId, setRescanWatchDirId] = useState('');
  const [rescanUseCustomProcessing, setRescanUseCustomProcessing] = useState(false);
  const [rescanProcessing, setRescanProcessing] = useState<OutputProcessingConfig>(() => defaultOutputProcessing());
  const [auditRoot, setAuditRoot] = useState(() => readAuditPreferences().root ?? '');
  const [auditTmdbId, setAuditTmdbId] = useState(() => readAuditPreferences().tmdbId ?? '');
  const [auditIncludeSeasonZero, setAuditIncludeSeasonZero] = useState(() => readAuditPreferences().includeSeasonZero ?? false);
  const [auditEmbyItemUrl, setAuditEmbyItemUrl] = useState(() => readAuditPreferences().embyItemUrl ?? '');
  const [auditEmbyApiKey, setAuditEmbyApiKey] = useState('');
  const [auditEmbyAPIKeys, setAuditEmbyAPIKeys] = useState<EmbyAPIKey[]>([]);
  const [auditTab, setAuditTab] = useState<AuditTab>('missing');
  const [auditSelectedEmbyKeyId, setAuditSelectedEmbyKeyId] = useState(() => readAuditPreferences().embyApiKeyId ?? '');
  const [newEmbyKeyTitle, setNewEmbyKeyTitle] = useState('');
  const [newEmbyKeyValue, setNewEmbyKeyValue] = useState('');
  const [addEmbyKeyOpen, setAddEmbyKeyOpen] = useState(false);
  const [savingEmbyKey, setSavingEmbyKey] = useState(false);
  const [missingAuditReport, setMissingAuditReport] = useState<AuditReport | null>(null);
  const [embyAuditReport, setEmbyAuditReport] = useState<AuditReport | null>(null);
  const [auditingMissing, setAuditingMissing] = useState(false);
  const [auditingEmby, setAuditingEmby] = useState(false);
  const [fileAuditLocalRoot, setFileAuditLocalRoot] = useState(() => readAuditPreferences().fileLocalRoot ?? '');
  const [fileAuditRemoteRoot, setFileAuditRemoteRoot] = useState(() => readAuditPreferences().fileRemoteRoot ?? '');
  const [fileAuditSFTPAddr, setFileAuditSFTPAddr] = useState(() => readAuditPreferences().sftpAddr ?? '');
  const [fileAuditSFTPUser, setFileAuditSFTPUser] = useState(() => readAuditPreferences().sftpUser ?? '');
  const [fileAuditSFTPPassword, setFileAuditSFTPPassword] = useState('');
  const [fileAuditSFTPKeyPath, setFileAuditSFTPKeyPath] = useState(() => readAuditPreferences().sftpKeyPath ?? '');
  const [fileAuditSFTPKnownHostsPath, setFileAuditSFTPKnownHostsPath] = useState(() => readAuditPreferences().sftpKnownHostsPath ?? '');
  const [fileAuditSFTPInsecure, setFileAuditSFTPInsecure] = useState(() => readAuditPreferences().sftpInsecureIgnoreHost ?? false);
  const [fileAuditAllowSTRM, setFileAuditAllowSTRM] = useState(() => readAuditPreferences().allowStrmProxy ?? true);
  const [fileAuditCompareSize, setFileAuditCompareSize] = useState(() => readAuditPreferences().compareSize ?? true);
  const [fileAuditCompareMD5, setFileAuditCompareMD5] = useState(() => readAuditPreferences().compareMd5 ?? false);
  const [fileAuditReport, setFileAuditReport] = useState<FileAuditReport | null>(null);
  const [auditingFiles, setAuditingFiles] = useState(false);
  const [selectedTask, setSelectedTask] = useState<TaskDetail | null>(null);
  const [recentArtifactsOpen, setRecentArtifactsOpen] = useState(false);
  const [selectedTaskIds, setSelectedTaskIds] = useState<number[]>([]);
  const [checkingTools, setCheckingTools] = useState(false);
  const [savingConfig, setSavingConfig] = useState(false);
  const [cancelingTasks, setCancelingTasks] = useState(false);
  const [retryingTasks, setRetryingTasks] = useState(false);
  const [ignoringTasks, setIgnoringTasks] = useState(false);
  const [notice, setNotice] = useState('');
  const [rescanning, setRescanning] = useState(false);
  const [error, setError] = useState<string>('');
  const [confirmation, setConfirmation] = useState<ConfirmationRequest | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [activePage, setActivePage] = useState<PageKey>(() => pageFromPath(window.location.pathname));
  const applyingTmdbShowRef = useRef(false);
  const recalculatingRenamePathsRef = useRef(new Set<string>());
  const renamePreviewAbortRef = useRef<AbortController | null>(null);
  const observedTaskStatusesRef = useRef(new Map<number, string>());
  const observedUploadStatusesRef = useRef(new Map<number, string>());
  const lastRenameSelectionIndexRef = useRef<number | null>(null);
  const lastTaskSelectionIndexRef = useRef<number | null>(null);
  const modalBusyRef = useRef({ applyingBatchEpisode, rescanning, savingUploadProvider, savingEmbyKey });
  modalBusyRef.current = { applyingBatchEpisode, rescanning, savingUploadProvider, savingEmbyKey };
  const displayTimezone = config?.server.timezone || 'Asia/Shanghai';
  const renameBatchConcurrency = previewWorkerCount(config?.renaming?.concurrency ?? 3);
  const renameErrorCount = renamePreview.filter((item) => item.status === 'error' || item.conflict).length;
  const renameWarningCount = renamePreview.filter((item) => item.status === 'warning').length;
  const availableToolCount = tools.filter((tool) => tool.available).length;
  const coreToolsReady = ['ffmpeg', 'ffprobe'].every((name) => tools.some((tool) => tool.name === name && tool.available));
  const enabledWatchDirCount = watchDirs.filter((dir) => dir.enabled).length;
  const activeTaskCount = tasks.filter((task) => task.status === 'pending' || task.status === 'running').length;
  const failedTaskCount = tasks.filter((task) => task.status === 'failed').length;
  const configDirty = Boolean(config && savedConfig && JSON.stringify(config) !== JSON.stringify(savedConfig));
  const currentPageMeta = pageMeta[activePage];
  const modalStackKey = [
    directoryPicker && 'directory', targetPathEditor && 'target-path', selectedHistoryBatch && 'history-detail', renameHistoryOpen && 'history',
    renameTemplateEditorOpen && 'template', tmdbEpisodeDetail && 'episode-detail', addEmbyKeyOpen && 'emby-key', auditTmdbMatchOpen && 'audit-tmdb', tmdbMatchOpen && 'tmdb',
    batchEpisodeOpen && 'batch', editingWatchDir && 'edit-dir', addWatchDirOpen && 'add-dir', rescanOpen && 'rescan', selectedUploadBatch && 'upload-detail',
    uploadCookieProvider && 'upload-cookie', (newUploadProviderOpen || uploadProviderModal) && 'upload-provider', selectedTask && 'task-detail',
    recentArtifactsOpen && 'artifacts'
  ].filter(Boolean).join('|');

  useEffect(() => applyThemeMode(themeMode), [themeMode]);

  useEffect(() => {
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(''), 3600);
    return () => window.clearTimeout(timer);
  }, [notice]);

  useEffect(() => {
    async function load() {
      try {
        const [healthResponse, configResponse, toolsResponse, tasksResponse, dirsResponse, artifactsResponse, desktopRuntime] = await Promise.all([
          fetch('/api/health'),
          fetch('/api/config'),
          fetch('/api/tools/status'),
          fetch(`/api/tasks?page=1&pageSize=${taskPageSize}`),
          fetch('/api/watch-dirs'),
          fetch('/api/artifacts?limit=10'),
          getRuntimeInfo()
        ]);
        const loadedHealth = await healthResponse.json() as Health;
        const loadedConfig = await configResponse.json() as AppConfig;
        setHealth(loadedHealth);
        setConnectionOnline(healthResponse.ok);
        setHealthCheckedAt(new Date());
        setConfig(loadedConfig);
        setSavedConfig(structuredClone(loadedConfig));
        setRuntimeInfo(desktopRuntime);
        setTools(asArray<ToolStatus>(await toolsResponse.json()));
        applyTaskList(await tasksResponse.json());
        setWatchDirs(asArray<WatchDir>(await dirsResponse.json()));
        setArtifacts(asArray<Artifact>(await artifactsResponse.json()));
        await loadRenameHistory();
        await loadEmbyAPIKeys();
      } catch (err) {
        setConnectionOnline(false);
        setError(err instanceof Error ? err.message : '加载失败');
      } finally {
        setInitialLoading(false);
      }
    }

    void load();
  }, [taskPageSize]);

  useEffect(() => {
    let active = true;
    async function refreshHealth() {
      try {
        const response = await fetch('/api/health');
        if (!response.ok) throw new Error(response.statusText);
        const next = await response.json() as Health;
        if (!active) return;
        setHealth(next);
        setConnectionOnline(true);
        setHealthCheckedAt(new Date());
      } catch {
        if (active) {
          setConnectionOnline(false);
          setHealthCheckedAt(new Date());
        }
      }
    }
    const interval = window.setInterval(() => void refreshHealth(), 10000);
    return () => {
      active = false;
      window.clearInterval(interval);
    };
  }, []);

  function requestConfirmation(request: ConfirmationRequest) {
    setConfirmation(request);
  }

  async function acceptConfirmation() {
    if (!confirmation || confirming) return;
    const action = confirmation.onConfirm;
    setConfirming(true);
    try {
      await action();
      setConfirmation(null);
    } finally {
      setConfirming(false);
    }
  }

  useEffect(() => {
    if (!configDirty) return;
    const preventUnload = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener('beforeunload', preventUnload);
    return () => window.removeEventListener('beforeunload', preventUnload);
  }, [configDirty]);

  useEffect(() => {
    function saveShortcut(event: KeyboardEvent) {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's' && activePage === 'settings' && configDirty) {
        event.preventDefault();
        void saveConfig();
      }
    }
    window.addEventListener('keydown', saveShortcut);
    return () => window.removeEventListener('keydown', saveShortcut);
  }, [activePage, configDirty, config]);

  useEffect(() => {
    if (!modalStackKey || confirmation) return;
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';

    const focusTimer = window.setTimeout(() => {
      const dialogs = Array.from(document.querySelectorAll<HTMLElement>('.modal-card')).filter((dialog) => dialog.offsetParent !== null);
      const activeDialog = dialogs[dialogs.length - 1];
      if (!activeDialog) return;
      const initialFocus = activeDialog.querySelector<HTMLElement>('[autofocus]')
        ?? activeDialog.querySelector<HTMLElement>('.icon-close-button:not(:disabled), button:not(:disabled), input:not(:disabled)');
      if (initialFocus) initialFocus.focus();
      else {
        activeDialog.tabIndex = -1;
        activeDialog.focus();
      }
    }, 0);

    function closeTopModal() {
      if (directoryPicker) setDirectoryPicker(null);
      else if (targetPathEditor) setTargetPathEditor(null);
      else if (selectedHistoryBatch) setSelectedHistoryBatch(null);
      else if (renameHistoryOpen) setRenameHistoryOpen(false);
      else if (renameTemplateEditorOpen) setRenameTemplateEditorOpen(false);
      else if (tmdbEpisodeDetail) setTmdbEpisodeDetail(null);
      else if (addEmbyKeyOpen && !modalBusyRef.current.savingEmbyKey) setAddEmbyKeyOpen(false);
      else if (auditTmdbMatchOpen) setAuditTmdbMatchOpen(false);
      else if (tmdbMatchOpen) setTmdbMatchOpen(false);
      else if (batchEpisodeOpen && !modalBusyRef.current.applyingBatchEpisode) setBatchEpisodeOpen(false);
      else if (editingWatchDir) setEditingWatchDir(null);
      else if (addWatchDirOpen) setAddWatchDirOpen(false);
      else if (rescanOpen && !modalBusyRef.current.rescanning) setRescanOpen(false);
      else if (selectedUploadBatch) setSelectedUploadBatch(null);
      else if (uploadCookieProvider && !modalBusyRef.current.savingUploadProvider) { setUploadCookieProvider(null); setCookieAuth(null); setUploadCookieValue(''); }
      else if ((newUploadProviderOpen || uploadProviderModal) && !modalBusyRef.current.savingUploadProvider) { setNewUploadProviderOpen(false); setUploadProviderModal(null); }
      else if (selectedTask) setSelectedTask(null);
      else if (recentArtifactsOpen) setRecentArtifactsOpen(false);
    }

    function handleDialogKey(event: KeyboardEvent) {
      const dialogs = Array.from(document.querySelectorAll<HTMLElement>('.modal-card')).filter((dialog) => dialog.offsetParent !== null);
      const activeDialog = dialogs[dialogs.length - 1];
      if (!activeDialog) return;
      if (event.key === 'Escape') {
        event.preventDefault();
        closeTopModal();
        return;
      }
      if (event.key !== 'Tab') return;
      const focusable = Array.from(activeDialog.querySelectorAll<HTMLElement>('button:not(:disabled), [href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])'));
      if (!focusable.length) {
        event.preventDefault();
        activeDialog.tabIndex = -1;
        activeDialog.focus();
        return;
      }
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (!activeDialog.contains(document.activeElement)) {
        event.preventDefault();
        (event.shiftKey ? last : first).focus();
      } else if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    }

    document.addEventListener('keydown', handleDialogKey);
    return () => {
      window.clearTimeout(focusTimer);
      document.removeEventListener('keydown', handleDialogKey);
      document.body.style.overflow = previousOverflow;
      previousFocus?.focus();
    };
  }, [modalStackKey, confirmation]);

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

  async function loadEmbyAPIKeys() {
    const response = await fetch('/api/emby-api-keys');
    if (!response.ok) return;
    setAuditEmbyAPIKeys(asArray<EmbyAPIKey>(await response.json()));
  }

  useEffect(() => {
    writeAuditPreferences({
      root: auditRoot,
      tmdbId: auditTmdbId,
      includeSeasonZero: auditIncludeSeasonZero,
      embyItemUrl: auditEmbyItemUrl,
      embyApiKeyId: auditSelectedEmbyKeyId,
      fileLocalRoot: fileAuditLocalRoot,
      fileRemoteRoot: fileAuditRemoteRoot,
      sftpAddr: fileAuditSFTPAddr,
      sftpUser: fileAuditSFTPUser,
      sftpKeyPath: fileAuditSFTPKeyPath,
      sftpKnownHostsPath: fileAuditSFTPKnownHostsPath,
      sftpInsecureIgnoreHost: fileAuditSFTPInsecure,
      allowStrmProxy: fileAuditAllowSTRM,
      compareSize: fileAuditCompareSize,
      compareMd5: fileAuditCompareMD5
    });
  }, [auditRoot, auditTmdbId, auditIncludeSeasonZero, auditEmbyItemUrl, auditSelectedEmbyKeyId, fileAuditLocalRoot, fileAuditRemoteRoot, fileAuditSFTPAddr, fileAuditSFTPUser, fileAuditSFTPKeyPath, fileAuditSFTPKnownHostsPath, fileAuditSFTPInsecure, fileAuditAllowSTRM, fileAuditCompareSize, fileAuditCompareMD5]);

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
    if (page === activePage) return;
    if (activePage === 'settings' && page !== 'settings' && configDirty) {
      requestConfirmation({
        title: '放弃设置修改？',
        message: '当前页面包含尚未保存的修改。离开后这些修改将无法恢复。',
        confirmLabel: '放弃并离开',
        tone: 'danger',
        onConfirm: () => {
          discardConfigChanges();
          navigateDirect(page);
        }
      });
      return;
    }
    navigateDirect(page);
  }

  function navigateDirect(page: PageKey) {
    setActivePage(page);
    const path = pagePaths[page];
    if (window.location.pathname !== path) {
      window.history.pushState(null, '', path);
    }
  }

  function applyTaskList(value: TaskListResponse | Task[] | null | undefined) {
    if (Array.isArray(value)) {
      observeTaskStatuses(value);
      setTasks(value);
      setTaskTotal(value.length);
      setTaskPage(1);
      setSelectedTaskIds((ids) => ids.filter((id) => value.some((task) => task.id === id)));
      return;
    }
    const items = asArray<Task>(value?.items);
    observeTaskStatuses(items);
    setTasks(items);
    setTaskTotal(value?.total ?? 0);
    setTaskPage(value?.page ?? 1);
    setSelectedTaskIds((ids) => ids.filter((id) => items.some((task) => task.id === id)));
  }

  async function loadTasks(page = taskPage, status = taskStatusFilter) {
    const params = new URLSearchParams({ page: String(page), pageSize: String(taskPageSize) });
    if (taskPathFilter.trim()) params.set('path', taskPathFilter.trim());
    if (status !== 'all') params.set('status', status);
    if (taskFromFilter) params.set('from', zonedInputToUTC(taskFromFilter, displayTimezone, false));
    if (taskToFilter) params.set('to', zonedInputToUTC(taskToFilter, displayTimezone, true));
    const response = await fetch(`/api/tasks?${params.toString()}`);
    if (!response.ok) {
      setError(await response.text());
      return;
    }
    applyTaskList(await response.json());
  }

  function applyUploadBatchList(value: UploadBatchListResponse) {
    const items = asArray<UploadBatch>(value.items);
    observeUploadStatuses(items);
    setUploadBatches(items);
    setUploadTotal(value.total ?? 0);
    setUploadPage(value.page ?? 1);
  }

  function observeTaskStatuses(items: Task[]) {
    const observed = observedTaskStatusesRef.current;
    for (const task of items) {
      const previous = observed.get(task.id);
      observed.set(task.id, task.status);
      if (!previous || previous === task.status || !['completed', 'failed'].includes(task.status)) continue;
      const name = task.mediaPath.split(/[\\/]/).pop() || task.mediaPath || `任务 #${task.id}`;
      const failed = task.status === 'failed';
      void notifyDesktop(failed ? '媒体任务失败' : '媒体任务完成', failed ? `${name}：${task.errorSummary || '请打开任务详情查看原因'}` : name).catch(() => {});
    }
  }

  function observeUploadStatuses(items: UploadBatch[]) {
    const observed = observedUploadStatusesRef.current;
    for (const batch of items) {
      const previous = observed.get(batch.id);
      observed.set(batch.id, batch.status);
      if (!previous || previous === batch.status || !['completed', 'partial', 'failed'].includes(batch.status)) continue;
      const failed = batch.status === 'failed' || batch.status === 'partial';
      const name = batch.seriesPath.split(/[\\/]/).pop() || batch.seriesKey || `上传批次 #${batch.id}`;
      void notifyDesktop(failed ? '上传批次需要处理' : '上传批次完成', failed ? `${name}：${batch.failedTargets} 个目标失败` : name).catch(() => {});
    }
  }

  async function loadUploadProviders() {
    const response = await fetch('/api/upload/providers');
    if (!response.ok) throw new Error(await response.text());
    setUploadProviders(asArray<UploadProvider>(await response.json()));
  }

  async function loadUploadProviderTypes() {
    const response = await fetch('/api/upload/provider-types');
    if (!response.ok) throw new Error(await response.text());
    setUploadProviderTypes(asArray<UploadProviderDescriptor>(await response.json()));
  }

  async function loadUploadSummary() {
    const response = await fetch('/api/uploads/summary');
    if (!response.ok) throw new Error(await response.text());
    setUploadSummary(await response.json() as UploadSummary);
  }

  async function loadUploadBatches(page = uploadPage, status = uploadStatusFilter) {
    const params = new URLSearchParams({ page: String(page), pageSize: String(taskPageSize) });
    if (status !== 'all') params.set('status', status);
    if (uploadPathFilter.trim()) params.set('path', uploadPathFilter.trim());
    const response = await fetch(`/api/uploads?${params.toString()}`);
    if (!response.ok) throw new Error(await response.text());
    applyUploadBatchList(await response.json() as UploadBatchListResponse);
  }

  async function refreshUploads(page = uploadPage, status = uploadStatusFilter) {
    try {
      await Promise.all([loadUploadSummary(), loadUploadProviders(), loadUploadProviderTypes(), loadUploadBatches(page, status)]);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载上传管理失败');
    }
  }

  async function browseDirectory(options: { title: string; value: string; rootPath?: string; onSelect: (path: string) => void }) {
    setError('');
    try {
      const result = await pickDesktopDirectory({ title: options.title, initialPath: options.value, allowedRoot: options.rootPath });
      if (result.handled) {
        if (result.path) options.onSelect(result.path);
        return;
      }
      setDirectoryPicker(options);
    } catch (err) {
      setError(err instanceof Error ? err.message : '选择目录失败');
    }
  }

  async function browseFile(options: { title: string; value: string; displayName?: string; pattern?: string; onSelect: (path: string) => void }) {
    setError('');
    try {
      const result = await pickDesktopFile({ title: options.title, initialPath: options.value, displayName: options.displayName, pattern: options.pattern });
      if (!result.handled) {
        setNotice('浏览器管理模式不支持系统文件选择，请直接填写路径。');
        return;
      }
      if (result.path) options.onSelect(result.path);
    } catch (err) {
      setError(err instanceof Error ? err.message : '选择文件失败');
    }
  }

  async function revealPath(path: string) {
    if (!path) return;
    try {
      const handled = await revealDesktopPath(path);
      if (!handled) setNotice('该操作仅在桌面应用中可用。');
    } catch (err) {
      setError(err instanceof Error ? err.message : '无法在文件管理器中定位该路径');
    }
  }

  async function loadUploadBatchDetail(batchID: number) {
    try {
      const response = await fetch(`/api/uploads/${batchID}`);
      if (!response.ok) throw new Error(await response.text());
      setSelectedUploadBatch(await response.json() as UploadBatchDetail);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载上传批次详情失败');
    }
  }

  async function saveUploadProvider(provider: UploadProvider) {
    setSavingUploadProvider(true);
    setError('');
    try {
      const isNew = provider.id === 0;
      const response = await fetch(isNew ? '/api/upload/providers' : `/api/upload/providers/${provider.id}`, {
        method: isNew ? 'POST' : 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(provider)
      });
      if (!response.ok) throw new Error(await response.text());
      setUploadProviderModal(null);
      setNewUploadProviderOpen(false);
      setNotice(isNew ? '上传目标已添加。请配置 Cookie 后启用自动上传。' : '上传目标已保存。');
      await refreshUploads();
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存上传目标失败');
    } finally {
      setSavingUploadProvider(false);
    }
  }

  function deleteUploadProvider(provider: UploadProvider) {
    requestConfirmation({
      title: `删除“${provider.name}”？`,
      message: '该上传目标将停止接收新批次。已有上传历史会阻止删除，通常更建议先禁用目标。',
      confirmLabel: '删除上传目标',
      tone: 'danger',
      onConfirm: () => performDeleteUploadProvider(provider)
    });
  }

  async function performDeleteUploadProvider(provider: UploadProvider) {
    try {
      const response = await fetch(`/api/upload/providers/${provider.id}`, { method: 'DELETE' });
      if (!response.ok) throw new Error(await response.text());
      setNotice('上传目标已删除。');
      await refreshUploads();
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除上传目标失败');
    }
  }

  async function saveUploadCookie() {
    if (!uploadCookieProvider || !uploadCookieValue.trim()) return;
    setSavingUploadProvider(true);
    try {
      const response = await fetch(`/api/upload/providers/${uploadCookieProvider.id}/cookie`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ cookie: uploadCookieValue.trim() })
      });
      if (!response.ok) throw new Error(await response.text());
      setUploadCookieValue('');
      setCookieAuth(null);
      setNotice('115 Cookie 已保存。');
      await loadUploadProviders();
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存 115 Cookie 失败');
    } finally {
      setSavingUploadProvider(false);
    }
  }

  async function startCookieAuth(terminal = 'tv') {
    if (!uploadCookieProvider) return;
    setSavingUploadProvider(true);
    try {
      const response = await fetch(`/api/upload/providers/${uploadCookieProvider.id}/auth/115cookie`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ terminal })
      });
      if (!response.ok) throw new Error(await response.text());
      setCookieAuth(await response.json() as CookieAuthStatus);
    } catch (err) {
      setError(err instanceof Error ? err.message : '启动二维码授权失败');
    } finally {
      setSavingUploadProvider(false);
    }
  }

  async function checkUploadProvider(provider: UploadProvider) {
    setCheckingUploadProviderID(provider.id);
    try {
      const response = await fetch(`/api/upload/providers/${provider.id}/check`, { method: 'POST' });
      if (!response.ok) throw new Error(await response.text());
      setNotice(`“${provider.name}”连接正常。`);
    } catch (err) {
      setError(err instanceof Error ? err.message : '检查上传目标失败');
    } finally {
      setCheckingUploadProviderID(null);
    }
  }

  function actOnUploadTarget(target: UploadBatchTarget, action: 'retry' | 'cancel') {
    if (action === 'cancel') {
      requestConfirmation({
        title: `取消向“${target.providerName}”上传？`,
        message: '尚未传输的文件将停止；已经上传到远端的文件不会被删除。之后可以手动重新排队。',
        confirmLabel: '取消上传',
        tone: 'danger',
        onConfirm: () => performUploadTargetAction(target, action)
      });
      return;
    }
    void performUploadTargetAction(target, action);
  }

  async function performUploadTargetAction(target: UploadBatchTarget, action: 'retry' | 'cancel') {
    setUploadTargetActionID(target.id);
    try {
      const response = await fetch(`/api/uploads/targets/${target.id}/${action}`, { method: 'POST' });
      if (!response.ok) throw new Error(await response.text());
      setNotice(action === 'retry' ? '上传目标已重新排队。' : '上传目标已取消。');
      if (selectedUploadBatch) await loadUploadBatchDetail(selectedUploadBatch.batch.id);
      await refreshUploads();
    } catch (err) {
      setError(err instanceof Error ? err.message : '更新上传目标失败');
    } finally {
      setUploadTargetActionID(null);
    }
  }

  useEffect(() => {
    if (activePage !== 'tasks') return;
    const interval = window.setInterval(() => {
      void loadTasks(taskPage, taskStatusFilter);
    }, taskListRefreshIntervalMs);
    return () => window.clearInterval(interval);
  }, [activePage, taskPage, taskStatusFilter, taskPageSize, taskPathFilter, taskFromFilter, taskToFilter, displayTimezone]);

  useEffect(() => {
    if (activePage !== 'uploads') return;
    void refreshUploads(uploadPage, uploadStatusFilter);
    const interval = window.setInterval(() => {
      void refreshUploads(uploadPage, uploadStatusFilter);
    }, uploadListRefreshIntervalMs);
    return () => window.clearInterval(interval);
  }, [activePage, uploadPage, uploadStatusFilter, uploadPathFilter, taskPageSize]);

  useEffect(() => {
    if (!runtimeInfo?.desktop) return;
    let active = true;
    async function pollDesktopNotifications() {
      try {
        const [tasksResponse, uploadsResponse] = await Promise.all([
          fetch('/api/tasks?page=1&pageSize=50'),
          fetch('/api/uploads?page=1&pageSize=50')
        ]);
        if (!active) return;
        if (tasksResponse.ok) {
          const value = await tasksResponse.json() as TaskListResponse;
          observeTaskStatuses(asArray<Task>(value.items));
        }
        if (uploadsResponse.ok) {
          const value = await uploadsResponse.json() as UploadBatchListResponse;
          observeUploadStatuses(asArray<UploadBatch>(value.items));
        }
      } catch {
        // Notification polling must not replace the main connection status.
      }
    }
    void pollDesktopNotifications();
    const interval = window.setInterval(() => void pollDesktopNotifications(), desktopNotificationPollIntervalMs);
    return () => {
      active = false;
      window.clearInterval(interval);
    };
  }, [runtimeInfo?.desktop]);

  useEffect(() => {
    if (!selectedUploadBatch) return;
    const batchID = selectedUploadBatch.batch.id;
    const interval = window.setInterval(() => void loadUploadBatchDetail(batchID), uploadListRefreshIntervalMs);
    return () => window.clearInterval(interval);
  }, [selectedUploadBatch?.batch.id]);

  useEffect(() => {
    if (!cookieAuth || !uploadCookieProvider || ['authorized', 'expired', 'cancelled', 'error'].includes(cookieAuth.state)) return;
    const providerID = uploadCookieProvider.id;
    const sessionID = cookieAuth.sessionId;
    const interval = window.setInterval(async () => {
      try {
        const response = await fetch(`/api/upload/providers/${providerID}/auth/115cookie?sessionId=${encodeURIComponent(sessionID)}`);
        if (!response.ok) throw new Error(await response.text());
        const next = await response.json() as CookieAuthStatus;
        setCookieAuth(next);
        if (next.state === 'authorized') {
          setNotice('115 Cookie 授权成功。');
          await loadUploadProviders();
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : '查询二维码授权状态失败');
      }
    }, 2000);
    return () => window.clearInterval(interval);
  }, [cookieAuth?.sessionId, cookieAuth?.state, uploadCookieProvider?.id]);

  useEffect(() => {
    if (!selectedTask) return;
    const taskId = selectedTask.task.id;
    let active = true;
    const interval = window.setInterval(async () => {
      try {
        const detail = await fetchTaskDetail(taskId);
        if (active) {
          setSelectedTask((current) => current?.task.id === taskId ? detail : current);
        }
      } catch {
        // Keep the current dialog content if a background refresh fails.
      }
    }, taskDetailRefreshIntervalMs);
    return () => {
      active = false;
      window.clearInterval(interval);
    };
  }, [selectedTask?.task.id]);

  function resetTaskFilters() {
    setTaskStatusFilter('all');
    setTaskPathFilter('');
    setTaskFromFilter('');
    setTaskToFilter('');
    void loadTasksWithoutFilters();
  }

  function selectTaskStatusFilter(status: TaskStatusFilter) {
    setTaskStatusFilter(status);
    void loadTasks(1, status);
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

  async function previewRename(bypassTmdbCache = false) {
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
    setRenamePreviewTotal(0);
    setSelectedRenamePaths([]);
    const controller = new AbortController();
    renamePreviewAbortRef.current = controller;
    try {
      const input = {
        path: renamePath.trim(),
        template: renameTemplate,
        matchPattern: renameMatchPattern,
        bypassTmdbCache,
        language: renameLanguage,
        releaseGroup: renameReleaseGroup.trim()
      };
      const handledByDesktop = await previewDesktopRename(input, (event) => applyRenamePreviewMessage({
        type: event.type,
        item: event.item as RenamePreviewItem | undefined,
        count: event.count,
        total: event.total,
        error: event.error
      }), controller.signal);
      if (handledByDesktop) return;

      const response = await fetch('/api/rename/preview/stream', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
        signal: controller.signal
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
      if (err instanceof Error && err.name === 'AbortError') {
        setError('');
        setNotice('已取消重命名预览。');
      } else {
        setError(err instanceof Error ? err.message : '生成预览失败');
      }
    } finally {
      if (renamePreviewAbortRef.current === controller) renamePreviewAbortRef.current = null;
      setPreviewingRename(false);
    }
  }

  function cancelRenamePreview() {
    renamePreviewAbortRef.current?.abort();
  }

  function rememberRenamePreferences() {
    const template = renameTemplate.trim() || defaultRenameTemplate;
    const nextHistory = [template, ...renameTemplateHistory.filter((item) => item !== template)].slice(0, renameTemplateHistoryLimit);
    setRenameTemplateHistory(nextHistory);
    writeRenamePreferences({
      path: renamePath.trim(),
      template,
      matchPattern: renameMatchPattern.trim(),
      language: renameLanguage,
      releaseGroup: renameReleaseGroup.trim(),
      templateHistory: nextHistory
    });
  }

  function deleteRenameTemplateHistory(template: string) {
    const nextHistory = renameTemplateHistory.filter((item) => item !== template);
    setRenameTemplateHistory(nextHistory);
    writeRenamePreferences({ ...readRenamePreferences(), templateHistory: nextHistory });
    if (!nextHistory.length) setRenameTemplateHistoryOpen(false);
  }

  function handleRenamePreviewMessage(line: string) {
    if (!line.trim()) return;
    applyRenamePreviewMessage(JSON.parse(line) as RenamePreviewStreamMessage);
  }

  function applyRenamePreviewMessage(message: RenamePreviewStreamMessage) {
    if (message.type === 'start') {
      setRenamePreviewTotal(message.total ?? 0);
    } else if (message.type === 'item' && message.item) {
      setRenamePreview((items) => [...items, message.item as RenamePreviewItem]);
      setRenamePreviewCount(message.count ?? 0);
      setRenamePreviewTotal(message.total ?? 0);
    } else if (message.type === 'error') {
      setError(message.error || '生成预览失败');
    } else if (message.type === 'done') {
      setRenamePreviewCount(message.count ?? 0);
      setRenamePreviewTotal(message.total ?? message.count ?? 0);
      setNotice(`预览生成完成，共 ${message.count ?? 0} 个文件。`);
    }
  }

  async function openTmdbEpisodeDetail(item: RenamePreviewItem, refresh = false) {
    setLoadingTmdbEpisodeDetail(true);
    setError('');
    try {
      const params = new URLSearchParams({ showId: String(item.tmdbShowId), season: String(item.season), episode: String(item.episode), language: renameLanguage, refresh: String(refresh) });
      const response = await fetch(`/api/tmdb/episode?${params.toString()}`);
      if (!response.ok) {
        setError(await response.text());
        return;
      }
      setTmdbEpisodeDetail(await response.json() as TmdbEpisodeDetail);
    } catch (err) {
      setError(err instanceof Error ? err.message : '获取 TMDB 详情失败');
    } finally {
      setLoadingTmdbEpisodeDetail(false);
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
        matchPattern: renameMatchPattern,
        bypassTmdbCache: options.forceTmdb ?? false,
        language: renameLanguage,
        show: options.show ?? item.show,
        title: item.title,
        releaseGroup: renameReleaseGroup.trim(),
        season: item.season,
        episode: item.episode,
        tmdbShowId: options.tmdbShowId ?? item.tmdbShowId,
        newName: (options.keepManualName ?? item.manualName) ? item.newName : ''
      })
    });
    if (!response.ok) {
      const message = await response.text();
      setError(message);
      throw new Error(message);
    }
    return await response.json() as RenamePreviewItem;
  }

  async function recalculateRenameItem(item: RenamePreviewItem, options: { tmdbShowId?: number; show?: string; forceTmdb?: boolean; keepManualName?: boolean } = {}) {
    if (recalculatingRenamePathsRef.current.has(item.path)) return;
    recalculatingRenamePathsRef.current.add(item.path);
    setRecalculatingRenamePaths(Array.from(recalculatingRenamePathsRef.current));
    try {
      const next = await previewAdjustedRenameItem(item, options);
      if (next) replaceRenameItem(next);
    } catch (err) {
      updateRenameItem(item.path, { status: 'error', message: err instanceof Error ? err.message : '重新预览失败' });
    } finally {
      recalculatingRenamePathsRef.current.delete(item.path);
      setRecalculatingRenamePaths(Array.from(recalculatingRenamePathsRef.current));
    }
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

  function openAuditTmdbMatch() {
    const pathParts = auditRoot.trim().split(/[\\/]/).filter(Boolean);
    setTmdbQuery(missingAuditReport?.showTitle || pathParts[pathParts.length - 1] || '');
    setTmdbResults([]);
    setAuditTmdbMatchOpen(true);
  }

  function applyTmdbShowToAudit(show: TMDBSearchResult) {
    setAuditTmdbId(String(show.id));
    setAuditTmdbMatchOpen(false);
    setNotice(`已选择 ${show.name || show.originalName}（TMDB #${show.id}），请开始核对。`);
  }

  async function applyTmdbShowToSelected(show: TMDBSearchResult) {
    if (applyingTmdbShowRef.current) return;
    const targets = renamePreview.filter((item) => selectedRenamePaths.includes(item.path));
    if (!targets.length) {
      setError('请先勾选要套用的文件');
      return;
    }
    applyingTmdbShowRef.current = true;
    setApplyingTmdbShowId(show.id);
    setTmdbApplyProgress(0);
    setTmdbApplyTotal(targets.length);
    setError('');
    let completed = 0;
    try {
      await runWithConcurrency(targets, renameBatchConcurrency, async (item) => {
        try {
          await recalculateRenameItem({ ...item, manualName: false }, { tmdbShowId: show.id, show: show.name || show.originalName, forceTmdb: true, keepManualName: false });
        } finally {
          completed++;
          setTmdbApplyProgress(completed);
        }
      });
      setTmdbMatchOpen(false);
    } finally {
      applyingTmdbShowRef.current = false;
      setApplyingTmdbShowId(null);
      setTmdbApplyProgress(0);
      setTmdbApplyTotal(0);
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
    setBatchEpisodeProgress(0);
    setError('');
    let completed = 0;
    try {
      await runWithConcurrency(targets, renameBatchConcurrency, async (item, index) => {
        const episode = batchEpisodeMode === 'sequence'
          ? batchEpisodeStart + index
          : batchEpisodeMode === 'offset'
            ? item.episode + batchEpisodeOffset
            : item.episode;
        const adjusted = { ...item, season: batchSeason, episode: Math.max(0, episode), manualName: false };
        try {
          const next = await previewAdjustedRenameItem(adjusted, { forceTmdb: true, keepManualName: false });
          if (next) replaceRenameItem(next);
        } catch (err) {
          updateRenameItem(item.path, { status: 'error', message: err instanceof Error ? err.message : '重新预览失败' });
        } finally {
          completed++;
          setBatchEpisodeProgress(completed);
        }
      });
      setBatchEpisodeOpen(false);
      setNotice(`已批量修正 ${targets.length} 个文件的季集并重新预览。`);
    } finally {
      setApplyingBatchEpisode(false);
      setBatchEpisodeProgress(0);
    }
  }

  function applySelectedRenames() {
    const targets = renamePreview.filter((item) => selectedRenamePaths.includes(item.path));
    if (!targets.length) {
      setError('请先勾选要重命名的文件');
      return;
    }
    requestConfirmation({
      title: `重命名 ${targets.length} 个文件？`,
      message: '应用会同时移动匹配的字幕、NFO、图片等伴生文件，并拒绝覆盖已经存在的目标。操作完成后可以从历史记录撤销。',
      confirmLabel: '应用重命名',
      onConfirm: () => performSelectedRenames(targets)
    });
  }

  async function performSelectedRenames(targets: RenamePreviewItem[]) {
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
      setError('该批次存在不可撤销项，已停止撤销。已打开详情供检查。');
      setSelectedHistoryBatch(check.batch);
      return;
    }
    requestConfirmation({
      title: `撤销 ${check.items.length} 个文件移动？`,
      message: '文件将恢复到该批次执行前的位置。若路径已被其他文件占用，撤销会停止并保留当前状态。',
      confirmLabel: '撤销该批次',
      tone: 'danger',
      onConfirm: () => performUndoRenameBatch(id)
    });
  }

  async function performUndoRenameBatch(id: string) {
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

  async function addWatchDir() {
    if (!newWatchDir.trim()) return;
    setError('');
    const response = await fetch('/api/watch-dirs', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: newWatchDir.trim(), recursive: true, watchEnabled: newWatchDirWatchEnabled, scanOnStart: false, useGlobalProcessing: newWatchDirUseGlobalProcessing, processing: newWatchDirProcessing })
    });
    if (!response.ok) {
      setError(await response.text());
      return;
    }
    const created = await response.json();
    setWatchDirs((items) => [...items, created]);
    setNewWatchDir('');
    setNewWatchDirWatchEnabled(true);
    setNewWatchDirUseGlobalProcessing(true);
    setNewWatchDirProcessing(outputProcessingFromConfig(config));
    setAddWatchDirOpen(false);
    setNotice('媒体目录已添加，自动监听配置已热更新。');
  }

  function openEditWatchDir(dir: WatchDir) {
    setEditingWatchDir(dir);
    setEditingWatchDirPath(dir.path);
    setEditingWatchDirWatchEnabled(dir.watchEnabled);
    setEditingWatchDirUseGlobalProcessing(dir.useGlobalProcessing);
    setEditingWatchDirProcessing(dir.processing?.strategy ? dir.processing : outputProcessingFromConfig(config));
  }

  async function submitEditWatchDir() {
    if (!editingWatchDir || !editingWatchDirPath.trim()) return;
    const updated = await updateWatchDir(editingWatchDir, {
      path: editingWatchDirPath.trim(),
      watchEnabled: editingWatchDirWatchEnabled,
      scanOnStart: false,
      useGlobalProcessing: editingWatchDirUseGlobalProcessing,
      processing: editingWatchDirProcessing
    });
    if (!updated) return;
    setEditingWatchDir(null);
    setEditingWatchDirPath('');
  }

  async function updateWatchDir(dir: WatchDir, patch: Partial<WatchDir>) {
    setError('');
    const next = { ...dir, ...patch, scanOnStart: false };
    const response = await fetch(`/api/watch-dirs/${dir.id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(next)
    });
    if (!response.ok) {
      setError(await response.text());
      return false;
    }
    const updated = await response.json();
    setWatchDirs((items) => items.map((item) => item.id === dir.id ? updated : item));
    setNotice('目录配置已更新，自动监听配置已热更新。');
    return true;
  }

  function deleteWatchDir(id: number) {
    const directory = watchDirs.find((item) => item.id === id);
    requestConfirmation({
      title: '移除媒体目录？',
      message: `${directory?.path ?? '该目录'} 将不再被自动监听。已有任务、产物和媒体文件不会被删除。`,
      confirmLabel: '移除目录',
      tone: 'danger',
      onConfirm: () => performDeleteWatchDir(id)
    });
  }

  async function performDeleteWatchDir(id: number) {
    setError('');
    const response = await fetch(`/api/watch-dirs/${id}`, { method: 'DELETE' });
    if (!response.ok) {
      setError(await response.text());
      return;
    }
    setWatchDirs((items) => items.filter((item) => item.id !== id));
    setNotice('媒体目录已移除，磁盘上的文件未被修改。');
  }

  async function rescan() {
    setRescanning(true);
    setError('');
    try {
      const payload: Record<string, unknown> = {
        useCustomProcessing: rescanUseCustomProcessing,
        processing: rescanProcessing
      };
      if (rescanScope === 'dir') {
        const selected = watchDirs.find((dir) => String(dir.id) === rescanWatchDirId);
        if (!selected) {
          setError('请选择媒体目录');
          return;
        }
        payload.watchDirId = selected.id;
        if (rescanTarget.trim()) payload.path = rescanTarget.trim();
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
        setError(await readErrorMessage(response));
        return;
      }
      setNotice('补扫已加入队列。');
      await loadTasks(1);
    } catch (err) {
      setError(err instanceof Error ? err.message : '补扫失败');
    } finally {
      setRescanning(false);
    }
  }

  function openRescanDialog(scope: RescanScope, target = '') {
    setRescanScope(scope);
    const selected = scope === 'dir' ? watchDirs.find((dir) => dir.path === target) : undefined;
    setRescanWatchDirId(selected ? String(selected.id) : '');
    setRescanTarget('');
    setRescanUseCustomProcessing(false);
    setRescanProcessing(outputProcessingFromConfig(config));
    setRescanOpen(true);
  }

  async function runSeriesAudit(mode: 'missing' | 'emby') {
    if (!auditRoot.trim()) {
      setError('请输入要核对的剧集根目录');
      return;
    }
    if (mode === 'emby' && !auditEmbyItemUrl.trim()) {
      setError('请输入 Emby 剧集页面 URL');
      return;
    }
    if (mode === 'emby' && !auditEmbyApiKey.trim() && !auditSelectedEmbyKeyId) {
      setError('请选择或输入 Emby API Key');
      return;
    }
    const setAuditing = mode === 'missing' ? setAuditingMissing : setAuditingEmby;
    setAuditing(true);
    setError('');
    setNotice('');
    try {
      const response = await fetch(mode === 'missing' ? '/api/audit/missing' : '/api/audit/emby', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          root: auditRoot.trim(),
          ...(mode === 'missing' ? {
            tmdbShowId: Number(auditTmdbId) || 0,
            includeSeasonZero: auditIncludeSeasonZero,
          } : {
            embyItemUrl: auditEmbyItemUrl.trim(),
            embyApiKey: auditEmbyApiKey.trim(),
            embyApiKeyId: Number(auditSelectedEmbyKeyId) || 0,
          }),
        })
      });
      if (!response.ok) {
        setError(await response.text());
        return;
      }
      const report = await response.json() as AuditReport;
      if (mode === 'missing') {
        setMissingAuditReport(report);
        const missingCount = report.seasonReports.reduce((sum, season) => sum + (season.missingEpisodes?.length ?? 0), 0);
        const artifactCount = groupArtifactIssues(report.artifactIssues).length;
        setNotice(`剧集缺漏核对完成：缺失 ${missingCount} 集，发现 ${artifactCount} 个产物异常文件或目录。`);
        if (!report.tmdbShowId) {
          const pathParts = auditRoot.trim().split(/[\\/]/).filter(Boolean);
          setTmdbQuery(report.showTitle || pathParts[pathParts.length - 1] || '');
          setTmdbResults([]);
          setAuditTmdbMatchOpen(true);
        }
      } else {
        setEmbyAuditReport(report);
        setNotice(`Emby 与本地核对完成：发现 ${report.embyComparisons?.length ?? 0} 项差异。`);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '剧集核对失败');
    } finally {
      setAuditing(false);
    }
  }

  async function runFileAudit() {
    if (!fileAuditLocalRoot.trim() || !fileAuditRemoteRoot.trim()) {
      setError('请输入本地目录和远端目录');
      return;
    }
    if (!fileAuditSFTPAddr.trim() || !fileAuditSFTPUser.trim()) {
      setError('请输入 SFTP 地址和用户名');
      return;
    }
    setAuditingFiles(true);
    setError('');
    setNotice('');
    try {
      const response = await fetch('/api/audit/files', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          localRoot: fileAuditLocalRoot.trim(),
          remoteRoot: fileAuditRemoteRoot.trim(),
          sftpAddr: fileAuditSFTPAddr.trim(),
          sftpUser: fileAuditSFTPUser.trim(),
          sftpPassword: fileAuditSFTPPassword,
          sftpKeyPath: fileAuditSFTPKeyPath.trim(),
          sftpKnownHostsPath: fileAuditSFTPKnownHostsPath.trim(),
          sftpInsecureIgnoreHost: fileAuditSFTPInsecure,
          allowStrmProxy: fileAuditAllowSTRM,
          compareSize: fileAuditCompareSize,
          compareMd5: fileAuditCompareMD5
        })
      });
      if (!response.ok) {
        setError(await response.text());
        return;
      }
      const report = await response.json() as FileAuditReport;
      setFileAuditReport(report);
      setNotice(`文件对齐检查完成：本地 ${report.localCount} 个，远端 ${report.remoteCount} 个，差异 ${report.issues?.length ?? 0} 项。`);
    } catch (err) {
      setError(err instanceof Error ? err.message : '文件对齐检查失败');
    } finally {
      setAuditingFiles(false);
    }
  }

  async function saveEmbyAPIKey() {
    if (!newEmbyKeyTitle.trim() || !newEmbyKeyValue.trim()) {
      setError('请输入 Emby API Key 标题和 Key');
      return;
    }
    setSavingEmbyKey(true);
    setError('');
    try {
      const response = await fetch('/api/emby-api-keys', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title: newEmbyKeyTitle.trim(), apiKey: newEmbyKeyValue.trim() })
      });
      if (!response.ok) {
        setError(await response.text());
        return;
      }
      const saved = await response.json() as EmbyAPIKey;
      setNewEmbyKeyTitle('');
      setNewEmbyKeyValue('');
      await loadEmbyAPIKeys();
      setAuditSelectedEmbyKeyId(String(saved.id));
      setAuditEmbyApiKey('');
      setAddEmbyKeyOpen(false);
      setNotice('Emby API Key 已保存。');
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存 Emby API Key 失败');
    } finally {
      setSavingEmbyKey(false);
    }
  }

  function deleteEmbyAPIKey(id: number) {
    const key = auditEmbyAPIKeys.find((item) => item.id === id);
    requestConfirmation({
      title: `删除 API Key“${key?.title ?? ''}”？`,
      message: '删除后无法恢复。该操作不会影响 Emby 服务器上的其他 API Key。',
      confirmLabel: '删除 API Key',
      tone: 'danger',
      onConfirm: () => performDeleteEmbyAPIKey(id)
    });
  }

  async function performDeleteEmbyAPIKey(id: number) {
    setError('');
    const response = await fetch(`/api/emby-api-keys/${id}`, { method: 'DELETE' });
    if (!response.ok) {
      setError(await response.text());
      return;
    }
    setAuditEmbyAPIKeys((keys) => keys.filter((key) => key.id !== id));
    if (auditSelectedEmbyKeyId === String(id)) {
      setAuditSelectedEmbyKeyId('');
    }
    setNotice('Emby API Key 已删除。');
  }

  async function fetchTaskDetail(id: number) {
    const response = await fetch(`/api/tasks/${id}`);
    if (!response.ok) {
      throw new Error(await response.text());
    }
    return normalizeTaskDetail(await response.json());
  }

  async function loadTaskDetail(id: number) {
    setError('');
    try {
      setSelectedTask(await fetchTaskDetail(id));
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载任务详情失败');
    }
  }

  function toggleTaskSelection(id: number, checked: boolean, shiftKey = false) {
    const index = tasks.findIndex((task) => task.id === id);
    setSelectedTaskIds((ids) => {
      if (shiftKey && lastTaskSelectionIndexRef.current !== null && index >= 0) {
        const start = lastTaskSelectionIndexRef.current;
        if (start >= 0) {
          const [from, to] = start < index ? [start, index] : [index, start];
          const range = tasks.slice(from, to + 1).map((task) => task.id);
          return checked ? [...new Set([...ids, ...range])] : ids.filter((item) => !range.includes(item));
        }
      }
      return checked ? [...new Set([...ids, id])] : ids.filter((item) => item !== id);
    });
    if (index >= 0) lastTaskSelectionIndexRef.current = index;
  }

  function handleTaskRowClick(event: ReactMouseEvent<HTMLTableRowElement>, task: Task, index: number) {
    const target = event.target as HTMLElement;
    if (target.closest('input, button, select, textarea, a')) return;
    const selected = selectedTaskIds.includes(task.id);
    if (event.shiftKey && lastTaskSelectionIndexRef.current !== null) {
      const [from, to] = lastTaskSelectionIndexRef.current < index ? [lastTaskSelectionIndexRef.current, index] : [index, lastTaskSelectionIndexRef.current];
      const range = tasks.slice(from, to + 1).map((entry) => entry.id);
      setSelectedTaskIds((ids) => selected ? ids.filter((id) => !range.includes(id)) : [...new Set([...ids, ...range])]);
      return;
    }
    setSelectedTaskIds((ids) => selected ? ids.filter((id) => id !== task.id) : [...new Set([...ids, task.id])]);
    lastTaskSelectionIndexRef.current = index;
  }

  async function retrySelectedTasks() {
    if (!selectedTaskIds.length) {
      setError('请先勾选要重试的任务');
      return;
    }
    setRetryingTasks(true);
    setError('');
    try {
      const response = await fetch('/api/tasks/retry', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ids: selectedTaskIds })
      });
      if (!response.ok) {
        setError(await response.text());
        return;
      }
      const result = await response.json();
      setNotice(`已重新排队 ${result.count ?? 0} 个任务。`);
      setSelectedTaskIds([]);
      await loadTasks(taskPage);
    } catch (err) {
      setError(err instanceof Error ? err.message : '重试任务失败');
    } finally {
      setRetryingTasks(false);
    }
  }

  async function ignoreSelectedTasks() {
    if (!selectedTaskIds.length) {
      setError('请先勾选要忽略的失败任务');
      return;
    }
    setIgnoringTasks(true);
    setError('');
    try {
      const response = await fetch('/api/tasks/ignore', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ids: selectedTaskIds })
      });
      if (!response.ok) {
        setError(await response.text());
        return;
      }
      const result = await response.json();
      setNotice(`已忽略 ${result.count ?? 0} 个失败任务。`);
      setSelectedTaskIds([]);
      await loadTasks(taskPage);
    } catch (err) {
      setError(err instanceof Error ? err.message : '忽略任务失败');
    } finally {
      setIgnoringTasks(false);
    }
  }

  function cancelActiveTasks() {
    requestConfirmation({
      title: '取消全部活动任务？',
      message: `将取消当前列表中的待执行和执行中任务。已经生成的文件不会自动删除。`,
      confirmLabel: '取消活动任务',
      tone: 'danger',
      onConfirm: performCancelActiveTasks
    });
  }

  async function performCancelActiveTasks() {
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
    if ((config.upload.includeTypes ?? []).length === 0) {
      setError('请至少选择一种发布文件类型，或关闭自动上传。');
      return;
    }
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
      setSavedConfig(structuredClone(result.config));
      setRestartRequired(Boolean(result.restartRequired));
      setNotice('配置已保存。');
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

  function discardConfigChanges() {
    if (!savedConfig) return;
    setConfig(structuredClone(savedConfig));
    setNotice('未保存的设置已放弃。');
  }

  function handleSettingsTabKeyDown(event: ReactKeyboardEvent<HTMLButtonElement>, currentTab: SettingsTab) {
    const currentIndex = settingsTabOptions.findIndex((option) => option.value === currentTab);
    let nextIndex: number | null = null;
    if (event.key === 'ArrowRight' || event.key === 'ArrowDown') nextIndex = (currentIndex + 1) % settingsTabOptions.length;
    else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') nextIndex = (currentIndex - 1 + settingsTabOptions.length) % settingsTabOptions.length;
    else if (event.key === 'Home') nextIndex = 0;
    else if (event.key === 'End') nextIndex = settingsTabOptions.length - 1;
    if (nextIndex === null) return;

    event.preventDefault();
    const nextTab = settingsTabOptions[nextIndex].value;
    setSettingsTab(nextTab);
    document.getElementById(`settings-tab-${nextTab}`)?.focus();
  }

  const extensionInput = config?.processing.extensions?.join('\n') ?? '';
  const CurrentPageIcon = currentPageMeta.icon;

  return (
    <main className="app-shell">
      <aside className="sidebar">
        <div className="brand-lockup">
          <span className="brand-mark" aria-hidden="true"><Film size={20} /></span>
          <div className="brand-copy">
            <h1>Nya Media</h1>
            <span>Metadata Desktop</span>
          </div>
        </div>
        <nav className="module-nav" aria-label="应用模块">
          <p className="nav-section-label">工作台</p>
          {workspacePages.map((page) => <TabButton key={page} active={activePage === page} label={pageMeta[page].title} icon={pageMeta[page].icon} onClick={() => navigate(page)} />)}
          <p className="nav-section-label">自动化</p>
          {automationPages.map((page) => (
            <TabButton
              key={page}
              active={activePage === page}
              label={pageMeta[page].title}
              icon={pageMeta[page].icon}
              badge={page === 'tasks' && activeTaskCount ? activeTaskCount : page === 'uploads' && uploadSummary.failed ? uploadSummary.failed : undefined}
              badgeTone={page === 'uploads' && uploadSummary.failed ? 'danger' : 'default'}
              onClick={() => navigate(page)}
            />
          ))}
        </nav>
        <div className="sidebar-footer">
          <TabButton active={activePage === 'settings'} label={pageMeta.settings.title} icon={Settings} badge={configDirty ? 1 : undefined} badgeTone="warn" onClick={() => navigate('settings')} />
          <button className="service-mini" type="button" onClick={() => navigate('dashboard')}>
            <span className={connectionOnline ? 'connection-dot online' : 'connection-dot offline'} aria-hidden="true" />
            <span className="service-copy"><strong>{connectionOnline ? '后台运行正常' : '连接已中断'}</strong><small>{healthCheckedAt ? `${healthCheckedAt.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })} 更新` : '正在连接'}</small></span>
            <Activity size={16} aria-hidden="true" />
          </button>
        </div>
      </aside>

      <section className="content-panel">
        <header className="workspace-header">
          <div className="workspace-title">
            <span className="workspace-title-icon" aria-hidden="true"><CurrentPageIcon size={19} /></span>
            <div>
              <h2>{currentPageMeta.title}</h2>
              <p>{currentPageMeta.description}</p>
            </div>
          </div>
          <div className="workspace-status">
            <span className="environment-chip" title={runtimeInfo?.desktop ? runtimeInfo.dataDir : '通过浏览器访问后台服务'}>
              {runtimeInfo?.desktop ? <Monitor size={15} /> : <Database size={15} />}
              {runtimeInfo?.desktop ? `${runtimeInfo.platform} · ${runtimeInfo.version}` : 'Web 管理模式'}
            </span>
            <button className="icon-button secondary" type="button" title="刷新页面" aria-label="刷新页面" onClick={() => window.location.reload()}><RefreshCw size={17} /></button>
          </div>
        </header>

        {restartRequired && (
          <section className="restart-banner" role="status">
            <RefreshCw size={18} aria-hidden="true" />
            <div><strong>配置将在重启后完全生效</strong><span>当前任务可以继续运行；完成后关闭并重新打开应用即可。</span></div>
          </section>
        )}
        {notice && <section className="toast-card" role="status"><CheckCircle2 size={18} /><span>{notice}</span><button className="toast-close" type="button" aria-label="关闭通知" onClick={() => setNotice('')}><X size={16} /></button></section>}
        {initialLoading && <InitialLoading />}

        <div className={initialLoading ? 'workspace-content loading' : 'workspace-content'}>

        {activePage === 'dashboard' && (
        <section className="dashboard-page">
          <section className="dashboard-overview">
            <div className="dashboard-heading">
              <p className="eyebrow">Dashboard</p>
              <h2>媒体元数据控制台</h2>
              <p>查看服务状态、任务队列、目录监听和本地工具可用性。</p>
            </div>
            <div className="dashboard-metrics" aria-label="运行概览">
              <DashboardMetric label="服务状态" value={health?.status ?? 'loading'} tone={health?.status === 'ok' ? 'good' : 'warn'} />
              <DashboardMetric label="任务总数" value={String(taskTotal)} />
              <DashboardMetric label="活跃任务" value={String(activeTaskCount)} tone={activeTaskCount ? 'warn' : 'neutral'} />
              <DashboardMetric label="失败任务" value={String(failedTaskCount)} tone={failedTaskCount ? 'bad' : 'good'} />
              <DashboardMetric label="媒体目录" value={`${enabledWatchDirCount}/${watchDirs.length}`} />
              <DashboardMetric label="可用工具" value={`${availableToolCount}/${tools.length || 4}`} tone={tools.length && availableToolCount !== tools.length ? 'bad' : 'good'} />
            </div>
          </section>

          {(!coreToolsReady || watchDirs.length === 0) && (
            <section className="setup-checklist" aria-labelledby="setup-checklist-title">
              <div className="setup-checklist-heading">
                <div><span className="eyebrow">运行准备</span><h3 id="setup-checklist-title">完成基础配置</h3></div>
                <span className="setup-progress">{Number(coreToolsReady) + Number(watchDirs.length > 0)}/2 已完成</span>
              </div>
              <div className="setup-rows">
                <SetupRow icon={FileCheck2} title="媒体工具" detail={coreToolsReady ? 'ffmpeg 与 ffprobe 已可用' : '需要配置 ffmpeg 与 ffprobe'} complete={coreToolsReady} actionLabel={coreToolsReady ? undefined : '配置工具'} onAction={() => { setSettingsTab('basic'); navigate('settings'); }} />
                <SetupRow icon={FolderCog} title="媒体目录" detail={watchDirs.length ? `已添加 ${watchDirs.length} 个目录` : '尚未添加自动扫描目录'} complete={watchDirs.length > 0} actionLabel={watchDirs.length ? undefined : '添加目录'} onAction={() => { setNewWatchDirProcessing(outputProcessingFromConfig(config)); setAddWatchDirOpen(true); }} />
                <SetupRow icon={WandSparkles} title="元数据来源" detail={config?.scraping.enableTmdb ? 'TMDB 刮削已开启' : '可选：配置 TMDB 以补全剧集资料'} complete={Boolean(config?.scraping.enableTmdb)} optional actionLabel={config?.scraping.enableTmdb ? undefined : '查看设置'} onAction={() => { setSettingsTab('scraping'); navigate('settings'); }} />
              </div>
            </section>
          )}

          <section className="dashboard-content-grid">
            <Card title="配置摘要">
              <Row label="运行模式" value={runtimeInfo?.desktop ? '桌面内置服务（无本地端口）' : `Web · ${config?.server.addr ?? '-'}`} />
              <Row label="显示时区" value={displayTimezone} />
              <Row label="数据库" value={runtimeInfo?.database || config?.database.path || '-'} />
              <Row label="扫描并发" value={String(config?.processing.concurrency ?? '-')} />
              <Row label="重命名并发" value={String(config?.renaming?.concurrency ?? '-')} />
              <Row label="扩展名" value={config?.processing.extensions?.join(', ') ?? '-'} />
              <Row label="TMDB" value={config?.scraping.enableTmdb ? `${config?.scraping.tmdbBaseUrl ?? '-'} · ${config?.scraping.tmdbRequestTimeoutSeconds ?? '-'}s` : '关闭'} />
            </Card>

            <Card title="处理能力">
              <div className="feature-grid">
                <DashboardFeature label="字幕提取" enabled={config?.processing.enableSubtitles} />
                <DashboardFeature label="MediaInfo" enabled={config?.processing.enableMediaInfo} />
                <DashboardFeature label="NFO" enabled={config?.processing.enableNfo} />
                <DashboardFeature label="BIF" enabled={config?.processing.enableBif} />
                <DashboardFeature label="图片接管" enabled={config?.processing.enableImageTakeover} />
                <DashboardFeature label="人物刮削" enabled={config?.scraping.enablePeople} />
              </div>
            </Card>

            <Card title="工具状态" action={<button onClick={checkTools} disabled={checkingTools}>{checkingTools ? '检测中' : '重新检测'}</button>}>
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
        </section>
      )}

        {activePage === 'settings' && (
        <section className="page-grid settings-grid">
            <Card title="设置" action={<div className="inline-actions"><button className="secondary icon-text-button" type="button" onClick={discardConfigChanges} disabled={!configDirty || savingConfig}><RefreshCw size={16} />放弃修改</button><button className="icon-text-button" onClick={saveConfig} disabled={savingConfig || !configDirty || !config || (config.upload.includeTypes ?? []).length === 0}><Save size={16} />{savingConfig ? '保存中' : '保存配置'}</button></div>}>
            {config ? (
              <div className="config-form settings-form">
                <div className="settings-tabs" role="tablist" aria-label="设置分类" aria-orientation="horizontal">
                  {settingsTabOptions.map((option) => <button
                    id={`settings-tab-${option.value}`}
                    className={settingsTab === option.value ? 'status-tab active' : 'status-tab'}
                    type="button"
                    role="tab"
                    aria-selected={settingsTab === option.value}
                    aria-controls={`settings-panel-${option.value}`}
                    tabIndex={settingsTab === option.value ? 0 : -1}
                    key={option.value}
                    onClick={() => setSettingsTab(option.value)}
                    onKeyDown={(event) => handleSettingsTabKeyDown(event, option.value)}
                  >{option.label}</button>)}
                </div>
                <section id="settings-panel-basic" className={`settings-section ${settingsTab === 'basic' ? 'active' : ''}`} role="tabpanel" aria-labelledby="settings-tab-basic" hidden={settingsTab !== 'basic'}>
                  <ThemeSelector value={themeMode} onChange={setThemeMode} />
                  <label>显示时区<input list="timezone-options" value={config.server.timezone} onChange={(event) => updateConfig((draft) => { draft.server.timezone = event.target.value; })} placeholder="Asia/Shanghai" /></label>
                  <datalist id="timezone-options">
                    {timeZoneOptions.map((timezone) => <option key={timezone} value={timezone} />)}
                  </datalist>
                  <PathField label="ffmpeg" value={config.tools.ffmpeg} onChange={(value) => updateConfig((draft) => { draft.tools.ffmpeg = value; })} onBrowse={() => void browseFile({ title: '选择 ffmpeg', value: config.tools.ffmpeg, onSelect: (value) => updateConfig((draft) => { draft.tools.ffmpeg = value; }) })} />
                  <PathField label="ffprobe" value={config.tools.ffprobe} onChange={(value) => updateConfig((draft) => { draft.tools.ffprobe = value; })} onBrowse={() => void browseFile({ title: '选择 ffprobe', value: config.tools.ffprobe, onSelect: (value) => updateConfig((draft) => { draft.tools.ffprobe = value; }) })} />
                  <PathField label="mkvextract" value={config.tools.mkvextract} onChange={(value) => updateConfig((draft) => { draft.tools.mkvextract = value; })} onBrowse={() => void browseFile({ title: '选择 mkvextract', value: config.tools.mkvextract, onSelect: (value) => updateConfig((draft) => { draft.tools.mkvextract = value; }) })} />
                  <PathField label="mediainfo" value={config.tools.mediainfo} onChange={(value) => updateConfig((draft) => { draft.tools.mediainfo = value; })} onBrowse={() => void browseFile({ title: '选择 mediainfo', value: config.tools.mediainfo, onSelect: (value) => updateConfig((draft) => { draft.tools.mediainfo = value; }) })} />
                </section>
                <section id="settings-panel-processing" className={`settings-section ${settingsTab === 'processing' ? 'active' : ''}`} role="tabpanel" aria-labelledby="settings-tab-processing" hidden={settingsTab !== 'processing'}>
                  <label className="extensions-field">扩展名<textarea value={extensionInput} onChange={(event) => updateConfig((draft) => { draft.processing.extensions = normalizeExtensions(event.target.value); })} placeholder={commonVideoExtensions.join('\n')} rows={8} /><small>每行一个后缀，或用逗号分隔，例如 `.mkv`、`.mp4`、`.rmvb`。</small></label>
                  <label>扫描处理并发<input type="number" min="1" value={config.processing.concurrency} onChange={(event) => updateConfig((draft) => { draft.processing.concurrency = Number(event.target.value); })} /></label>
                  <label>整理命名并发<input type="number" min="1" max="8" value={config.renaming?.concurrency ?? 3} onChange={(event) => updateConfig((draft) => { draft.renaming = { ...(draft.renaming ?? { concurrency: 3 }), concurrency: Number(event.target.value) }; })} /><small>用于生成预览、批量修正季集、批量应用剧集；设为 1 可降低 TMDB 风控风险。</small></label>
                  <SelectField label="处理策略" value={config.processing.strategy || 'missing'} options={[{ code: 'missing', name: '只补缺失' }, { code: 'force', name: '强制重建' }]} onChange={(value) => updateConfig((draft) => { draft.processing.strategy = value as RescanStrategy; })} />
                  <Toggle label="字幕提取" checked={config.processing.enableSubtitles} onChange={(value) => updateConfig((draft) => { draft.processing.enableSubtitles = value; })} />
                  <Toggle label="MediaInfo" checked={config.processing.enableMediaInfo} onChange={(value) => updateConfig((draft) => { draft.processing.enableMediaInfo = value; })} />
                  <Toggle label="NFO" checked={config.processing.enableNfo} onChange={(value) => updateConfig((draft) => { draft.processing.enableNfo = value; })} />
                  <Toggle label="BIF" checked={config.processing.enableBif} onChange={(value) => updateConfig((draft) => { draft.processing.enableBif = value; })} />
                  <label>BIF 宽度<input type="number" value={config.processing.bifWidth} onChange={(event) => updateConfig((draft) => { draft.processing.bifWidth = Number(event.target.value); })} /></label>
                  <label>BIF 间隔秒<input type="number" value={config.processing.bifInterval} onChange={(event) => updateConfig((draft) => { draft.processing.bifInterval = Number(event.target.value); })} /></label>
                  <SelectField label="BIF 加速" value={config.processing.bifHwAccel || 'cpu'} options={bifHwAccelOptions} onChange={(value) => updateConfig((draft) => { draft.processing.bifHwAccel = value; })} />
                </section>
                <section id="settings-panel-uploads" className={`settings-section settings-section-wide ${settingsTab === 'uploads' ? 'active' : ''}`} role="tabpanel" aria-labelledby="settings-tab-uploads" hidden={settingsTab !== 'uploads'}>
                  <SettingsGroup title="上传调度">
                    <Toggle label="元数据完成后自动创建上传批次" checked={config.upload.enabled} onChange={(value) => updateConfig((draft) => { draft.upload.enabled = value; })} />
                    <label>上传目标并发<input type="number" min="1" max="8" value={config.upload.concurrency} onChange={(event) => updateConfig((draft) => { draft.upload.concurrency = Number(event.target.value); })} /></label>
                    <label>番剧变更合并秒数<input type="number" min="1" value={Math.max(1, Math.round((config.upload.quietPeriod ?? 120000000000) / 1_000_000_000))} onChange={(event) => updateConfig((draft) => { draft.upload.quietPeriod = Math.max(1, Number(event.target.value) || 1) * 1_000_000_000; })} /><small>同一番剧在这个安静窗口内只生成一个上传批次。</small></label>
                    <label>失败自动重试次数<input type="number" min="1" max="10" value={config.upload.maxAttempts} onChange={(event) => updateConfig((draft) => { draft.upload.maxAttempts = Number(event.target.value); })} /></label>
                    <fieldset className="upload-type-fieldset"><legend>发布文件类型</legend><div className="upload-type-grid">{uploadTypeOptions.map((option) => <label className="checkbox-label" key={option.value}><input type="checkbox" checked={(config.upload.includeTypes ?? []).includes(option.value)} onChange={(event) => updateConfig((draft) => { const current = new Set(draft.upload.includeTypes ?? []); if (event.target.checked) current.add(option.value); else current.delete(option.value); draft.upload.includeTypes = Array.from(current); })} />{option.label}</label>)}</div>{(config.upload.includeTypes ?? []).length === 0 && <small className="upload-selection-warning">至少选择一种文件类型，自动上传才可保存。</small>}</fieldset>
                    <p className="settings-note">网盘账号、Cookie 和目标目录在“上传”页面管理。一个批次会为每个启用目标保留独立状态，后续通知可按目标分别消费。</p>
                  </SettingsGroup>
                </section>
                <section id="settings-panel-scraping" className={`settings-section settings-section-wide ${settingsTab === 'scraping' ? 'active' : ''}`} role="tabpanel" aria-labelledby="settings-tab-scraping" hidden={settingsTab !== 'scraping'}>
                  <SettingsGroup title="刮削内容">
                    <Toggle label="TMDB 刮削" checked={config.scraping.enableTmdb} onChange={(value) => updateConfig((draft) => { draft.scraping.enableTmdb = value; })} />
                    <Toggle label="刮削演员/职员" checked={config.scraping.enablePeople} onChange={(value) => updateConfig((draft) => { draft.scraping.enablePeople = value; })} />
                    <SelectField label="刮削语言" value={config.scraping.language} options={languageOptions} onChange={(value) => updateConfig((draft) => { draft.scraping.language = value; })} />
                    <LanguageMultiPicker label="备用语言顺序" values={config.scraping.fallbackLanguages ?? []} onChange={(values) => updateConfig((draft) => { draft.scraping.fallbackLanguages = values; })} />
                    <SelectField label="刮削地区" value={config.scraping.region} options={regionOptions} onChange={(value) => updateConfig((draft) => { draft.scraping.region = value; })} />
                  </SettingsGroup>
                  <SettingsGroup title="图片与海报">
                    <Toggle label="接管剧集/季度图片" checked={config.processing.enableImageTakeover} onChange={(value) => updateConfig((draft) => { draft.processing.enableImageTakeover = value; })} />
                    <Toggle label="优先原语言海报" checked={config.scraping.preferOriginalLanguagePoster} onChange={(value) => updateConfig((draft) => { draft.scraping.preferOriginalLanguagePoster = value; })} />
                    <ImageSourcePriorityPicker label="图片源顺序" values={config.scraping.imageSources ?? []} onChange={(values) => updateConfig((draft) => { draft.scraping.imageSources = values; })} />
                  </SettingsGroup>
                </section>
                <section id="settings-panel-sources" className={`settings-section settings-section-wide ${settingsTab === 'sources' ? 'active' : ''}`} role="tabpanel" aria-labelledby="settings-tab-sources" hidden={settingsTab !== 'sources'}>
                  <SettingsGroup title="TMDB">
                    <label>Token<input type="password" value={config.scraping.tmdbToken} onChange={(event) => updateConfig((draft) => { draft.scraping.tmdbToken = event.target.value; })} placeholder="Bearer token" /></label>
                    <label>API Key<input value={config.scraping.tmdbApiKey} onChange={(event) => updateConfig((draft) => { draft.scraping.tmdbApiKey = event.target.value; })} placeholder="可选，优先使用 Token" /></label>
                    <label>接口地址<input value={config.scraping.tmdbBaseUrl} onChange={(event) => updateConfig((draft) => { draft.scraping.tmdbBaseUrl = event.target.value; })} placeholder="https://api.themoviedb.org" /><small>程序会自动追加 `/3`，这里只填前缀，支持子目录。</small></label>
                    <label>图片下载地址<input value={config.scraping.tmdbImageBaseUrl} onChange={(event) => updateConfig((draft) => { draft.scraping.tmdbImageBaseUrl = event.target.value; })} placeholder="https://image.tmdb.org" /><small>程序会自动追加 `/t/p/original`，这里只填前缀，支持子目录。NFO 仍写官方地址。</small></label>
                    <label>接口超时秒<input type="number" min="3" max="60" value={config.scraping.tmdbRequestTimeoutSeconds} onChange={(event) => updateConfig((draft) => { draft.scraping.tmdbRequestTimeoutSeconds = Number(event.target.value); })} /><small>只影响 TMDB API 请求，不影响图片下载。</small></label>
                  </SettingsGroup>
                  <SettingsGroup title="Fanart">
                    <label>API Key<input type="password" value={config.scraping.fanartApiKey} onChange={(event) => updateConfig((draft) => { draft.scraping.fanartApiKey = event.target.value; })} placeholder="用于 clearart/clearlogo" /></label>
                    <label>接口地址<input value={config.scraping.fanartBaseUrl} onChange={(event) => updateConfig((draft) => { draft.scraping.fanartBaseUrl = event.target.value; })} placeholder="https://webservice.fanart.tv" /><small>程序会自动追加 `/v3`，这里只填前缀，支持子目录。</small></label>
                    <p className="settings-note">剧集图片需要 TVDB ID；当前会从 TMDB external IDs 自动映射，映射不到时跳过 Fanart。</p>
                  </SettingsGroup>
                  <SettingsGroup title="网络">
                    <label>代理<input value={config.scraping.proxy} onChange={(event) => updateConfig((draft) => { draft.scraping.proxy = event.target.value; })} placeholder="http://127.0.0.1:7890" /></label>
                  </SettingsGroup>
                </section>
              </div>
            ) : <p className="muted">配置加载中。</p>}
          </Card>
        </section>
      )}

        {activePage === 'watchDirs' && (
        <section className="page-grid">
          <Card title="媒体目录" action={<div className="inline-actions"><button className="secondary" onClick={() => openRescanDialog('all')} disabled={rescanning}>{rescanning ? '扫描中' : '扫描生成'}</button><button onClick={() => { setNewWatchDirProcessing(outputProcessingFromConfig(config)); setAddWatchDirOpen(true); }}>添加媒体目录</button></div>}>
            {watchDirs.length ? watchDirs.map((dir) => (
              <div className="dir-item" key={dir.id}>
                <div>
                  <strong>{dir.path}</strong>
                  <small>{dir.watchEnabled ? '自动监听' : '不监听'} · {dir.useGlobalProcessing ? '跟随全局处理设置' : '独立处理设置'}</small>
                </div>
                <div className="inline-actions">
                  <button className="secondary" onClick={() => openEditWatchDir(dir)}>编辑</button>
                  <button onClick={() => openRescanDialog('dir', dir.path)} disabled={rescanning}>扫描生成</button>
                  <button className="danger" onClick={() => deleteWatchDir(dir.id)}>删除</button>
                </div>
              </div>
            )) : <p className="muted">尚未配置媒体目录。</p>}
          </Card>
        </section>
      )}

        {activePage === 'rename' && (
        <section className="page-grid rename-page-grid">
          <Card title="整理命名" action={<button className="secondary" type="button" onClick={() => setRenameHistoryOpen(true)}>重命名历史{renameHistory.length ? ` (${renameHistory.length})` : ''}</button>}>
            <div className="rename-controls">
              <label className="rename-control-primary">目录或文件路径<div className="path-input"><input value={renamePath} onChange={(event) => setRenamePath(event.target.value)} placeholder="D:\\Media\\Anime\\Season 1" /><button type="button" className="icon-text-button" onClick={() => void browseDirectory({ title: '选择整理目录', value: renamePath, onSelect: setRenamePath })}><FolderOpen size={16} />选择</button></div></label>
              <label className="rename-control-primary">命名模板
                <div className="template-input-row">
                  <button className="target-path-preview rename-template-preview" type="button" onClick={() => setRenameTemplateEditorOpen(true)}>{renameTemplate || defaultRenameTemplate}</button>
                  <div className="template-history-picker">
                    <button className="secondary template-history-trigger" type="button" onClick={() => setRenameTemplateHistoryOpen((value) => !value)} disabled={!renameTemplateHistory.length}>最近模板</button>
                    {renameTemplateHistoryOpen ? <div className="template-history-menu">
                      {renameTemplateHistory.map((template) => <div className="template-history-item" key={template}>
                        <button className="template-history-use" type="button" title={template} onClick={() => { setRenameTemplate(template); setRenameTemplateHistoryOpen(false); }}>{template}</button>
                        <button className="template-history-delete" type="button" title="删除最近模板" aria-label={`删除模板 ${template}`} onClick={() => deleteRenameTemplateHistory(template)}><span aria-hidden="true">&times;</span></button>
                      </div>)}
                    </div> : null}
                  </div>
                </div>
              </label>
              <SelectField label="查询语言" value={renameLanguage} options={languageOptions} onChange={setRenameLanguage} />
              <label>字幕组<input value={renameReleaseGroup} onChange={(event) => setRenameReleaseGroup(event.target.value)} placeholder="留空则从原文件名识别" /></label>
              <div className="rename-preview-action">
                <button className="secondary" type="button" onClick={previewingRename ? cancelRenamePreview : () => void previewRename(true)}>{previewingRename ? '取消预览' : '忽略缓存重新生成'}</button>
                <button type="button" onClick={() => void previewRename()} disabled={previewingRename}>{previewingRename ? renamePreviewTotal ? `生成预览 ${renamePreviewCount} / ${renamePreviewTotal}` : '正在扫描文件…' : '生成预览'}</button>
              </div>
            </div>
          </Card>

          <Card title="重命名预览" action={<div className="rename-preview-summary"><span>共 <strong>{renamePreview.length}</strong> 项</span><span>已选 <strong>{selectedRenamePaths.length}</strong></span><span className={renameWarningCount ? 'warn' : ''}>警告 <strong>{renameWarningCount}</strong></span><span className={renameErrorCount ? 'bad' : ''}>错误 <strong>{renameErrorCount}</strong></span></div>}>
            <div className="rename-match-bar">
              <div className="rename-action-row">
                <div className="inline-actions rename-bulk-actions">
                  <button className="secondary" type="button" onClick={selectAllRenameItems} disabled={!renamePreview.length}>全选</button>
                  <button className="secondary" type="button" onClick={invertRenameSelection} disabled={!renamePreview.length}>反选</button>
                  <button className="secondary" type="button" onClick={openBatchEpisodeDialog} disabled={!selectedRenamePaths.length}>批量修正季集</button>
                  <button className="secondary" type="button" onClick={() => setTmdbMatchOpen(true)} disabled={!selectedRenamePaths.length}>更改匹配剧集</button>
                  <span className="rename-preview-stats">并发 {renameBatchConcurrency}</span>
                </div>
                <button className="rename-apply-button" type="button" onClick={applySelectedRenames} disabled={applyingRename || !selectedRenamePaths.length}>{applyingRename ? '重命名中' : `执行选中重命名 (${selectedRenamePaths.length})`}</button>
              </div>
            </div>
            <div className="task-table-wrap">
              <table className="task-table rename-table">
                <thead>
                  <tr>
                    <th>选择</th>
                    <th>状态</th>
                    <th>识别结果</th>
                    <th>原文件名</th>
                    <th>新文件名</th>
                    <th>说明</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {renamePreview.length ? renamePreview.map((item, index) => {
                    const recalculatingItem = recalculatingRenamePaths.includes(item.path);
                    return (
                    <tr className={selectedRenamePaths.includes(item.path) ? 'rename-row selected' : 'rename-row'} key={item.path} onClick={(event) => handleRenameRowClick(event, item, index)} title="点击行选择，Shift+点击连续选择">
                      <td><span className={selectedRenamePaths.includes(item.path) ? 'rename-row-index selected' : 'rename-row-index'} aria-hidden="true"><strong>{index + 1}</strong></span></td>
                      <td>
                        <div className="rename-status-cell">
                          <span className={`pill ${item.status === 'error' ? 'bad' : item.status === 'ok' ? 'ok' : ''}`}>{item.status}</span>
                          <small title={`身份来源：${renameIdentitySourceLabel(item.identitySource || item.source)}`}>{renameIdentitySourceLabel(item.identitySource || item.source)}</small>
                          <small title={`元数据：${renameMetadataSourceLabel(item.metadataSource)}`}>{item.metadataSource === 'tmdb' ? 'TMDB' : renameMetadataSourceLabel(item.metadataSource)}</small>
                        </div>
                      </td>
                      <td className="rename-edit-cell">
                        <label className="rename-edit-field">
                          <span>剧名</span>
                          <input className="rename-readonly-input" value={item.show || ''} readOnly title="请勾选文件后使用“更改匹配剧集”修改剧集" placeholder="剧名" />
                        </label>
                        <div className="rename-episode-edit">
                          <label className="rename-edit-field">
                            <span>季</span>
                            <input type="number" min="0" value={item.season ?? 0} onChange={(event) => updateRenameItem(item.path, { season: Number(event.target.value) })} onKeyDown={(event) => { if (event.key === 'Enter') void recalculateRenameItem({ ...item, manualName: false }, { forceTmdb: true, keepManualName: false }); }} title="季，回车重新查 TMDB" />
                          </label>
                          <label className="rename-edit-field">
                            <span>集</span>
                            <input type="number" min="0" value={item.episode ?? 0} onChange={(event) => updateRenameItem(item.path, { episode: Number(event.target.value) })} onKeyDown={(event) => { if (event.key === 'Enter') void recalculateRenameItem({ ...item, manualName: false }, { forceTmdb: true, keepManualName: false }); }} title="集，回车重新查 TMDB" />
                          </label>
                        </div>
                        <label className="rename-edit-field">
                          <span>标题</span>
                          <input className="rename-readonly-input" value={item.title || ''} readOnly title="如需自定义标题，请直接编辑“新文件名”" placeholder="标题" />
                        </label>
                        {item.tmdbShowId ? <button className="tmdb-detail-link" type="button" onClick={() => void openTmdbEpisodeDetail(item)} disabled={loadingTmdbEpisodeDetail}>TMDB #{item.tmdbShowId}</button> : null}
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
                          <button type="button" title="根据当前剧名、季、集重新查询 TMDB 并生成预览" onClick={() => recalculateRenameItem({ ...item, manualName: false }, { forceTmdb: true, keepManualName: false })} disabled={applyingTmdbShowId !== null || applyingBatchEpisode || recalculatingItem}>{recalculatingItem ? '生成中' : '重新生成'}</button>
                        </div>
                      </td>
                    </tr>
                  );
                  }) : (
                    <tr><td colSpan={7} className="empty-cell">尚未生成预览。</td></tr>
                  )}
                </tbody>
              </table>
            </div>
          </Card>
        </section>
      )}

        {activePage === 'audit' && (
        <section className="page-grid audit-page-grid">
          <div className="audit-tabs" role="group" aria-label="核对类型">
            <button className={auditTab === 'missing' ? 'status-tab active' : 'status-tab'} type="button" aria-pressed={auditTab === 'missing'} onClick={() => setAuditTab('missing')}>剧集缺漏</button>
            <button className={auditTab === 'emby' ? 'status-tab active' : 'status-tab'} type="button" aria-pressed={auditTab === 'emby'} onClick={() => setAuditTab('emby')}>Emby 与本地核对</button>
            <button className={auditTab === 'files' ? 'status-tab active' : 'status-tab'} type="button" aria-pressed={auditTab === 'files'} onClick={() => setAuditTab('files')}>文件对齐检查</button>
          </div>

          {auditTab === 'missing' && <Card title="剧集缺漏" action={<button onClick={() => runSeriesAudit('missing')} disabled={auditingMissing}>{auditingMissing ? '核对中' : '开始核对'}</button>}>
            <div className="audit-controls">
              <label>剧集根目录<div className="path-input"><input value={auditRoot} onChange={(event) => setAuditRoot(event.target.value)} placeholder="D:\Media\TV\Example Show" /><button type="button" className="icon-text-button" onClick={() => void browseDirectory({ title: '选择剧集根目录', value: auditRoot, onSelect: setAuditRoot })}><FolderOpen size={16} />选择</button></div></label>
              <label><FieldLabel label="TMDB 剧集 ID" help="留空时读取 tvshow.nfo 中的 TMDB ID；手动填写或选择剧集会覆盖自动读取的 ID。" /><div className="path-input"><input value={auditTmdbId} onChange={(event) => setAuditTmdbId(event.target.value)} inputMode="numeric" placeholder="可选，优先于 tvshow.nfo" /><button type="button" onClick={openAuditTmdbMatch}>选择剧集</button></div></label>
            </div>
            <div className="audit-option-row">
              <Toggle label={<FieldLabel label="检查 Season 0" help="开启后，Season 0 会参与缺漏判断和产物检查。" />} checked={auditIncludeSeasonZero} onChange={setAuditIncludeSeasonZero} />
            </div>
          </Card>}

          {auditTab === 'emby' && <Card title="Emby 与本地核对" action={<button onClick={() => runSeriesAudit('emby')} disabled={auditingEmby}>{auditingEmby ? '核对中' : '开始核对'}</button>}>
            <div className="emby-audit-form">
              <section className="audit-form-section">
                <div className="audit-form-section-heading">
                  <strong>核对目标</strong>
                  <span>选择本地剧集，并粘贴对应的 Emby 剧集详情页地址。</span>
                </div>
                <div className="emby-audit-targets">
                  <label>本地剧集根目录<div className="path-input"><input value={auditRoot} onChange={(event) => setAuditRoot(event.target.value)} placeholder="D:\Media\TV\Example Show" /><button type="button" className="icon-text-button" onClick={() => void browseDirectory({ title: '选择本地剧集根目录', value: auditRoot, onSelect: setAuditRoot })}><FolderOpen size={16} />选择</button></div></label>
                  <label>Emby 剧集页面 URL<input value={auditEmbyItemUrl} onChange={(event) => setAuditEmbyItemUrl(event.target.value)} placeholder="https://emby.example.com/web/index.html#!/item?id=662" /></label>
                </div>
              </section>
              <section className="audit-form-section">
                <div className="audit-form-section-heading">
                  <strong>访问凭证</strong>
                  <span>选择已保存的 Key，或输入仅用于本次核对的临时 Key。</span>
                </div>
                <div className="emby-key-list-block">
                  <span>已保存的 API Key</span>
                  <div className="emby-key-list">
                    {auditEmbyAPIKeys.map((key) => (
                      <div className={auditSelectedEmbyKeyId === String(key.id) ? 'emby-key-item selected' : 'emby-key-item'} key={key.id}>
                        <button className="emby-key-use" type="button" onClick={() => { setAuditSelectedEmbyKeyId(String(key.id)); setAuditEmbyApiKey(''); }}>
                          {auditSelectedEmbyKeyId === String(key.id) ? <span className="emby-key-check" aria-hidden="true">✓</span> : null}
                          <span>{key.title}</span>
                        </button>
                        <button className="template-history-delete" type="button" title="删除 API Key" aria-label={`删除 API Key ${key.title}`} onClick={() => deleteEmbyAPIKey(key.id)}><X size={15} aria-hidden="true" /></button>
                      </div>
                    ))}
                    <button className="emby-key-add" type="button" onClick={() => { setNewEmbyKeyTitle(''); setNewEmbyKeyValue(''); setAddEmbyKeyOpen(true); }}>+ 添加 API Key</button>
                  </div>
                </div>
                <div className="emby-audit-credentials">
                  <label>临时 API Key<input type="password" value={auditEmbyApiKey} onChange={(event) => { setAuditEmbyApiKey(event.target.value); if (event.target.value) setAuditSelectedEmbyKeyId(''); }} placeholder="不保存，仅用于本次核对" /></label>
                </div>
              </section>
            </div>
          </Card>}

          {auditTab === 'files' && <Card title="文件对齐检查" action={<button onClick={runFileAudit} disabled={auditingFiles}>{auditingFiles ? '检查中' : '开始检查'}</button>}>
            <div className="file-audit-form">
              <section className="audit-form-section">
                <div className="audit-form-section-heading">
                  <strong>核对目录</strong>
                  <span>比较本地目录与远端 SFTP 目录中的文件树。</span>
                </div>
                <div className="file-audit-targets">
                  <label>本地目录<div className="path-input"><input value={fileAuditLocalRoot} onChange={(event) => setFileAuditLocalRoot(event.target.value)} placeholder="D:\Media\TV\Example Show" /><button type="button" className="icon-text-button" onClick={() => void browseDirectory({ title: '选择本地目录', value: fileAuditLocalRoot, onSelect: setFileAuditLocalRoot })}><FolderOpen size={16} />选择</button></div></label>
                  <label>远端目录<input value={fileAuditRemoteRoot} onChange={(event) => setFileAuditRemoteRoot(event.target.value)} placeholder="/media/TV/Example Show" /></label>
                </div>
              </section>
              <section className="audit-form-section">
                <div className="audit-form-section-heading">
                  <strong>SFTP 连接</strong>
                  <span>密码仅用于本次检查，不会保存。密码与私钥可任选其一。</span>
                </div>
                <div className="file-audit-connection">
                  <label>SFTP 地址<input value={fileAuditSFTPAddr} onChange={(event) => setFileAuditSFTPAddr(event.target.value)} placeholder="nas.example.com:22" /></label>
                  <label>SFTP 用户<input value={fileAuditSFTPUser} onChange={(event) => setFileAuditSFTPUser(event.target.value)} placeholder="user" /></label>
                  <label>SFTP 密码<input type="password" value={fileAuditSFTPPassword} onChange={(event) => setFileAuditSFTPPassword(event.target.value)} placeholder="可选，不保存" /></label>
                  <PathField label="私钥路径" value={fileAuditSFTPKeyPath} placeholder="C:\Users\me\.ssh\id_ed25519" onChange={setFileAuditSFTPKeyPath} onBrowse={() => void browseFile({ title: '选择 SFTP 私钥', value: fileAuditSFTPKeyPath, onSelect: setFileAuditSFTPKeyPath })} />
                  <PathField label="known_hosts" value={fileAuditSFTPKnownHostsPath} placeholder="C:\Users\me\.ssh\known_hosts" onChange={setFileAuditSFTPKnownHostsPath} onBrowse={() => void browseFile({ title: '选择 known_hosts', value: fileAuditSFTPKnownHostsPath, onSelect: setFileAuditSFTPKnownHostsPath })} />
                </div>
                <div className="file-audit-security-option">
                  <Toggle label={<FieldLabel label="跳过 SFTP 主机指纹校验" help="仅在无法提供 known_hosts 时使用。开启后无法验证连接的远端主机身份。" />} checked={fileAuditSFTPInsecure} onChange={setFileAuditSFTPInsecure} />
                </div>
              </section>
              <section className="audit-form-section">
                <div className="audit-form-section-heading">
                  <strong>比较规则</strong>
                  <span>选择文件匹配方式以及需要核对的内容。</span>
                </div>
                <div className="audit-option-row file-audit-options">
                  <Toggle label={<FieldLabel label="允许视频匹配同名 .strm" help="匹配为 .strm 时不会比较文件大小或 MD5。" />} checked={fileAuditAllowSTRM} onChange={setFileAuditAllowSTRM} />
                  <Toggle label="比较文件大小" checked={fileAuditCompareSize} onChange={setFileAuditCompareSize} />
                  <Toggle label="比较 MD5" checked={fileAuditCompareMD5} onChange={setFileAuditCompareMD5} />
                </div>
              </section>
            </div>
          </Card>}

          {auditTab === 'files' && fileAuditReport && (
            <>
              <Card title="文件对齐摘要">
                <div className="audit-summary-grid file-audit-summary-grid">
                  <div className="audit-stat"><span>本地文件</span><strong>{fileAuditReport.localCount}</strong><small>{fileAuditReport.localRoot}</small></div>
                  <div className="audit-stat"><span>远端文件</span><strong>{fileAuditReport.remoteCount}</strong><small>{fileAuditReport.remoteRoot}</small></div>
                  <div className="audit-stat"><span>差异</span><strong>{fileAuditReport.issues?.length ?? 0}</strong><small>{fileAuditReport.issues?.length ? '需要检查' : '未发现'}</small></div>
                </div>
              </Card>

              <Card title="文件差异">
                <div className="task-table-wrap">
                  <table className="task-table audit-table file-audit-table">
                    <thead>
                      <tr>
                        <th>级别</th>
                        <th>类型</th>
                        <th>相对路径</th>
                        <th>本地</th>
                        <th>远端</th>
                        <th>说明</th>
                      </tr>
                    </thead>
                    <tbody>
                      {fileAuditReport.issues?.length ? fileAuditReport.issues.map((issue, index) => (
                        <tr key={`${issue.type}-${issue.path}-${index}`}>
                          <td><span className={issue.severity === 'error' ? 'pill bad' : 'pill'}>{issue.severity}</span></td>
                          <td>{formatFileAuditIssueType(issue.type)}</td>
                          <td className="path-cell">{issue.path}</td>
                          <td className="path-cell">{issue.local || '-'}</td>
                          <td className="path-cell">{issue.remote || '-'}</td>
                          <td className="path-cell">{issue.detail || '-'}</td>
                        </tr>
                      )) : <tr><td colSpan={6} className="empty-cell">未发现文件差异。</td></tr>}
                    </tbody>
                  </table>
                </div>
              </Card>
            </>
          )}

          {auditTab === 'emby' && embyAuditReport && (
            <>
              <Card title="Emby 与本地核对摘要">
                <div className="audit-summary-grid emby-audit-summary-grid">
                  <div className="audit-stat"><span>剧集</span><strong>{embyAuditReport.showTitle || '-'}</strong><small>{embyAuditReport.root}</small></div>
                  <div className="audit-stat"><span>本地单集</span><strong>{embyAuditReport.localEpisodes.length}</strong><small>参与核对</small></div>
                  <div className="audit-stat"><span>Emby 差异</span><strong>{embyAuditReport.embyComparisons?.length ?? 0}</strong><small>{embyAuditReport.embyComparisons?.length ? '需要检查' : '未发现'}</small></div>
                </div>
              </Card>

              <Card title="Emby 差异">
                <p className="muted">这里只列出本地与 Emby 不一致的问题；没有行表示当前对比项未发现差异。对比范围包括剧集、季度和单集的标题、简介、图片存在性、可用的 TMDB ID 和视频源。</p>
                <div className="task-table-wrap">
                  <table className="task-table audit-table">
                    <thead>
                      <tr>
                        <th>级别</th>
                        <th>对象</th>
                        <th>字段</th>
                        <th>本地</th>
                        <th>Emby</th>
                        <th>说明</th>
                      </tr>
                    </thead>
                    <tbody>
                      {embyAuditReport.embyComparisons?.length ? embyAuditReport.embyComparisons.map((issue, index) => (
                        <tr key={`${issue.season}-${issue.episode}-${issue.field}-${index}`}>
                          <td><span className={issue.severity === 'error' ? 'pill bad' : 'pill'}>{issue.severity}</span></td>
                          <td>{formatAuditIssueTarget(issue)}</td>
                          <td>{issue.field}</td>
                          <td className="path-cell">{issue.local || '-'}</td>
                          <td className="path-cell">{issue.emby || '-'}</td>
                          <td className="path-cell">{issue.detail || '-'}</td>
                        </tr>
                      )) : <tr><td colSpan={6} className="empty-cell">未发现 Emby 与本地差异。</td></tr>}
                    </tbody>
                  </table>
                </div>
              </Card>

              {embyAuditReport.warnings?.length ? (
                <Card title="警告">
                  <div className="audit-warning-list">
                    {embyAuditReport.warnings.map((warning) => <p key={warning}>{warning}</p>)}
                  </div>
                </Card>
              ) : null}
            </>
          )}

          {auditTab === 'missing' && missingAuditReport && (
            <>
              <Card title="剧集缺漏摘要">
                <div className="audit-summary-grid">
                  <div className="audit-stat"><span>剧集</span><strong>{missingAuditReport.showTitle || '-'}</strong><small>{missingAuditReport.tmdbShowId ? `TMDB #${missingAuditReport.tmdbShowId}` : '未识别 TMDB ID'}</small></div>
                  <div className="audit-stat"><span>本地单集</span><strong>{missingAuditReport.localEpisodes.length}</strong><small>{missingAuditReport.root}</small></div>
                  <div className="audit-stat"><span>缺失集数</span><strong>{missingAuditReport.seasonReports.reduce((sum, season) => sum + (season.missingEpisodes?.length ?? 0), 0)}</strong><small>{missingAuditReport.seasonReports.length} 个季度</small></div>
                  <div className="audit-stat"><span>产物异常对象</span><strong>{groupArtifactIssues(missingAuditReport.artifactIssues).length}</strong><small>{missingAuditReport.artifactIssues?.length ? '需要补齐' : '未发现'}</small></div>
                </div>
              </Card>

              <Card title="季度缺漏">
                <div className="task-table-wrap">
                  <table className="task-table audit-table">
                    <thead>
                      <tr>
                        <th>季度</th>
                        <th>已有</th>
                        <th>期望</th>
                        <th>来源</th>
                        <th>缺失</th>
                        <th>说明</th>
                      </tr>
                    </thead>
                    <tbody>
                      {missingAuditReport.seasonReports.some((season) => season.missingEpisodes?.length) ? missingAuditReport.seasonReports.filter((season) => season.missingEpisodes?.length).map((season) => (
                        <tr key={season.season}>
                          <td>S{String(season.season).padStart(2, '0')}</td>
                          <td>{formatEpisodeList(season.existingEpisodes)}</td>
                          <td>{season.expectedEpisodes?.length ? formatEpisodeList(season.expectedEpisodes) : season.expectedCount || '未知'}</td>
                          <td>{season.expectedSource || '-'}</td>
                          <td><span className={season.missingEpisodes?.length ? 'pill bad' : 'pill ok'}>{season.missingEpisodes?.length ? formatEpisodeList(season.missingEpisodes) : '无'}</span></td>
                          <td className="path-cell">{season.note || '-'}</td>
                        </tr>
                      )) : <tr><td colSpan={6} className="empty-cell">未发现季度缺漏。</td></tr>}
                    </tbody>
                  </table>
                </div>
              </Card>

              <Card title="异常明细">
                <p className="muted">只显示存在产物缺失的视频文件或目录。同一集存在多个视频版本时，每个版本分别核对并显示。</p>
                <div className="task-table-wrap">
                  <table className="task-table audit-table">
                    <thead>
                      <tr>
                        <th>对象</th>
                        <th>文件或目录</th>
                        <th>缺失产物</th>
                      </tr>
                    </thead>
                    <tbody>
                      {missingAuditReport.artifactIssues?.length ? groupArtifactIssues(missingAuditReport.artifactIssues).map((group) => (
                        <tr key={group.path}>
                          <td>{group.target}</td>
                          <td className="path-cell">{group.path}</td>
                          <td>{group.fields.join('、')}</td>
                        </tr>
                      )) : <tr><td colSpan={3} className="empty-cell">未发现产物缺失。</td></tr>}
                    </tbody>
                  </table>
                </div>
              </Card>

              {missingAuditReport.warnings?.length ? (
                <Card title="警告">
                  <div className="audit-warning-list">
                    {missingAuditReport.warnings.map((warning) => <p key={warning}>{warning}</p>)}
                  </div>
                </Card>
              ) : null}
            </>
          )}
        </section>
      )}

        {activePage === 'uploads' && (
        <section className="page-grid uploads-page-grid">
          <Card title="上传概览" action={<button className="secondary" type="button" onClick={() => void refreshUploads()}>刷新</button>}>
            <div className="upload-summary-grid" aria-label="上传概览">
              <DashboardMetric label="合并中" value={String(uploadSummary.collecting)} tone={uploadSummary.collecting ? 'warn' : 'neutral'} />
              <DashboardMetric label="等待上传" value={String(uploadSummary.pending)} tone={uploadSummary.pending ? 'warn' : 'neutral'} />
              <DashboardMetric label="上传中" value={String(uploadSummary.running)} tone={uploadSummary.running ? 'warn' : 'neutral'} />
              <DashboardMetric label="已完成" value={String(uploadSummary.completed)} tone="good" />
              <DashboardMetric label="失败/部分失败" value={String(uploadSummary.failed)} tone={uploadSummary.failed ? 'bad' : 'good'} />
            </div>
          </Card>

          <Card title="上传目标" action={<button type="button" onClick={() => setNewUploadProviderOpen(true)}>添加目标</button>}>
            <div className="task-table-wrap">
              <table className="task-table upload-provider-table">
                <thead><tr><th>名称</th><th>类型</th><th>远端根目录</th><th>授权</th><th>状态</th><th>操作</th></tr></thead>
                <tbody>
                  {uploadProviders.length ? uploadProviders.map((provider) => (
                    <tr key={provider.id}>
                      <td><strong>{provider.name}</strong></td>
                      <td>{provider.type}</td>
                      <td className="path-cell">{provider.remoteRoot}</td>
                      <td>{provider.type === '115cookie' ? <span className={provider.hasCookie ? 'pill ok' : 'pill warn'}>{provider.hasCookie ? 'Cookie 已配置' : '未授权'}</span> : '-'}</td>
                      <td><span className={provider.enabled ? 'pill ok' : 'pill ignored'}>{provider.enabled ? '启用' : '停用'}</span></td>
                      <td><div className="inline-actions"><button className="secondary" type="button" onClick={() => setUploadProviderModal(provider)}>编辑</button>{provider.type === '115cookie' && <button className="secondary" type="button" onClick={() => { setUploadCookieProvider(provider); setUploadCookieValue(''); setCookieAuth(null); }}>授权</button>}<button className="secondary" type="button" disabled={checkingUploadProviderID === provider.id || !provider.hasCookie} onClick={() => void checkUploadProvider(provider)}>{checkingUploadProviderID === provider.id ? '检查中' : '检查'}</button><button className="danger" type="button" onClick={() => void deleteUploadProvider(provider)}>删除</button></div></td>
                    </tr>
                  )) : <tr><td colSpan={6} className="empty-cell">尚未配置上传目标。</td></tr>}
                </tbody>
              </table>
            </div>
          </Card>

          <Card title="上传批次" action={<button className="secondary" type="button" onClick={() => void refreshUploads(1)}>刷新队列</button>}>
            <div className="task-status-tabs" role="group" aria-label="上传批次状态过滤">
              {uploadStatusFilters.map((status) => <button className={uploadStatusFilter === status.value ? 'status-tab active' : 'status-tab'} type="button" key={status.value} aria-pressed={uploadStatusFilter === status.value} onClick={() => { setUploadStatusFilter(status.value); void refreshUploads(1, status.value); }}>{status.label}</button>)}
            </div>
            <div className="task-filters upload-filters">
              <label>番剧路径<input value={uploadPathFilter} onChange={(event) => setUploadPathFilter(event.target.value)} placeholder="输入番剧目录关键字" /></label>
              <div className="filter-actions"><button type="button" onClick={() => void refreshUploads(1)}>过滤</button><button className="secondary" type="button" onClick={() => { setUploadPathFilter(''); setUploadStatusFilter('all'); void refreshUploads(1, 'all'); }}>重置</button></div>
            </div>
            <div className="task-table-wrap">
              <table className="task-table">
                <thead><tr><th>ID</th><th>状态</th><th>番剧目录</th><th>文件</th><th>目标</th><th>完成</th><th>失败</th><th>可上传时间</th><th>操作</th></tr></thead>
                <tbody>
                  {uploadBatches.length ? uploadBatches.map((batch) => (
                    <tr key={batch.id} onClick={() => void loadUploadBatchDetail(batch.id)}>
                      <td>#{batch.id}</td>
                      <td><span className={uploadStatusPillClass(batch.status)}>{batch.status}</span></td>
                      <td className="path-cell">{batch.seriesPath}</td>
                      <td>{batch.fileCount}</td>
                      <td>{batch.targetCount}</td>
                      <td>{batch.completedTargets}</td>
                      <td>{batch.failedTargets}</td>
                      <td>{formatStoredTime(batch.readyAt, displayTimezone)}</td>
                      <td><button className="secondary" type="button" onClick={(event) => { event.stopPropagation(); void loadUploadBatchDetail(batch.id); }}>详情</button></td>
                    </tr>
                  )) : <tr><td colSpan={9} className="empty-cell">暂无上传批次。</td></tr>}
                </tbody>
              </table>
            </div>
            <div className="pagination-bar">
              <span>共 {uploadTotal} 条，第 {uploadPage} / {Math.max(1, Math.ceil(uploadTotal / taskPageSize))} 页</span>
              <div className="inline-actions"><button className="secondary" type="button" disabled={uploadPage <= 1} onClick={() => void refreshUploads(uploadPage - 1)}>上一页</button><button className="secondary" type="button" disabled={uploadPage >= Math.ceil(uploadTotal / taskPageSize)} onClick={() => void refreshUploads(uploadPage + 1)}>下一页</button></div>
            </div>
          </Card>
        </section>
      )}

        {activePage === 'tasks' && (
        <section className="page-grid task-page-grid">
          <Card title="任务列表" action={<div className="inline-actions"><button className="secondary" type="button" onClick={() => setRecentArtifactsOpen(true)}>最近产物</button><button className="secondary" onClick={() => void retrySelectedTasks()} disabled={retryingTasks || selectedTaskIds.length === 0}>{retryingTasks ? '重试中' : `重试选中${selectedTaskIds.length ? `(${selectedTaskIds.length})` : ''}`}</button><button className="secondary" onClick={() => void ignoreSelectedTasks()} disabled={ignoringTasks || selectedTaskIds.length === 0}>{ignoringTasks ? '忽略中' : `忽略失败${selectedTaskIds.length ? `(${selectedTaskIds.length})` : ''}`}</button><button className="danger" onClick={cancelActiveTasks} disabled={cancelingTasks}>{cancelingTasks ? '取消中' : '取消待执行/执行中'}</button></div>}>
            <div className="task-status-tabs" role="group" aria-label="任务状态过滤">
              {taskStatusFilters.map((status) => (
                <button className={taskStatusFilter === status.value ? 'status-tab active' : 'status-tab'} type="button" key={status.value} aria-pressed={taskStatusFilter === status.value} onClick={() => selectTaskStatusFilter(status.value)}>
                  {status.label}
                </button>
              ))}
            </div>
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
                    <th><input type="checkbox" aria-label="选择当前页任务" checked={tasks.length > 0 && tasks.every((task) => selectedTaskIds.includes(task.id))} onChange={(event) => setSelectedTaskIds(event.target.checked ? tasks.map((task) => task.id) : [])} /></th>
                    <th>ID</th>
                    <th>状态</th>
                    <th>类型</th>
                    <th>路径</th>
                    <th>创建时间</th>
                    <th>错误</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {tasks.length ? tasks.map((task, index) => (
                    <tr key={task.id} className={selectedTaskIds.includes(task.id) ? 'selected interactive-row' : 'interactive-row'} tabIndex={0} aria-selected={selectedTaskIds.includes(task.id)} onClick={(event) => handleTaskRowClick(event, task, index)} onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); void loadTaskDetail(task.id); } else if (event.key === ' ') { event.preventDefault(); toggleTaskSelection(task.id, !selectedTaskIds.includes(task.id), event.shiftKey); } }}>
                      <td><input type="checkbox" aria-label={`选择任务 ${task.id}`} checked={selectedTaskIds.includes(task.id)} onChange={(event) => toggleTaskSelection(task.id, event.target.checked, (event.nativeEvent as MouseEvent).shiftKey)} /></td>
                      <td>#{task.id}</td>
                      <td><span className={taskStatusPillClass(task.status)}>{task.status}</span></td>
                      <td>{task.type}</td>
                      <td className="path-cell">{task.mediaPath || '-'}</td>
                      <td>{formatStoredTime(task.createdAt, displayTimezone)}</td>
                      <td className="path-cell">{task.errorSummary || '-'}</td>
                      <td><div className="table-actions"><button className="icon-button secondary" type="button" title="在文件管理器中显示" aria-label={`在文件管理器中显示任务 ${task.id}`} disabled={!task.mediaPath} onClick={(event) => { event.stopPropagation(); void revealPath(task.mediaPath); }}><FolderOpen size={16} /></button><button className="secondary" type="button" onClick={(event) => { event.stopPropagation(); void loadTaskDetail(task.id); }}>详情</button></div></td>
                    </tr>
                  )) : (
                    <tr><td colSpan={8} className="empty-cell">暂无任务。</td></tr>
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

          {recentArtifactsOpen && <RecentArtifactsModal artifacts={artifacts} timezone={displayTimezone} onClose={() => setRecentArtifactsOpen(false)} />}
          {selectedTask && <TaskDetailModal detail={selectedTask} timezone={displayTimezone} onClose={() => setSelectedTask(null)} />}
        </section>
      )}
        </div>
      {(newUploadProviderOpen || uploadProviderModal) && <UploadProviderModal provider={uploadProviderModal ?? undefined} providerTypes={uploadProviderTypes} watchDirs={watchDirs} saving={savingUploadProvider} onClose={() => { setNewUploadProviderOpen(false); setUploadProviderModal(null); }} onSubmit={(provider) => void saveUploadProvider(provider)} />}
      {uploadCookieProvider && <UploadCookieModal provider={uploadCookieProvider} cookie={uploadCookieValue} auth={cookieAuth} saving={savingUploadProvider} onCookieChange={setUploadCookieValue} onClose={() => { setUploadCookieProvider(null); setCookieAuth(null); setUploadCookieValue(''); }} onSave={() => void saveUploadCookie()} onStartAuth={() => void startCookieAuth()} />}
      {selectedUploadBatch && <UploadBatchDetailModal detail={selectedUploadBatch} timezone={displayTimezone} actionTargetID={uploadTargetActionID} onClose={() => setSelectedUploadBatch(null)} onRetry={(target) => void actOnUploadTarget(target, 'retry')} onCancel={(target) => void actOnUploadTarget(target, 'cancel')} />}
      {rescanOpen && <RescanModal scope={rescanScope} target={rescanTarget} watchDirId={rescanWatchDirId} useCustomProcessing={rescanUseCustomProcessing} processing={rescanProcessing} directories={watchDirs} rescanning={rescanning} onClose={() => setRescanOpen(false)} onScopeChange={(value) => { setRescanScope(value); setRescanTarget(''); setRescanWatchDirId(''); }} onTargetChange={setRescanTarget} onWatchDirIdChange={(value) => { setRescanWatchDirId(value); setRescanTarget(''); }} onUseCustomProcessingChange={(value) => { setRescanUseCustomProcessing(value); if (value) setRescanProcessing(outputProcessingFromConfig(config)); }} onProcessingChange={(patch) => setRescanProcessing((value) => ({ ...value, ...patch }))} onBrowsePath={() => { const rootPath = rescanScope === 'dir' ? watchDirs.find((dir) => String(dir.id) === rescanWatchDirId)?.path ?? '' : ''; void browseDirectory({ title: '选择扫描路径', value: rescanTarget || rootPath, rootPath: rootPath || undefined, onSelect: setRescanTarget }); }} onSubmit={() => void rescan()} />}
      {addWatchDirOpen && <WatchDirModal title="添加媒体目录" submitLabel="添加" path={newWatchDir} watchEnabled={newWatchDirWatchEnabled} useGlobalProcessing={newWatchDirUseGlobalProcessing} processing={newWatchDirProcessing} onPathChange={setNewWatchDir} onWatchEnabledChange={setNewWatchDirWatchEnabled} onUseGlobalProcessingChange={(value) => { setNewWatchDirUseGlobalProcessing(value); if (!value) setNewWatchDirProcessing(outputProcessingFromConfig(config)); }} onProcessingChange={(patch) => setNewWatchDirProcessing((value) => ({ ...value, ...patch }))} onClose={() => setAddWatchDirOpen(false)} onBrowsePath={() => void browseDirectory({ title: '选择媒体目录', value: newWatchDir, onSelect: setNewWatchDir })} onSubmit={() => void addWatchDir()} />}
      {editingWatchDir && <WatchDirModal title="编辑媒体目录" submitLabel="保存" path={editingWatchDirPath} watchEnabled={editingWatchDirWatchEnabled} useGlobalProcessing={editingWatchDirUseGlobalProcessing} processing={editingWatchDirProcessing} onPathChange={setEditingWatchDirPath} onWatchEnabledChange={setEditingWatchDirWatchEnabled} onUseGlobalProcessingChange={(value) => { setEditingWatchDirUseGlobalProcessing(value); if (!value && editingWatchDirUseGlobalProcessing) setEditingWatchDirProcessing(outputProcessingFromConfig(config)); }} onProcessingChange={(patch) => setEditingWatchDirProcessing((value) => ({ ...value, ...patch }))} onClose={() => setEditingWatchDir(null)} onBrowsePath={() => void browseDirectory({ title: '选择媒体目录', value: editingWatchDirPath, onSelect: setEditingWatchDirPath })} onSubmit={() => void submitEditWatchDir()} />}
      {batchEpisodeOpen && <BatchEpisodeModal count={selectedRenamePaths.length} season={batchSeason} mode={batchEpisodeMode} offset={batchEpisodeOffset} start={batchEpisodeStart} applying={applyingBatchEpisode} progress={batchEpisodeProgress} onClose={() => setBatchEpisodeOpen(false)} onSeasonChange={setBatchSeason} onModeChange={setBatchEpisodeMode} onOffsetChange={setBatchEpisodeOffset} onStartChange={setBatchEpisodeStart} onSubmit={() => void applyBatchEpisodeFix()} />}
      {tmdbMatchOpen && <TmdbMatchModal count={selectedRenamePaths.length} query={tmdbQuery} results={tmdbResults} searching={searchingTmdb} applyingShowId={applyingTmdbShowId} applyProgress={tmdbApplyProgress} applyTotal={tmdbApplyTotal} onQueryChange={setTmdbQuery} onSearch={() => void searchTmdbShows()} onApply={(show) => void applyTmdbShowToSelected(show)} onClose={() => setTmdbMatchOpen(false)} />}
      {auditTmdbMatchOpen && <TmdbMatchModal title="选择核对剧集" description="选择后会将 TMDB ID 用于剧集缺漏判断。" applyLabel="选择剧集" query={tmdbQuery} results={tmdbResults} searching={searchingTmdb} applyingShowId={null} applyProgress={0} applyTotal={0} onQueryChange={setTmdbQuery} onSearch={() => void searchTmdbShows()} onApply={applyTmdbShowToAudit} onClose={() => setAuditTmdbMatchOpen(false)} />}
      {addEmbyKeyOpen && <AddEmbyKeyModal title={newEmbyKeyTitle} apiKey={newEmbyKeyValue} saving={savingEmbyKey} onTitleChange={setNewEmbyKeyTitle} onAPIKeyChange={setNewEmbyKeyValue} onClose={() => setAddEmbyKeyOpen(false)} onSubmit={() => void saveEmbyAPIKey()} />}
      {tmdbEpisodeDetail && <TmdbEpisodeDetailModal detail={tmdbEpisodeDetail} language={renameLanguage} refreshing={loadingTmdbEpisodeDetail} onRefresh={() => void openTmdbEpisodeDetail({ tmdbShowId: tmdbEpisodeDetail.showId, season: tmdbEpisodeDetail.season, episode: tmdbEpisodeDetail.episode } as RenamePreviewItem, true)} onClose={() => setTmdbEpisodeDetail(null)} />}
      {renameHistoryOpen && <RenameHistoryModal history={renameHistory} undoingId={undoingHistoryId} loading={loadingRenameHistory} timezone={displayTimezone} onClose={() => setRenameHistoryOpen(false)} onRefresh={() => void loadRenameHistory()} onOpenDetails={setSelectedHistoryBatch} onUndo={(id) => void undoRenameBatch(id)} />}
      {selectedHistoryBatch && <RenameHistoryDetailsModal batch={selectedHistoryBatch} undoCheck={undoCheckResult?.batch?.id === selectedHistoryBatch.id ? undoCheckResult : null} timezone={displayTimezone} onClose={() => setSelectedHistoryBatch(null)} />}
      {renameTemplateEditorOpen && <RenameTemplateEditorModal value={renameTemplate} matchPattern={renameMatchPattern} sample={renamePreview[0]?.currentName || renamePath} placeholders={renamePlaceholders} onChange={setRenameTemplate} onMatchPatternChange={setRenameMatchPattern} onClose={() => setRenameTemplateEditorOpen(false)} />}
      {targetPathEditor && <TargetPathEditorModal value={targetPathEditor.value} onChange={(value) => setTargetPathEditor({ ...targetPathEditor, value })} onClose={() => setTargetPathEditor(null)} onSubmit={applyTargetPathEdit} />}
      {directoryPicker && <DirectoryPicker title={directoryPicker.title} initialPath={directoryPicker.value} rootPath={directoryPicker.rootPath} onClose={() => setDirectoryPicker(null)} onSelect={(path) => { directoryPicker.onSelect(path); setDirectoryPicker(null); }} />}
      {confirmation && <ConfirmDialog request={confirmation} pending={confirming} onCancel={() => setConfirmation(null)} onConfirm={() => void acceptConfirmation()} />}
      {error && <AlertDialog title="操作失败" message={error} onClose={() => setError('')} />}
      </section>
    </main>
  );
}

function newUploadProviderDraft(): UploadProvider {
  return {
    id: 0,
    name: '',
    type: '115cookie',
    enabled: false,
    remoteRoot: '/',
    userAgent: '',
    collisionPolicy: 'fail',
    hasCookie: false,
    routes: [],
    createdAt: '',
    updatedAt: ''
  };
}

function uploadProviderDraft(provider?: UploadProvider): UploadProvider {
  if (!provider) return newUploadProviderDraft();
  const globalRoute = provider.routes.find((route) => route.watchDirId == null);
  if (!globalRoute) return provider;
  return {
    ...provider,
    remoteRoot: globalRoute.remoteRoot || provider.remoteRoot,
    collisionPolicy: globalRoute.collisionPolicy || provider.collisionPolicy
  };
}

function newUploadRoute(draft: UploadProvider, watchDirId?: number): UploadProviderRoute {
  return {
    watchDirId,
    enabled: true,
    remoteRoot: draft.remoteRoot || '/',
    collisionPolicy: draft.collisionPolicy || 'fail',
    includeTypes: []
  };
}

function nextUploadTypeSelection(current: string[], value: string, checked: boolean) {
  const types = new Set(current);
  if (checked) types.add(value);
  else types.delete(value);
  return Array.from(types);
}

function UploadRouteProfile(props: { route: UploadProviderRoute; includeDestination?: boolean; onChange: (patch: Partial<UploadProviderRoute>) => void }) {
  return (
    <div className="upload-route-profile">
      {props.includeDestination !== false && <><label>远端根目录<input value={props.route.remoteRoot} onChange={(event) => props.onChange({ remoteRoot: event.target.value })} placeholder="/Anime" required /></label><label>碰撞策略<select value={props.route.collisionPolicy} onChange={(event) => props.onChange({ collisionPolicy: event.target.value as UploadProvider['collisionPolicy'] })}><option value="replace">替换同名不同大小文件</option><option value="skip">跳过同名不同大小文件</option><option value="fail">作为冲突失败</option></select></label></>}
      <fieldset className="upload-route-type-fieldset">
        <legend>文件类型覆盖</legend>
        <div className="upload-type-grid">
          {uploadTypeOptions.map((option) => <label className="checkbox-label" key={option.value}><input type="checkbox" checked={(props.route.includeTypes ?? []).includes(option.value)} onChange={(event) => props.onChange({ includeTypes: nextUploadTypeSelection(props.route.includeTypes ?? [], option.value, event.target.checked) })} />{option.label}</label>)}
        </div>
        <small>不选择时沿用“上传调度”中的默认文件类型。</small>
      </fieldset>
    </div>
  );
}

function UploadProviderModal(props: { provider?: UploadProvider; providerTypes: UploadProviderDescriptor[]; watchDirs: WatchDir[]; saving: boolean; onClose: () => void; onSubmit: (provider: UploadProvider) => void }) {
  const [draft, setDraft] = useState<UploadProvider>(() => uploadProviderDraft(props.provider));
  const initialGlobalRoute = draft.routes.find((route) => route.watchDirId == null);
  const [hasGlobalRoute, setHasGlobalRoute] = useState(() => initialGlobalRoute ? initialGlobalRoute.enabled : draft.routes.length === 0);
  const editing = draft.id > 0;
  const globalRoute = draft.routes.find((route) => route.watchDirId == null);
  const scopedRoutes = draft.routes.filter((route) => route.watchDirId != null);
  const selectedWatchDirIDs = new Set(scopedRoutes.flatMap((route) => route.watchDirId == null ? [] : [route.watchDirId]));
  const descriptors = props.providerTypes.length ? props.providerTypes : [{ type: '115cookie', name: '115 Cookie', implemented: true, secretKeys: ['cookie'] }];
  const providerTypes = descriptors.some((descriptor) => descriptor.type === draft.type) ? descriptors : [...descriptors, { type: draft.type, name: draft.type, implemented: false, secretKeys: [] }];
  const selectedProviderType = providerTypes.find((descriptor) => descriptor.type === draft.type);
  const providerTypeUsable = !selectedProviderType || selectedProviderType.implemented || !draft.enabled;
  const routeScopeValid = hasGlobalRoute || selectedWatchDirIDs.size > 0;

  function setGlobalRouteEnabled(enabled: boolean) {
    setHasGlobalRoute(enabled);
    if (!enabled) return;
    setDraft((current) => current.routes.some((route) => route.watchDirId == null) ? current : { ...current, routes: [newUploadRoute(current), ...current.routes] });
  }

  function toggleWatchDir(id: number) {
    setDraft((current) => {
      const currentRoute = current.routes.find((route) => route.watchDirId === id);
      if (currentRoute) return { ...current, routes: current.routes.filter((route) => route.watchDirId !== id) };
      return { ...current, routes: [...current.routes, newUploadRoute(current, id)] };
    });
  }

  function updateScopedRoute(watchDirID: number, patch: Partial<UploadProviderRoute>) {
    setDraft((current) => ({ ...current, routes: current.routes.map((route) => route.watchDirId === watchDirID ? { ...route, ...patch } : route) }));
  }

  function updateGlobalRoute(patch: Partial<UploadProviderRoute>) {
    setDraft((current) => {
      const currentGlobal = current.routes.find((route) => route.watchDirId == null);
      const nextGlobal = { ...(currentGlobal ?? newUploadRoute(current)), ...patch, watchDirId: undefined };
      return { ...current, routes: currentGlobal ? current.routes.map((route) => route.watchDirId == null ? nextGlobal : route) : [nextGlobal, ...current.routes] };
    });
  }

  function routesForSubmit(): UploadProviderRoute[] {
    const scoped = draft.routes.filter((route) => route.watchDirId != null).map((route) => ({ ...route, enabled: true }));
    return [{ ...(globalRoute ?? newUploadRoute(draft)), watchDirId: undefined, enabled: hasGlobalRoute, remoteRoot: draft.remoteRoot, collisionPolicy: draft.collisionPolicy }, ...scoped];
  }

  const canSubmit = !props.saving && !!draft.name.trim() && !!draft.remoteRoot.trim() && routeScopeValid && providerTypeUsable;

  return (
    <div className="modal-backdrop" role="presentation" onClick={props.saving ? undefined : props.onClose}>
      <section className="modal-card upload-provider-modal" role="dialog" aria-modal="true" aria-labelledby="upload-provider-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <div><h2 id="upload-provider-title">{editing ? '编辑上传目标' : '添加上传目标'}</h2><small>账号授权、目录路由和上传批次独立保存，可并行配置多个网盘。</small></div>
          <IconCloseButton onClick={props.onClose} disabled={props.saving} />
        </div>
        <form className="config-form" onSubmit={(event) => { event.preventDefault(); if (canSubmit) props.onSubmit({ ...draft, routes: routesForSubmit() }); }}>
          <label>显示名称<input autoFocus value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} placeholder="例如：115 主归档" required /></label>
          <label>Provider 类型<select value={draft.type} onChange={(event) => setDraft({ ...draft, type: event.target.value })}>{providerTypes.map((providerType) => <option key={providerType.type} value={providerType.type} disabled={!providerType.implemented && providerType.type !== draft.type}>{providerType.name}{providerType.implemented ? '' : '（尚未安装）'}</option>)}</select><small>{selectedProviderType?.implemented ? '已安装的 Provider 可立即配置授权。' : '此 Provider 类型已预留，但尚未安装上传实现。'}</small></label>
          <label>{hasGlobalRoute ? '默认远端根目录' : '新目录默认远端根目录'}<input value={draft.remoteRoot} onChange={(event) => setDraft({ ...draft, remoteRoot: event.target.value })} placeholder="/Anime" required /></label>
          <label>{hasGlobalRoute ? '默认碰撞策略' : '新目录默认碰撞策略'}<select value={draft.collisionPolicy} onChange={(event) => setDraft({ ...draft, collisionPolicy: event.target.value as UploadProvider['collisionPolicy'] })}><option value="replace">替换同名不同大小文件</option><option value="skip">跳过同名不同大小文件</option><option value="fail">作为冲突失败</option></select></label>
          <label>自定义 User-Agent（可选）<input value={draft.userAgent} onChange={(event) => setDraft({ ...draft, userAgent: event.target.value })} placeholder="Mozilla/5.0" /></label>
          <fieldset className="upload-route-fieldset">
            <legend>适用媒体目录</legend>
            <Toggle label="默认应用到所有监控目录" checked={hasGlobalRoute} onChange={setGlobalRouteEnabled} />
            <small>{hasGlobalRoute ? '下面选中的目录会覆盖默认路由；未选中的目录使用默认路由。' : '关闭默认路由后，至少选择一个目录；只有选中的目录会创建上传任务。'}</small>
            {hasGlobalRoute && <details className="upload-route-details"><summary>默认路由的文件类型覆盖</summary><UploadRouteProfile route={{ ...(globalRoute ?? newUploadRoute(draft)), remoteRoot: draft.remoteRoot, collisionPolicy: draft.collisionPolicy }} includeDestination={false} onChange={updateGlobalRoute} /></details>}
            <div className="upload-watch-dir-list">
              {props.watchDirs.length ? props.watchDirs.map((watchDir) => {
                const selected = selectedWatchDirIDs.has(watchDir.id);
                const route = scopedRoutes.find((item) => item.watchDirId === watchDir.id);
                return <div className="upload-watch-dir-item" key={watchDir.id}><label className="checkbox-label"><input type="checkbox" checked={selected} onChange={() => toggleWatchDir(watchDir.id)} />{watchDir.path}</label>{selected && route && <details className="upload-route-details" open><summary>{hasGlobalRoute ? '覆盖此目录的默认路由' : '此目录的上传路由'}</summary><UploadRouteProfile route={route} onChange={(patch) => updateScopedRoute(watchDir.id, patch)} /></details>}</div>;
              }) : <small>尚未配置监控目录。</small>}
            </div>
            {!routeScopeValid && <small className="upload-selection-warning">请至少选择一个媒体目录，或启用默认路由。</small>}
          </fieldset>
          <Toggle label="为新上传批次启用此目标" checked={draft.enabled} onChange={(enabled) => setDraft({ ...draft, enabled })} />
          <div className="inline-actions modal-actions"><button className="secondary" type="button" onClick={props.onClose} disabled={props.saving}>取消</button><button type="submit" disabled={!canSubmit}>{props.saving ? '保存中' : '保存目标'}</button></div>
        </form>
      </section>
    </div>
  );
}

function UploadCookieModal(props: { provider: UploadProvider; cookie: string; auth: CookieAuthStatus | null; saving: boolean; onCookieChange: (value: string) => void; onClose: () => void; onSave: () => void; onStartAuth: () => void }) {
  const qrURL = props.auth ? `/api/upload/providers/${props.provider.id}/auth/115cookie/${encodeURIComponent(props.auth.sessionId)}/qrcode` : '';
  const terminal = props.auth && ['authorized', 'expired', 'cancelled', 'error'].includes(props.auth.state);
  return (
    <div className="modal-backdrop" role="presentation" onClick={props.saving ? undefined : props.onClose}>
      <section className="modal-card upload-cookie-modal" role="dialog" aria-modal="true" aria-labelledby="upload-cookie-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header"><div><h2 id="upload-cookie-title">115 Cookie 授权</h2><small>{props.provider.name}</small></div><IconCloseButton onClick={props.onClose} disabled={props.saving} /></div>
        <div className="upload-auth-grid">
          <section className="upload-auth-panel">
            <h3>粘贴 Cookie</h3>
            <textarea value={props.cookie} onChange={(event) => props.onCookieChange(event.target.value)} placeholder="粘贴 115 Cookie" rows={7} />
            <button type="button" disabled={props.saving || !props.cookie.trim()} onClick={props.onSave}>{props.saving ? '保存中' : '保存 Cookie'}</button>
          </section>
          <section className="upload-auth-panel">
            <h3>二维码授权</h3>
            {props.auth ? <><img className="upload-auth-qr" src={qrURL} alt="115 登录二维码" /><p className="settings-note">{props.auth.message || props.auth.state}</p>{terminal ? <button type="button" className="secondary" onClick={props.onStartAuth}>重新获取二维码</button> : <span className="pill running">{props.auth.state}</span>}</> : <button type="button" onClick={props.onStartAuth} disabled={props.saving}>获取登录二维码</button>}
          </section>
        </div>
      </section>
    </div>
  );
}

function UploadBatchDetailModal(props: { detail: UploadBatchDetail; timezone: string; actionTargetID: number | null; onClose: () => void; onRetry: (target: UploadBatchTarget) => void; onCancel: (target: UploadBatchTarget) => void }) {
  return (
    <div className="modal-backdrop" role="presentation" onClick={props.onClose}>
      <section className="modal-card upload-batch-detail-modal" role="dialog" aria-modal="true" aria-labelledby="upload-batch-detail-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header"><div><h2 id="upload-batch-detail-title">上传批次 #{props.detail.batch.id}</h2><small>{props.detail.batch.seriesPath}</small></div><IconCloseButton onClick={props.onClose} /></div>
        <div className="upload-detail-summary">
          <Row label="状态" value={props.detail.batch.status} />
          <Row label="可上传时间" value={formatStoredTime(props.detail.batch.readyAt, props.timezone)} />
          <Row label="文件 / 目标" value={`${props.detail.files.length} / ${props.detail.targets.length}`} />
        </div>
        <section className="upload-detail-section"><h3>目标</h3><div className="task-table-wrap"><table className="task-table"><thead><tr><th>目标</th><th>状态</th><th>尝试</th><th>错误</th><th>操作</th></tr></thead><tbody>{props.detail.targets.map((target) => <tr key={target.id}><td><strong>{target.providerName}</strong><small>{target.remoteRoot}</small></td><td><span className={uploadStatusPillClass(target.status)}>{target.status}</span></td><td>{target.attempts}</td><td className="path-cell">{target.errorSummary || '-'}</td><td><div className="inline-actions">{['failed', 'canceled'].includes(target.status) && <button className="secondary" type="button" disabled={props.actionTargetID === target.id} onClick={() => props.onRetry(target)}>{props.actionTargetID === target.id ? '处理中' : '重试'}</button>}{['waiting', 'pending'].includes(target.status) && <button className="danger" type="button" disabled={props.actionTargetID === target.id} onClick={() => props.onCancel(target)}>{props.actionTargetID === target.id ? '处理中' : '取消'}</button>}</div></td></tr>)}</tbody></table></div></section>
        <section className="upload-detail-section"><h3>文件</h3><div className="task-table-wrap"><table className="task-table"><thead><tr><th>相对路径</th><th>类型</th><th>大小</th><th>传输状态</th></tr></thead><tbody>{props.detail.files.map((file) => { const transfers = props.detail.transfers.filter((transfer) => transfer.batchFileId === file.id); return <tr key={file.id}><td className="path-cell">{file.relativePath}</td><td>{file.fileType}</td><td>{formatUploadBytes(file.size)}</td><td>{transfers.length ? transfers.map((transfer) => <span className={uploadStatusPillClass(transfer.status)} key={transfer.id}>{transfer.status}</span>) : '-'}</td></tr>; })}</tbody></table></div></section>
      </section>
    </div>
  );
}

function formatUploadBytes(value: number) {
  if (!Number.isFinite(value) || value < 0) return '-';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let index = 0;
  let result = value;
  while (result >= 1024 && index < units.length - 1) {
    result /= 1024;
    index++;
  }
  return `${result >= 10 || index === 0 ? result.toFixed(0) : result.toFixed(1)} ${units[index]}`;
}

function ConfirmDialog(props: { request: ConfirmationRequest; pending: boolean; onCancel: () => void; onConfirm: () => void }) {
  const dialogRef = useRef<HTMLElement>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const onCancelRef = useRef(props.onCancel);
  const pendingRef = useRef(props.pending);
  onCancelRef.current = props.onCancel;
  pendingRef.current = props.pending;

  useEffect(() => {
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    cancelRef.current?.focus();

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape' && !pendingRef.current) {
        event.preventDefault();
        onCancelRef.current();
        return;
      }
      if (event.key !== 'Tab' || !dialogRef.current) return;
      const focusable = Array.from(dialogRef.current.querySelectorAll<HTMLElement>('button:not(:disabled), [href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])'));
      if (!focusable.length) {
        event.preventDefault();
        dialogRef.current.focus();
        return;
      }
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (!dialogRef.current.contains(document.activeElement)) {
        event.preventDefault();
        (event.shiftKey ? last : first).focus();
      } else if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    }

    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('keydown', handleKeyDown);
      document.body.style.overflow = previousOverflow;
      previousFocus?.focus();
    };
  }, []);

  return (
    <div className="modal-backdrop confirm-backdrop" role="presentation" onMouseDown={props.pending ? undefined : props.onCancel}>
      <section ref={dialogRef} className="modal-card confirm-dialog" role="alertdialog" aria-modal="true" aria-labelledby="confirm-title" aria-describedby="confirm-message" tabIndex={-1} onMouseDown={(event) => event.stopPropagation()}>
        <span className={props.request.tone === 'danger' ? 'confirm-icon danger' : 'confirm-icon'} aria-hidden="true"><AlertTriangle size={21} /></span>
        <div className="confirm-content">
          <h2 id="confirm-title">{props.request.title}</h2>
          <p id="confirm-message">{props.request.message}</p>
        </div>
        <div className="inline-actions modal-actions">
          <button ref={cancelRef} className="secondary" type="button" disabled={props.pending} onClick={props.onCancel}>取消</button>
          <button className={props.request.tone === 'danger' ? 'danger' : ''} type="button" disabled={props.pending} onClick={props.onConfirm}>{props.pending ? '处理中...' : props.request.confirmLabel}</button>
        </div>
      </section>
    </div>
  );
}

function AlertDialog(props: { title: string; message: string; onClose: () => void }) {
  return (
    <section className="error-toast" role="alert" aria-live="assertive">
      <span className="error-toast-icon" aria-hidden="true"><AlertTriangle size={19} /></span>
      <div><strong>{props.title}</strong><span>{props.message}</span></div>
      <button className="icon-button" type="button" aria-label="关闭错误消息" title="关闭" onClick={props.onClose}><X size={16} /></button>
    </section>
  );
}

function AddEmbyKeyModal(props: { title: string; apiKey: string; saving: boolean; onTitleChange: (value: string) => void; onAPIKeyChange: (value: string) => void; onClose: () => void; onSubmit: () => void }) {
  return (
    <div className="modal-backdrop" role="presentation" onClick={props.saving ? undefined : props.onClose}>
      <section className="modal-card add-emby-key-modal" role="dialog" aria-modal="true" aria-labelledby="add-emby-key-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <div>
            <h2 id="add-emby-key-title">添加 API Key</h2>
            <small>保存后可以在 Emby 核对中直接选择使用。</small>
          </div>
          <IconCloseButton onClick={props.onClose} disabled={props.saving} />
        </div>
        <form className="config-form" onSubmit={(event) => { event.preventDefault(); props.onSubmit(); }}>
          <label>Key 标题<input autoFocus value={props.title} onChange={(event) => props.onTitleChange(event.target.value)} placeholder="例如：主 Emby" /></label>
          <label>API Key<input type="password" value={props.apiKey} onChange={(event) => props.onAPIKeyChange(event.target.value)} placeholder="粘贴 Emby API Key" /></label>
          <div className="inline-actions modal-actions">
            <button className="secondary" type="button" onClick={props.onClose} disabled={props.saving}>取消</button>
            <button type="submit" disabled={props.saving || !props.title.trim() || !props.apiKey.trim()}>{props.saving ? '保存中' : '保存 API Key'}</button>
          </div>
        </form>
      </section>
    </div>
  );
}

function IconCloseButton(props: { onClick: () => void; disabled?: boolean }) {
  return (
    <button className="icon-close-button" type="button" onClick={props.onClick} disabled={props.disabled} aria-label="关闭" title="关闭">
      <X size={18} aria-hidden="true" />
    </button>
  );
}

function TabButton(props: { active: boolean; label: string; icon: LucideIcon; badge?: number; badgeTone?: 'default' | 'warn' | 'danger'; onClick: () => void }) {
  const Icon = props.icon;
  return (
    <button className={props.active ? 'tab-button active' : 'tab-button'} type="button" aria-current={props.active ? 'page' : undefined} title={props.label} onClick={props.onClick}>
      <Icon size={18} aria-hidden="true" />
      <span className="tab-button-label">{props.label}</span>
      {props.badge ? <span className={`nav-badge ${props.badgeTone ?? 'default'}`}>{props.badge}</span> : null}
    </button>
  );
}

function ThemeSelector(props: { value: ThemeMode; onChange: (value: ThemeMode) => void }) {
  return (
    <fieldset className="theme-preference">
      <legend>界面主题</legend>
      <div className="theme-options">
        {themeOptions.map((option) => {
          const Icon = option.icon;
          return (
            <label className="theme-option" key={option.value} title={option.label}>
              <input
                type="radio"
                name="theme-mode"
                value={option.value}
                checked={props.value === option.value}
                onChange={() => props.onChange(option.value)}
              />
              <Icon size={16} aria-hidden="true" />
              <span>{option.label}</span>
            </label>
          );
        })}
      </div>
    </fieldset>
  );
}

function InitialLoading() {
  return (
    <section className="initial-loading" aria-live="polite" aria-label="正在加载工作区">
      <span className="loading-spinner" aria-hidden="true" />
      <div><strong>正在启动媒体工作区</strong><span>连接数据库并恢复后台任务...</span></div>
    </section>
  );
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

function DashboardMetric(props: { label: string; value: string; tone?: 'neutral' | 'good' | 'warn' | 'bad' }) {
  return (
    <div className={`dashboard-metric ${props.tone ?? 'neutral'}`}>
      <span>{props.label}</span>
      <strong>{props.value}</strong>
    </div>
  );
}

function DashboardFeature(props: { label: string; enabled?: boolean }) {
  return (
    <div className={props.enabled ? 'dashboard-feature enabled' : 'dashboard-feature'}>
      <span>{props.label}</span>
      <strong>{props.enabled ? '开启' : '关闭'}</strong>
    </div>
  );
}

function SetupRow(props: { icon: LucideIcon; title: string; detail: string; complete: boolean; optional?: boolean; actionLabel?: string; onAction: () => void }) {
  const Icon = props.icon;
  return (
    <div className="setup-row">
      <span className={props.complete ? 'setup-row-icon complete' : 'setup-row-icon'} aria-hidden="true">{props.complete ? <CheckCircle2 size={18} /> : <Icon size={18} />}</span>
      <div><strong>{props.title}</strong><span>{props.detail}</span></div>
      {props.optional && !props.complete ? <span className="setup-optional">可选</span> : null}
      {props.actionLabel ? <button className="secondary" type="button" onClick={props.onAction}>{props.actionLabel}</button> : <span className="setup-complete">已就绪</span>}
    </div>
  );
}

function ArtifactRow(props: { artifact: Artifact; timezone: string }) {
  return <Row label={`${props.artifact.type} · ${formatStoredTime(props.artifact.createdAt, props.timezone)}`} value={props.artifact.path} />;
}

function RecentArtifactsModal(props: { artifacts: Artifact[]; timezone: string; onClose: () => void }) {
  return (
    <div className="modal-backdrop" role="presentation" onClick={props.onClose}>
      <section className="modal-card recent-artifacts-modal" role="dialog" aria-modal="true" aria-labelledby="recent-artifacts-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <h2 id="recent-artifacts-title">最近产物</h2>
          <IconCloseButton onClick={props.onClose} />
        </div>
        {props.artifacts.length ? props.artifacts.map((artifact) => <ArtifactRow key={artifact.id} artifact={artifact} timezone={props.timezone} />) : <p className="muted">暂无产物。</p>}
      </section>
    </div>
  );
}

function TaskDetailModal(props: { detail: TaskDetail; timezone: string; onClose: () => void }) {
  const logs = [...asArray<TaskLog>(props.detail.logs)].reverse();
  return (
    <div className="modal-backdrop" role="presentation" onClick={props.onClose}>
      <section className="modal-card rescan-modal" role="dialog" aria-modal="true" aria-labelledby="task-detail-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <h2 id="task-detail-title">任务详情</h2>
          <IconCloseButton onClick={props.onClose} />
        </div>
        <Row label="任务" value={`${props.detail.task.type} #${props.detail.task.id}`} />
        {props.detail.task.mediaPath && <Row label="文件" value={props.detail.task.mediaPath} />}
        <Row label="处理策略" value={props.detail.task.overwriteExisting ? '强制重建' : '只补缺失'} />
        <Row label="状态" value={props.detail.task.status} />
        <Row label="尝试次数" value={String(props.detail.task.attempts)} />
        <Row label="创建时间" value={formatStoredTime(props.detail.task.createdAt, props.timezone)} />
        {props.detail.task.startedAt && <Row label="开始时间" value={formatStoredTime(props.detail.task.startedAt, props.timezone)} />}
        {props.detail.task.finishedAt && <Row label="结束时间" value={formatStoredTime(props.detail.task.finishedAt, props.timezone)} />}
        {props.detail.task.errorSummary && <Row label="错误" value={props.detail.task.errorSummary} />}
        <h3>日志</h3>
        {logs.length ? logs.map((log) => (
          <div className="log-line" key={log.id}>
            <span className={logLevelPillClass(log.level)}>{log.level}</span>
            <div className="log-body">
              <div className="log-meta">
                <strong>{log.message}</strong>
                <time>{formatStoredTime(log.createdAt, props.timezone)}</time>
              </div>
              {log.detail && <pre>{log.detail}</pre>}
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
  watchDirId: string;
  useCustomProcessing: boolean;
  processing: OutputProcessingConfig;
  directories: WatchDir[];
  rescanning: boolean;
  onClose: () => void;
  onScopeChange: (value: RescanScope) => void;
  onTargetChange: (value: string) => void;
  onWatchDirIdChange: (value: string) => void;
  onUseCustomProcessingChange: (value: boolean) => void;
  onProcessingChange: (patch: Partial<OutputProcessingConfig>) => void;
  onBrowsePath: () => void;
  onSubmit: () => void;
}) {
  return (
    <div className="modal-backdrop" role="presentation" onClick={props.rescanning ? undefined : props.onClose}>
      <section className="modal-card" role="dialog" aria-modal="true" aria-labelledby="rescan-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <h2 id="rescan-title">扫描生成</h2>
          <IconCloseButton onClick={props.onClose} />
        </div>
        <div className="rescan-modal-grid">
          <section className="rescan-section">
            <div className="rescan-section-heading">
              <strong>扫描范围</strong>
              <small>选择需要扫描生成的文件范围。</small>
            </div>
            <label>
              范围
              <select value={props.scope} onChange={(event) => props.onScopeChange(event.target.value as RescanScope)}>
                <option value="all">全部媒体目录</option>
                <option value="dir">媒体目录或子路径</option>
                <option value="path">任意路径</option>
              </select>
            </label>
            {props.scope === 'dir' && (
              <label>
                媒体目录
                <select value={props.watchDirId} onChange={(event) => props.onWatchDirIdChange(event.target.value)}>
                  <option value="">请选择</option>
                  {props.directories.map((dir) => <option key={dir.id} value={String(dir.id)}>{dir.path}</option>)}
                </select>
              </label>
            )}
            {(props.scope === 'path' || props.scope === 'dir') && (
              <label>
                {props.scope === 'dir' ? '子路径（留空扫描整个媒体目录）' : '路径'}
                <div className="path-input"><input value={props.target} onChange={(event) => props.onTargetChange(event.target.value)} placeholder="D:\\Media\\Anime\\S01" /><button type="button" onClick={props.onBrowsePath} disabled={props.scope === 'dir' && !props.watchDirId}>选择</button></div>
              </label>
            )}
          </section>
          <section className="rescan-section">
            <div className="rescan-section-heading">
              <strong>处理设置</strong>
              <small>默认继承所属媒体目录设置，也可以为本次扫描单独配置。</small>
            </div>
            <Toggle label="使用一次性处理设置" checked={props.useCustomProcessing} onChange={props.onUseCustomProcessingChange} />
            {!props.useCustomProcessing && <p className="rescan-inherit-note">路径不属于媒体目录时，将继承全局处理设置。</p>}
          </section>
          {props.useCustomProcessing && (
            <section className="rescan-section rescan-custom-settings">
              <div className="rescan-section-heading">
                <strong>一次性处理设置</strong>
                <small>这些设置只应用于本次扫描生成任务。</small>
              </div>
              <SelectField label="处理策略" value={props.processing.strategy} options={[{ code: 'missing', name: '只补缺失' }, { code: 'force', name: '强制重建' }]} onChange={(value) => props.onProcessingChange({ strategy: value as RescanStrategy })} />
              <div className="rescan-toggle-grid">
                <Toggle label="字幕提取" checked={props.processing.enableSubtitles} onChange={(value) => props.onProcessingChange({ enableSubtitles: value })} />
                <Toggle label="MediaInfo" checked={props.processing.enableMediaInfo} onChange={(value) => props.onProcessingChange({ enableMediaInfo: value })} />
                <Toggle label="NFO" checked={props.processing.enableNfo} onChange={(value) => props.onProcessingChange({ enableNfo: value })} />
                <Toggle label="BIF" checked={props.processing.enableBif} onChange={(value) => props.onProcessingChange({ enableBif: value })} />
                <Toggle label="接管剧集/季度图片" checked={props.processing.enableImageTakeover} onChange={(value) => props.onProcessingChange({ enableImageTakeover: value })} />
              </div>
              {props.processing.enableBif && (
                <div className="rescan-bif-grid">
                  <label>BIF 宽度<input type="number" value={props.processing.bifWidth} onChange={(event) => props.onProcessingChange({ bifWidth: Number(event.target.value) })} /></label>
                  <label>BIF 间隔秒<input type="number" value={props.processing.bifInterval} onChange={(event) => props.onProcessingChange({ bifInterval: Number(event.target.value) })} /></label>
                  <SelectField label="BIF 加速" value={props.processing.bifHwAccel || 'cpu'} options={bifHwAccelOptions} onChange={(value) => props.onProcessingChange({ bifHwAccel: value })} />
                </div>
              )}
            </section>
          )}
        </div>
        <div className="inline-actions modal-actions">
          <button className="secondary" onClick={props.onClose}>取消</button>
          <button onClick={props.onSubmit} disabled={props.rescanning}>{props.rescanning ? '扫描中' : '开始扫描生成'}</button>
        </div>
      </section>
    </div>
  );
}

function WatchDirModal(props: {
  title: string;
  submitLabel: string;
  path: string;
  watchEnabled: boolean;
  useGlobalProcessing: boolean;
  processing: OutputProcessingConfig;
  onPathChange: (value: string) => void;
  onWatchEnabledChange: (value: boolean) => void;
  onUseGlobalProcessingChange: (value: boolean) => void;
  onProcessingChange: (patch: Partial<OutputProcessingConfig>) => void;
  onClose: () => void;
  onBrowsePath: () => void;
  onSubmit: () => void;
}) {
  return (
    <div className="modal-backdrop" role="presentation" onClick={props.onClose}>
      <section className="modal-card watch-dir-modal" role="dialog" aria-modal="true" aria-labelledby="watch-dir-modal-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <h2 id="watch-dir-modal-title">{props.title}</h2>
          <IconCloseButton onClick={props.onClose} />
        </div>
        <div className="config-form watch-dir-modal-form">
          <label>
            媒体目录路径
            <div className="path-input"><input value={props.path} onChange={(event) => props.onPathChange(event.target.value)} placeholder="D:\\Media\\Anime" autoFocus /><button type="button" onClick={props.onBrowsePath}>选择</button></div>
          </label>
          <Toggle label="自动监听" checked={props.watchEnabled} onChange={props.onWatchEnabledChange} />
          <Toggle label="跟随全局处理设置" checked={props.useGlobalProcessing} onChange={props.onUseGlobalProcessingChange} />
          {!props.useGlobalProcessing && (
            <>
              <SelectField label="处理策略" value={props.processing.strategy} options={[{ code: 'missing', name: '只补缺失' }, { code: 'force', name: '强制重建' }]} onChange={(value) => props.onProcessingChange({ strategy: value as RescanStrategy })} />
              <Toggle label="字幕提取" checked={props.processing.enableSubtitles} onChange={(value) => props.onProcessingChange({ enableSubtitles: value })} />
              <Toggle label="MediaInfo" checked={props.processing.enableMediaInfo} onChange={(value) => props.onProcessingChange({ enableMediaInfo: value })} />
              <Toggle label="NFO" checked={props.processing.enableNfo} onChange={(value) => props.onProcessingChange({ enableNfo: value })} />
              <Toggle label="BIF" checked={props.processing.enableBif} onChange={(value) => props.onProcessingChange({ enableBif: value })} />
              <label>BIF 宽度<input type="number" value={props.processing.bifWidth} onChange={(event) => props.onProcessingChange({ bifWidth: Number(event.target.value) })} /></label>
              <label>BIF 间隔秒<input type="number" value={props.processing.bifInterval} onChange={(event) => props.onProcessingChange({ bifInterval: Number(event.target.value) })} /></label>
              <SelectField label="BIF 加速" value={props.processing.bifHwAccel || 'cpu'} options={bifHwAccelOptions} onChange={(value) => props.onProcessingChange({ bifHwAccel: value })} />
              <Toggle label="接管剧集/季度图片" checked={props.processing.enableImageTakeover} onChange={(value) => props.onProcessingChange({ enableImageTakeover: value })} />
            </>
          )}
        </div>
        <p className="muted">保存后默认递归处理该目录。自动监听会在保存后立即热更新，无需重启服务。</p>
        <div className="inline-actions modal-actions">
          <button className="secondary" onClick={props.onClose}>取消</button>
          <button onClick={props.onSubmit} disabled={!props.path.trim()}>{props.submitLabel}</button>
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
  progress: number;
  onClose: () => void;
  onSeasonChange: (value: number) => void;
  onModeChange: (value: BatchEpisodeMode) => void;
  onOffsetChange: (value: number) => void;
  onStartChange: (value: number) => void;
  onSubmit: () => void;
}) {
  return (
    <div className="modal-backdrop" role="presentation" onClick={props.applying ? undefined : props.onClose}>
      <section className="modal-card batch-episode-modal" role="dialog" aria-modal="true" aria-labelledby="batch-episode-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <h2 id="batch-episode-title">批量修正季集</h2>
          <IconCloseButton onClick={props.onClose} />
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
          <button onClick={props.onSubmit} disabled={props.applying}>{props.applying ? `应用中 ${props.progress}/${props.count}` : '应用并查 TMDB'}</button>
        </div>
      </section>
    </div>
  );
}

function TmdbMatchModal(props: {
  title?: string;
  description?: string;
  applyLabel?: string;
  count?: number;
  query: string;
  results: TMDBSearchResult[];
  searching: boolean;
  applyingShowId: number | null;
  applyProgress: number;
  applyTotal: number;
  onQueryChange: (value: string) => void;
  onSearch: () => void;
  onApply: (show: TMDBSearchResult) => void;
  onClose: () => void;
}) {
  const applying = props.applyingShowId !== null;
  return (
    <div className="modal-backdrop" role="presentation" onClick={applying ? undefined : props.onClose}>
      <section className="modal-card tmdb-match-modal" role="dialog" aria-modal="true" aria-labelledby="tmdb-match-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <div>
            <h2 id="tmdb-match-title">{props.title || '更改匹配剧集'}</h2>
            <small>{props.description || `将应用到当前选中的 ${props.count ?? 0} 个文件，并重新生成预览。`}</small>
          </div>
          <IconCloseButton onClick={props.onClose} disabled={applying} />
        </div>
        <form className="tmdb-match-search" onSubmit={(event) => { event.preventDefault(); props.onSearch(); }}>
          <input autoFocus value={props.query} onChange={(event) => props.onQueryChange(event.target.value)} placeholder="搜索 TMDB 剧集，例如 Frieren" />
          <button type="submit" disabled={props.searching || applying || !props.query.trim()}>{props.searching ? '搜索中' : '搜索剧集'}</button>
        </form>
        <div className="tmdb-match-results">
          {props.results.length ? props.results.map((show) => (
            <button className="tmdb-match-result" type="button" key={show.id} onClick={() => props.onApply(show)} disabled={applying} title="套用到选中项并按各自行季集重新获取标题">
              <span>
                <strong>{show.name || show.originalName}</strong>
                {show.originalName && show.originalName !== show.name ? <small>{show.originalName}</small> : null}
              </span>
              <span className="tmdb-match-meta">{show.firstAirDate?.slice(0, 4) || '年份未知'} · TMDB #{show.id}</span>
              {show.overview ? <p>{show.overview}</p> : null}
              {props.applyingShowId === show.id ? <em>应用中 {props.applyProgress}/{props.applyTotal}</em> : <em>{props.applyLabel || '选择并应用'}</em>}
            </button>
          )) : (
            <div className="tmdb-match-empty">
              <strong>{props.searching ? '正在搜索剧集…' : '搜索并选择正确的剧集'}</strong>
              <span>{props.description || '选择后会更新当前勾选项，并按照各自季集重新查询标题。'}</span>
            </div>
          )}
        </div>
      </section>
    </div>
  );
}

function TmdbEpisodeDetailModal(props: { detail: TmdbEpisodeDetail; language: string; refreshing: boolean; onRefresh: () => void; onClose: () => void }) {
  const detail = props.detail;
  return (
    <div className="modal-backdrop" role="presentation" onClick={props.onClose}>
      <section className="modal-card tmdb-episode-detail-modal" role="dialog" aria-modal="true" aria-labelledby="tmdb-episode-detail-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <div>
            <h2 id="tmdb-episode-detail-title">{detail.showName || detail.showOriginalName}</h2>
            <small>TMDB #{detail.showId} · 查询语言 {props.language}</small>
          </div>
          <div className="inline-actions">
            <button className="secondary" type="button" onClick={props.onRefresh} disabled={props.refreshing}>{props.refreshing ? '刷新中' : '刷新'}</button>
            <IconCloseButton onClick={props.onClose} />
          </div>
        </div>
        <div className="tmdb-show-summary">
          {detail.showPosterUrl ? <img src={detail.showPosterUrl} alt={detail.showName || 'TMDB 剧集海报'} /> : null}
          <div>
            <h3>{detail.showName || detail.showOriginalName}</h3>
            {detail.showOriginalName && detail.showOriginalName !== detail.showName ? <p className="muted">{detail.showOriginalName}</p> : null}
            <div className="tmdb-episode-detail-meta">
              <span>首播年份：{detail.showFirstAirDate?.slice(0, 4) || '-'}</span>
              <span>状态：{detail.showStatus || '-'}</span>
              <span>剧集评分：{detail.showVoteAverage ? detail.showVoteAverage.toFixed(1) : '-'}</span>
              <span>类型：{detail.showGenres?.join(' / ') || '-'}</span>
            </div>
            <p className="tmdb-episode-overview">{detail.showOverview || '暂无剧集简介。'}</p>
          </div>
        </div>
        <div className="tmdb-current-episode-heading">
          <span>当前单集</span>
        </div>
        <div className="tmdb-episode-detail-content">
          {detail.stillUrl ? <img src={detail.stillUrl} alt={detail.title || 'TMDB 单集剧照'} /> : null}
          <div className="tmdb-episode-detail-copy">
            <span className="pill ok">S{String(detail.season).padStart(2, '0')}E{String(detail.episode).padStart(2, '0')}</span>
            <h3>{detail.title || '暂无单集标题'}</h3>
            <div className="tmdb-episode-detail-meta">
              <span>单集 ID：{detail.episodeId || '-'}</span>
              <span>播出日期：{detail.airDate || '-'}</span>
              <span>单集评分：{detail.voteAverage ? detail.voteAverage.toFixed(1) : '-'}</span>
            </div>
            <p className="tmdb-episode-overview">{detail.overview || '暂无简介。'}</p>
          </div>
        </div>
      </section>
    </div>
  );
}

function RenameHistoryModal(props: {
  history: RenameHistoryBatch[];
  undoingId: string;
  loading: boolean;
  timezone: string;
  onClose: () => void;
  onRefresh: () => void;
  onOpenDetails: (batch: RenameHistoryBatch) => void;
  onUndo: (id: string) => void;
}) {
  return (
    <div className="modal-backdrop" role="presentation" onClick={props.onClose}>
      <section className="modal-card rename-history-modal" role="dialog" aria-modal="true" aria-labelledby="rename-history-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <h2 id="rename-history-title">重命名历史</h2>
          <div className="inline-actions">
            <button className="secondary" onClick={props.onRefresh} disabled={props.loading}>{props.loading ? '刷新中' : '刷新历史'}</button>
            <IconCloseButton onClick={props.onClose} />
          </div>
        </div>
        <div className="rename-history-list">
          {props.history.length ? props.history.map((batch) => (
            <div className="history-item" key={batch.id}>
              <div className="history-summary">
                <button className="secondary" type="button" onClick={() => props.onOpenDetails(batch)}>详情</button>
                <div>
                  <strong>{formatStoredTime(batch.createdAt, props.timezone)}</strong>
                  <small>{batch.items.length} 项 · {batch.id}{batch.undone ? ` · 已撤销 ${batch.undoneAt ? formatStoredTime(batch.undoneAt, props.timezone) : ''}` : ''}</small>
                </div>
                <div className="inline-actions">
                  <button className="secondary" onClick={() => props.onUndo(batch.id)} disabled={batch.undone || props.undoingId === batch.id}>{batch.undone ? '已撤销' : props.undoingId === batch.id ? '撤销中' : '撤销'}</button>
                </div>
              </div>
            </div>
          )) : <p className="muted">暂无重命名历史。</p>}
        </div>
      </section>
    </div>
  );
}

function RenameHistoryDetailsModal(props: { batch: RenameHistoryBatch; undoCheck: RenameUndoCheckResult | null; timezone: string; onClose: () => void }) {
  return (
    <div className="modal-backdrop detail-backdrop" role="presentation" onClick={props.onClose}>
      <section className="modal-card rename-history-detail-modal" role="dialog" aria-modal="true" aria-labelledby="rename-history-detail-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <div>
            <h2 id="rename-history-detail-title">历史详情</h2>
            <small>{formatStoredTime(props.batch.createdAt, props.timezone)} · {props.batch.items.length} 项 · {props.batch.id}</small>
          </div>
          <IconCloseButton onClick={props.onClose} />
        </div>
        <div className="rename-history-detail-scroll">
          <HistoryDetails batch={props.batch} undoCheck={props.undoCheck} />
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

function RenameTemplateEditorModal(props: { value: string; matchPattern: string; sample: string; placeholders: string[]; onChange: (value: string) => void; onMatchPatternChange: (value: string) => void; onClose: () => void }) {
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const [sample, setSample] = useState(() => props.sample.split(/[\\/]/).pop() || props.sample);
  const matchResult = testMatchPattern(props.matchPattern, sample);
  const customPlaceholders = Object.keys(matchResult.variables).filter((name) => !props.placeholders.includes(`{${name}}`)).map((name) => `{${name}}`);

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
    <div className="modal-backdrop" role="presentation" onClick={props.onClose}>
      <section className="modal-card rename-template-modal" role="dialog" aria-modal="true" aria-labelledby="rename-template-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <h2 id="rename-template-title">编辑命名模板</h2>
          <IconCloseButton onClick={props.onClose} />
        </div>
        <textarea ref={textareaRef} value={props.value} onChange={(event) => props.onChange(event.target.value)} placeholder={defaultRenameTemplate} autoFocus />
        <div className="placeholder-bar modal-placeholder-bar">
          <span>插入占位符：</span>
          {props.placeholders.map((placeholder) => <button className="secondary" type="button" key={placeholder} onClick={() => insertPlaceholder(placeholder)}>{placeholder}</button>)}
          <button className="secondary" type="button" onClick={() => insertPlaceholder('{if:releaseGroup| - {releaseGroup}|}')}>可选字幕组</button>
          <button className="secondary" type="button" onClick={() => insertPlaceholder('{if:变量|有值时输出|无值时输出}')}>自定义条件</button>
        </div>
        <div className="muted template-help">
          <p>可填写文件名、相对路径或完整路径。</p>
          <p>{'{show:zh-CN}'} / {'{title:ja-JP}'} 这类语言标识可按语言取剧名/集标题。</p>
          <p>{'{season:00}'} / {'{episode:000}'} 这类全 0 格式可控制补零位数。</p>
          <p>{'{if:releaseGroup| - {releaseGroup}|未知字幕组}'} 可根据变量是否有值选择输出内容；暂不支持嵌套和 `|` 转义。</p>
        </div>
        <details className="rename-match-rule">
          <summary>自定义匹配规则</summary>
          <div className="rename-match-rule-content">
            <label>Go RE2 正则表达式
              <textarea value={props.matchPattern} onChange={(event) => props.onMatchPatternChange(event.target.value)} placeholder={'^\\[(?P<group>[^\\]]+)\\]\\s*(?P<show>.+?)\\s*-\\s*(?P<episode>\\d+)'} />
              <small>使用命名捕获组，例如 (?P&lt;group&gt;...)。NFO 优先级高于自定义规则。</small>
            </label>
            <label>测试文件名<input value={sample} onChange={(event) => setSample(event.target.value)} placeholder="[LoliHouse] MAO - 03.mkv" /></label>
            <div className={matchResult.error ? 'rename-match-test bad' : matchResult.matched ? 'rename-match-test ok' : 'rename-match-test'}>
              <strong>{matchResult.error ? '规则无法用于即时测试' : matchResult.matched ? '匹配成功' : '未匹配'}</strong>
              {matchResult.error ? <span>{matchResult.error}</span> : Object.entries(matchResult.variables).map(([name, value]) => <span key={name}><code>{name}</code> = {value || '（空）'}</span>)}
            </div>
            {customPlaceholders.length ? <div className="placeholder-bar"><span>自定义变量：</span>{customPlaceholders.map((placeholder) => <button className="secondary" type="button" key={placeholder} onClick={() => insertPlaceholder(placeholder)}>{placeholder}</button>)}</div> : null}
          </div>
        </details>
        <div className="inline-actions modal-actions">
          <button onClick={props.onClose}>完成</button>
        </div>
      </section>
    </div>
  );
}

function testMatchPattern(pattern: string, sample: string): { matched: boolean; variables: Record<string, string>; error?: string } {
  if (!pattern.trim()) return { matched: false, variables: {} };
  try {
    const jsPattern = pattern.replace(/\(\?P<([A-Za-z][A-Za-z0-9_]*)>/g, '(?<$1>');
    const match = new RegExp(jsPattern).exec(sample.replace(/\.[^.]+$/, ''));
    return { matched: Boolean(match), variables: match?.groups ?? {} };
  } catch (err) {
    return { matched: false, variables: {}, error: err instanceof Error ? err.message : '正则表达式无效' };
  }
}

function TargetPathEditorModal(props: { value: string; onChange: (value: string) => void; onClose: () => void; onSubmit: () => void }) {
  return (
    <div className="modal-backdrop" role="presentation" onClick={props.onClose}>
      <section className="modal-card target-path-modal" role="dialog" aria-modal="true" aria-labelledby="target-path-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <h2 id="target-path-title">编辑目标路径</h2>
          <IconCloseButton onClick={props.onClose} />
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

function DirectoryPicker(props: { title: string; initialPath: string; rootPath?: string; onSelect: (path: string) => void; onClose: () => void }) {
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
      if (props.rootPath?.trim()) params.set('root', props.rootPath.trim());
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
    <div className="modal-backdrop" role="presentation" onClick={props.onClose}>
      <section className="modal-card" role="dialog" aria-modal="true" aria-labelledby="directory-picker-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <h2 id="directory-picker-title">{props.title}</h2>
          <IconCloseButton onClick={props.onClose} />
        </div>
        <div className="form-row">
          <input value={currentPath} onChange={(event) => setCurrentPath(event.target.value)} placeholder="选择磁盘或输入路径" readOnly={Boolean(props.rootPath)} />
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

function PathField(props: { label: string; value: string; placeholder?: string; onChange: (value: string) => void; onBrowse: () => void }) {
  return (
    <label className="path-field">
      <span>{props.label}</span>
      <div className="path-input">
        <input value={props.value} placeholder={props.placeholder} onChange={(event) => props.onChange(event.target.value)} />
        <button className="icon-button" type="button" title={`选择${props.label}`} aria-label={`选择${props.label}`} onClick={props.onBrowse}>
          <FolderOpen size={17} />
        </button>
      </div>
    </label>
  );
}

function FieldLabel(props: { label: string; help: string }) {
  return (
    <span className="field-label">
      <span>{props.label}</span>
      <span className="help-tip" tabIndex={0} role="img" aria-label={`${props.label}说明`} onClick={(event) => { event.preventDefault(); event.stopPropagation(); }}>
        ?
        <span className="help-tip-content" role="tooltip">{props.help}</span>
      </span>
    </span>
  );
}

function SettingsGroup(props: { title: string; children: ReactNode }) {
  return (
    <section className="settings-group">
      <h3>{props.title}</h3>
      <div className="settings-group-fields">
        {props.children}
      </div>
    </section>
  );
}

function Toggle(props: { label: ReactNode; checked: boolean; onChange: (value: boolean) => void }) {
  return (
    <label className="toggle-row">
      <span>{props.label}</span>
      <input type="checkbox" checked={props.checked} onChange={(event) => props.onChange(event.target.checked)} />
      <span className="toggle-switch" aria-hidden="true">
        <span className="toggle-switch-label">{props.checked ? '开' : '关'}</span>
        <span className="toggle-switch-thumb" />
      </span>
    </label>
  );
}

function SelectField(props: { label: string; value: string; options: SelectOption[]; onChange: (value: string) => void }) {
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

function ImageSourcePriorityPicker(props: { label: string; values: string[]; onChange: (values: string[]) => void }) {
  const selected = normalizeImageSources(props.values);
  const selectedSet = new Set(selected);
  const available = imageSourceOptions.filter((option) => !selectedSet.has(option.code));

  function move(code: string, offset: number) {
    const index = selected.indexOf(code);
    const nextIndex = index + offset;
    if (index < 0 || nextIndex < 0 || nextIndex >= selected.length) return;
    const next = [...selected];
    [next[index], next[nextIndex]] = [next[nextIndex], next[index]];
    props.onChange(next);
  }

  function remove(code: string) {
    props.onChange(selected.filter((value) => value !== code));
  }

  return (
    <div className="image-source-picker">
      <span>{props.label}</span>
      <div className="image-source-list">
        {selected.length ? selected.map((code, index) => (
          <div className="image-source-row" key={code}>
            <span>{index + 1}. {imageSourceLabel(code)}</span>
            <div className="image-source-actions">
              <button type="button" aria-label={`${imageSourceLabel(code)} 上移`} title="上移" onClick={() => move(code, -1)} disabled={index === 0}>↑</button>
              <button type="button" aria-label={`${imageSourceLabel(code)} 下移`} title="下移" onClick={() => move(code, 1)} disabled={index === selected.length - 1}>↓</button>
              <button type="button" aria-label={`停用 ${imageSourceLabel(code)}`} title="停用" onClick={() => remove(code)} disabled={selected.length === 1}>×</button>
            </div>
          </div>
        )) : <small className="muted">未启用图片源。</small>}
      </div>
      {available.length > 0 && (
        <div className="image-source-add">
          {available.map((option) => (
            <button className="language-option" key={option.code} type="button" onClick={() => props.onChange([...selected, option.code])}>
              + {option.name}
            </button>
          ))}
        </div>
      )}
    </div>
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

function normalizeImageSources(values: string[]): string[] {
  const allowed = new Set(imageSourceOptions.map((option) => option.code));
  const result: string[] = [];
  for (const value of values) {
    const code = value.trim().toLowerCase();
    if (!allowed.has(code) || result.includes(code)) continue;
    result.push(code);
  }
  if (result.length === 0) return ['tmdb', 'fanart'];
  return result;
}

function imageSourceLabel(code: string): string {
  const option = imageSourceOptions.find((item) => item.code === code);
  return option ? option.name : code;
}

function asArray<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : [];
}

async function readErrorMessage(response: Response): Promise<string> {
  const text = await response.text();
  if (!text) return response.statusText || '请求失败';
  try {
    const data = JSON.parse(text) as { error?: string };
    return data.error || text;
  } catch {
    return text;
  }
}

function formatEpisodeList(values: number[] | null | undefined): string {
  const items = asArray(values);
  if (!items.length) return '';
  return items.join(', ');
}

function formatAuditIssueTarget(issue: AuditComparisonIssue): string {
  if (issue.episode) {
    return `S${String(issue.season).padStart(2, '0')}E${String(issue.episode).padStart(2, '0')}`;
  }
  if (issue.season) {
    return `S${String(issue.season).padStart(2, '0')}`;
  }
  return '剧集';
}

function groupArtifactIssues(issues: AuditComparisonIssue[] | null | undefined): { path: string; target: string; fields: string[] }[] {
  const groups = new Map<string, { path: string; target: string; fields: string[] }>();
  for (const issue of asArray(issues)) {
    const path = issue.local || formatAuditIssueTarget(issue);
    const existing = groups.get(path);
    const field = formatArtifactField(issue.field);
    if (existing) {
      if (!existing.fields.includes(field)) existing.fields.push(field);
      continue;
    }
    groups.set(path, { path, target: formatAuditIssueTarget(issue), fields: [field] });
  }
  return Array.from(groups.values());
}

function formatArtifactField(field: string): string {
  switch (field) {
    case 'episode.nfo': return 'NFO';
    case 'episode.thumb': return '单集图片';
    case 'episode.mediainfo': return 'MediaInfo';
    case 'episode.bif': return 'BIF';
    case 'season.nfo': return '季度 NFO';
    case 'season.image': return '季度图片';
    case 'series.tvshow.nfo': return 'tvshow.nfo';
    default:
      return field.startsWith('series.image.') ? `剧集图片 ${field.slice('series.image.'.length)}` : field;
  }
}

function formatFileAuditIssueType(type: string): string {
  switch (type) {
    case 'missing_remote':
      return '远端缺少';
    case 'extra_remote':
      return '远端多出';
    case 'extra_remote_dir':
      return '远端目录多出';
    case 'size_mismatch':
      return '大小不一致';
    case 'md5_mismatch':
      return 'MD5 不一致';
    case 'md5_error':
      return 'MD5 失败';
    default:
      return type;
  }
}

function normalizeExtensions(value: string): string[] {
  const seen = new Set<string>();
  const result: string[] = [];
  for (const part of value.split(/[\n,]/)) {
    const ext = part.trim().toLowerCase();
    if (!ext) continue;
    const normalized = ext.startsWith('.') ? ext : `.${ext}`;
    if (seen.has(normalized)) continue;
    seen.add(normalized);
    result.push(normalized);
  }
  return result;
}

function normalizeLines(value: string): string[] {
  const seen = new Set<string>();
  const result: string[] = [];
  for (const part of value.split(/\n/)) {
    const line = part.trim();
    if (!line || seen.has(line)) continue;
    seen.add(line);
    result.push(line);
  }
  return result;
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
