import { useEffect, useId, useMemo, useRef, useState } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent, MouseEvent as ReactMouseEvent, ReactNode } from 'react';
import {
  Activity,
  AlertTriangle,
  Ban,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  CircleGauge,
  CloudUpload,
  Database,
  Download,
  Eye,
  FileCheck2,
  Film,
  Filter,
  FolderCog,
  FolderOpen,
  History,
  KeyRound,
  LayoutDashboard,
  ListTodo,
  Monitor,
  Moon,
  Plus,
  Pencil,
  RefreshCw,
  RotateCcw,
  Save,
  SearchCheck,
  Settings,
  SlidersHorizontal,
  Sun,
  Tags,
  Trash2,
  UploadCloud,
  Undo2,
  Webhook,
  WandSparkles,
  X
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { checkDesktopUpdates, downloadAndInstallDesktopUpdate, getDesktopPreferences, getRuntimeInfo, notifyDesktop, pickDesktopDirectory, pickDesktopFile, previewDesktopRename, revealDesktopPath, setDesktopAutostart, subscribeDesktopTaskChanges, subscribeDesktopUploadChanges } from './desktop';
import type { DesktopPreferences, DesktopRuntimeInfo, DesktopUpdateCheckResult } from './desktop';
import { applyThemeMode, readThemeMode } from './theme';
import type { ThemeMode } from './theme';

type Health = {
  status: string;
  time: string;
};

type AppConfig = {
  server: { addr: string; timezone: string; publicBaseURL?: string };
  database: { path: string };
  tools: {
    ffmpeg: string;
    ffprobe: string;
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

type WatchDir = { id: number; path: string; recursive: boolean; enabled: boolean; watchEnabled: boolean; scanOnStart: boolean; useGlobalProcessing: boolean; processing: OutputProcessingConfig; uploadConfigs: UploadProviderRoute[] };

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
  scanRunId: string;
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

type TaskRun = {
  id: string;
  source: string;
  scopePath: string;
  status: string;
  total: number;
  active: number;
  completed: number;
  failed: number;
  canceled: number;
  ignored: number;
  errorSummary: string;
  createdAt: string;
  scanFinishedAt?: string;
  updatedAt: string;
};

type TaskRunListResponse = {
  items: TaskRun[];
};

type TaskSummary = {
  total: number;
  active: number;
  failed: number;
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
  userAgent: string;
  requestIntervalMs: number;
  hasCookie: boolean;
  hasCredentials: boolean;
  authDevice: string;
  preuploadBeforeRapid: boolean;
  createdAt: string;
  updatedAt: string;
};

type UploadCollisionPolicy = 'replace' | 'skip' | 'fail';

type UploadProviderRoute = {
  id?: number;
  providerId?: number;
  watchDirId?: number;
  enabled: boolean;
  remoteRoot: string;
  collisionPolicy: UploadCollisionPolicy;
  includeTypes: string[];
  notificationTemplateId?: number;
  notificationVariables?: Record<string, string>;
};

type UploadNotificationTemplate = {
  id: number;
  name: string;
  url: string;
  headersTemplate: string;
  payloadTemplate: string;
  createdAt: string;
  updatedAt: string;
};

type UploadNotificationRecord = {
  id: number;
  batchId: number;
  batchTargetId: number;
  seriesPath: string;
  batchStatus: string;
  providerName: string;
  targetStatus: string;
  templateId: number;
  templateName: string;
  url: string;
  status: string;
  attempts: number;
  availableAt: string;
  responseStatus: number;
  errorSummary: string;
  createdAt: string;
  deliveredAt: string;
  updatedAt: string;
};

type UploadNotificationRecordListResponse = {
  items: UploadNotificationRecord[];
  total: number;
  page: number;
  pageSize: number;
};

type UploadProviderDescriptor = {
  type: string;
  name: string;
  implemented: boolean;
  secretKeys: string[];
  authDevices?: UploadAuthDevice[];
};

type UploadAuthDevice = { code: string; name: string };

type BaiduOpenCredentials = {
  clientID: string;
  clientSecret: string;
  accessToken: string;
  refreshToken: string;
  accessTokenExpiresAt: string;
  brokerBaseURL: string;
  brokerClientID: string;
  brokerToken: string;
};

type BaiduPCSCredentials = {
  cookie: string;
  bdstoken: string;
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
  providerName: string;
  remoteRoot: string;
  targetCount: number;
  completedTargets: number;
  failedTargets: number;
  transferCount: number;
  completedTransfers: number;
  failedTransfers: number;
  notificationCount: number;
  deliveredNotifications: number;
  pendingNotifications: number;
  failedNotifications: number;
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
  retryable: boolean;
  notificationTemplateId?: number;
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
  outcome?: 'created' | 'replaced' | 'unchanged' | 'skipped';
  errorSummary: string;
  phase?: string;
  statusMessage?: string;
  waitingUntil?: string;
};

type UploadBatchDetail = {
  batch: UploadBatch;
  files: UploadBatchFile[];
  targets: UploadBatchTarget[];
  transfers: UploadTransfer[];
  notifications: UploadNotificationRecord[];
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

type Open115AuthStatus = {
  sessionId: string;
  providerId: number;
  clientId: string;
  state: string;
  message: string;
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
};

type BaiduOpenAuthStatus = {
  sessionId: string;
  providerId: number;
  mode: string;
  authorizationUrl?: string;
  redirectUri?: string;
  callbackUrl?: string;
  expiresAt?: string;
  state: string;
  message: string;
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
};

type BaiduOpenAuthConfig = {
  providerId: number;
  clientId?: string;
  clientSecretConfigured: boolean;
  accessTokenConfigured: boolean;
  refreshTokenConfigured: boolean;
  authMode: string;
  brokerBaseUrl?: string;
  brokerClientId?: string;
  brokerTokenConfigured: boolean;
  brokerConfigured: boolean;
  callbackUrl?: string;
  brokerCallbackUrl?: string;
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

type RemoteDirectoryEntry = { id: string; name: string; path: string; isDir: boolean; size: number };
type RemoteDirectoryList = { path: string; entries: RemoteDirectoryEntry[] };
type RemoteDirectoryPickerRequest = { provider: UploadProvider; value: string; onSelect: (path: string) => void };

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
type SettingsTab = 'basic' | 'processing' | 'scraping' | 'sources' | 'about';

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
type UploadNotificationStatusFilter = 'all' | 'pending' | 'processing' | 'delivered' | 'failed';
type UploadFileStatus = 'running' | 'pending' | 'failed' | 'canceled' | 'completed';
type UploadFileStatusFilter = 'all' | UploadFileStatus;
type UploadView = 'batches' | 'providers' | 'notifications' | 'notificationRecords';
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
  { value: 'scraping', label: '刮削' },
  { value: 'sources', label: '数据源' },
  { value: 'about', label: '关于' }
];

const themeOptions: Array<{ value: ThemeMode; label: string; icon: LucideIcon }> = [
  { value: 'system', label: '跟随系统', icon: Monitor },
  { value: 'light', label: '浅色', icon: Sun },
  { value: 'dark', label: '深色', icon: Moon }
];

function pageFromPath(pathname: string): PageKey {
  if (pathname === '/uploads' || pathname.startsWith('/uploads/')) return 'uploads';
  switch (pathname) {
    case '/settings':
      return 'settings';
    case '/watch-dirs':
      return 'watchDirs';
    case '/tasks':
      return 'tasks';
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
  { value: 'all', label: '全部' },
  { value: 'pending', label: '待执行' },
  { value: 'running', label: '执行中' },
  { value: 'completed', label: '已完成' },
  { value: 'failed', label: '失败' },
  { value: 'ignored', label: '已忽略' },
  { value: 'canceled', label: '已取消' }
];
const uploadStatusFilters: { value: UploadStatusFilter; label: string }[] = [
  { value: 'all', label: '全部' },
  { value: 'collecting', label: '合并中' },
  { value: 'pending', label: '等待上传' },
  { value: 'running', label: '上传中' },
  { value: 'completed', label: '已完成' },
  { value: 'partial', label: '部分失败' },
  { value: 'failed', label: '失败' },
  { value: 'canceled', label: '已取消' }
];
const uploadNotificationStatusFilters: { value: UploadNotificationStatusFilter; label: string }[] = [
  { value: 'all', label: '全部' },
  { value: 'pending', label: '等待/重试' },
  { value: 'processing', label: '发送中' },
  { value: 'delivered', label: '已送达' },
  { value: 'failed', label: '失败' }
];
const uploadFileStatusFilters: { value: UploadFileStatusFilter; label: string }[] = [
  { value: 'all', label: '全部' },
  { value: 'running', label: '上传中' },
  { value: 'pending', label: '等待中' },
  { value: 'failed', label: '失败' },
  { value: 'canceled', label: '已取消' },
  { value: 'completed', label: '已完成' }
];
const uploadFileStatusOrder: UploadFileStatus[] = ['running', 'pending', 'failed', 'canceled', 'completed'];

const fallback115AuthDevices: UploadAuthDevice[] = [
  { code: 'web', name: '网页端' },
  { code: 'android', name: 'Android' },
  { code: 'ios', name: 'iOS' },
  { code: 'tv', name: '电视端' },
  { code: 'alipaymini', name: '支付宝小程序' },
  { code: 'wechatmini', name: '微信小程序' },
  { code: 'qandroid', name: '115组织 Android' }
];

function uploadAuthDevices(providerType: string, descriptors: UploadProviderDescriptor[]) {
  const configured = descriptors.find((descriptor) => descriptor.type === providerType)?.authDevices;
  return configured?.length ? configured : (providerType === '115cookie' ? fallback115AuthDevices : []);
}

function preferredUploadAuthDevice(providerType: string, descriptors: UploadProviderDescriptor[]) {
  const devices = uploadAuthDevices(providerType, descriptors);
  return devices.some((device) => device.code === 'web') ? 'web' : devices[0]?.code ?? '';
}

function uploadAuthDeviceName(code: string, providerType: string, descriptors: UploadProviderDescriptor[]) {
  if (!code) return '未记录';
  return uploadAuthDevices(providerType, descriptors).find((device) => device.code === code)?.name ?? code;
}
const taskDetailRefreshIntervalMs = 5000;
const uploadDetailRefreshIntervalMs = 2000;
const uploadDetailPageSize = 20;
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

function isRenameItemExecutable(item: RenamePreviewItem, pendingPaths: Set<string>, recalculatingPaths: Set<string>) {
  return !pendingPaths.has(item.path)
    && !recalculatingPaths.has(item.path)
    && !item.conflict
    && !['error', 'renamed', 'skipped'].includes(item.status)
    && Boolean(item.newName || item.newPath || item.renderedTarget);
}

function isCookieAuthorizationActive(auth: CookieAuthStatus | null) {
  return Boolean(auth && !['authorized', 'expired', 'cancelled', 'error'].includes(auth.state));
}

function isOpen115AuthorizationActive(auth: Open115AuthStatus | null) {
  return Boolean(auth && !['authorized', 'expired', 'cancelled', 'error'].includes(auth.state));
}

function isBaiduOpenAuthorizationActive(auth: BaiduOpenAuthStatus | null) {
  return Boolean(auth && !['authorized', 'completed', 'expired', 'cancelled', 'error', 'failed'].includes(auth.state));
}

function uploadProviderNeedsAuthorization(provider: UploadProvider | undefined) {
  return Boolean(provider && ((['115cookie', 'baidupcs'].includes(provider.type) && !provider.hasCookie) || (['115open', 'baidupan'].includes(provider.type) && !provider.hasCredentials)));
}

function watchDirDraftSignature(path: string, watchEnabled: boolean, useGlobalProcessing: boolean, processing: OutputProcessingConfig, uploadConfigs: UploadProviderRoute[]) {
  return JSON.stringify({ path, watchEnabled, useGlobalProcessing, processing, uploadConfigs });
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

function taskStatusLabel(status: string) {
  return taskStatusFilters.find((item) => item.value === status)?.label ?? status;
}

function taskTypeLabel(type: string) {
  switch (type) {
    case 'media_process': return '媒体处理';
    default: return type || '-';
  }
}

function isTaskRunActive(status: string) {
  return status === 'collecting' || status === 'running';
}

function isTaskRunTerminal(status: string) {
  return ['completed', 'failed', 'canceled', 'ignored', 'partial', 'empty'].includes(status);
}

function healthStatusLabel(status?: string) {
  switch (status) {
    case 'ok': return '正常';
    case 'degraded': return '部分异常';
    case 'error': return '异常';
    case undefined:
    case '': return '连接中';
    default: return status;
  }
}

function updateSupportMessage(reason?: DesktopUpdateCheckResult['reason']) {
  switch (reason) {
    case 'developmentBuild': return '开发构建不提供自动更新';
    case 'notInstalled': return '便携版不提供自动更新，请使用安装版';
    case 'unsupportedPlatform': return '当前平台暂不提供应用内自动更新';
    default: return '当前构建不提供自动更新';
  }
}

function formatBuildDate(value?: string) {
  if (!value || value === 'unknown') return '未知';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
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

function uploadStatusLabel(status: string) {
  if (status === 'waiting') return '等待上传';
  return uploadStatusFilters.find((item) => item.value === status)?.label ?? status;
}

function uploadNotificationStatusPillClass(status: string) {
  switch (status) {
    case 'delivered':
      return 'pill ok';
    case 'failed':
      return 'pill bad';
    case 'processing':
      return 'pill running';
    case 'pending':
      return 'pill pending';
    default:
      return 'pill';
  }
}

function uploadNotificationStatusLabel(status: string) {
  switch (status) {
    case 'pending': return '等待/重试';
    case 'processing': return '发送中';
    case 'delivered': return '已送达';
    case 'failed': return '失败';
    default: return status || '-';
  }
}

function uploadBatchNotificationSummary(batch: UploadBatch) {
  if (batch.notificationCount <= 0) return { className: 'pill ignored', label: '无通知记录' };
  if (batch.failedNotifications > 0) return { className: 'pill bad', label: `失败 ${batch.failedNotifications}` };
  if (batch.pendingNotifications > 0) return { className: 'pill pending', label: '发送中' };
  if (batch.deliveredNotifications === batch.notificationCount) return { className: 'pill ok', label: '已送达' };
  return { className: 'pill pending', label: '待处理' };
}

function uploadBatchNotificationDetailSummary(detail: UploadBatchDetail) {
  const notifications = detail.notifications ?? [];
  if (notifications.length > 0) {
    return {
      label: `${notifications.length} 条记录`,
      emptyMessage: ''
    };
  }

  const configuredTargets = detail.targets.filter((target) => (target.notificationTemplateId ?? 0) > 0);
  if (configuredTargets.length === 0) {
    return {
      label: '未配置通知',
      emptyMessage: '该上传目标没有配置通知模板。'
    };
  }

  const waitingTargets = configuredTargets.some((target) => ['waiting', 'pending', 'running'].includes(target.status));
  if (waitingTargets) {
    return {
      label: '已配置，等待上传完成',
      emptyMessage: '通知将在上传目标完成且检测到远端变化后生成。'
    };
  }

  const allConfiguredTargetsCompleted = configuredTargets.every((target) => target.status === 'completed');
  if (allConfiguredTargetsCompleted) {
    return {
      label: '本次无需发送',
      emptyMessage: '上传已完成，但没有检测到需要通知的远端变化。'
    };
  }

  return {
    label: '已配置，尚未生成记录',
    emptyMessage: '通知模板已配置，但上传目标未成功完成。'
  };
}

function uploadTargetStatusLabel(target: UploadBatchTarget) {
  if (target.status === 'pending' && target.errorSummary) return '等待自动重试';
  return uploadStatusLabel(target.status);
}

function uploadTargetScheduleLabel(target: UploadBatchTarget, timezone: string) {
  if (target.status !== 'pending') return '';
  if (!target.availableAt) return target.errorSummary ? '等待自动重试调度' : '等待调度';
  const availableAt = parseStoredTime(target.availableAt);
  const formatted = formatStoredTime(target.availableAt, timezone);
  const isReady = Boolean(availableAt && availableAt.getTime() <= Date.now());
  if (target.errorSummary) {
    return isReady ? `重试时间已到（${formatted}）` : `下次尝试：${formatted}`;
  }
  return isReady ? `已可执行，等待调度（${formatted}）` : `可执行时间：${formatted}`;
}

function uploadTransferIsWaiting(transfer: UploadTransfer) {
  const phase = transfer.phase?.trim().toLowerCase() ?? '';
  return Boolean(transfer.waitingUntil)
    || phase.includes('wait')
    || phase.includes('throttl')
    || phase.includes('rate_limit');
}

function uploadTransferHasActivePhase(transfer: UploadTransfer) {
  return Boolean(transfer.phase?.trim()) && !uploadTransferIsWaiting(transfer);
}

function uploadTransferIsActive(transfer: UploadTransfer) {
  return !['completed', 'canceled'].includes(transfer.status)
    && (uploadTransferIsWaiting(transfer) || uploadTransferHasActivePhase(transfer));
}

function uploadTransferQueueOrder(transfers: UploadTransfer[]) {
  const incompleteIDs = transfers
    .filter((transfer) => transfer.status !== 'completed')
    .map((transfer) => transfer.id);
  const candidateIDs = incompleteIDs.length ? incompleteIDs : transfers.map((transfer) => transfer.id);
  return candidateIDs.length ? Math.min(...candidateIDs) : Number.MAX_SAFE_INTEGER;
}

function uploadTransferProgress(transfer: UploadTransfer) {
  if (!uploadTransferIsActive(transfer) || transfer.bytesTotal <= 0 || transfer.bytesTransferred <= 0) return null;
  const bytesTransferred = Math.min(transfer.bytesTransferred, transfer.bytesTotal);
  return {
    bytesTransferred,
    bytesTotal: transfer.bytesTotal,
    percent: Math.min(100, Math.max(0, Math.round((bytesTransferred / transfer.bytesTotal) * 100)))
  };
}

function effectiveUploadTransferStatus(transfer: UploadTransfer, target?: UploadBatchTarget): UploadFileStatus {
  if (transfer.status === 'completed') return 'completed';
  if (target?.status === 'failed') return 'failed';
  if (target?.status === 'canceled') return 'canceled';
  if (uploadTransferIsWaiting(transfer)) return 'pending';
  if (uploadTransferHasActivePhase(transfer)) return 'running';
  if (transfer.status === 'failed' && ['waiting', 'pending', 'running'].includes(target?.status ?? '')) return 'pending';
  if (transfer.status === 'failed') return 'failed';
  if (transfer.status === 'running') return 'running';
  if (transfer.status === 'canceled') return 'canceled';
  return 'pending';
}

function aggregateUploadFileStatus(transfers: UploadTransfer[], targetsByID: ReadonlyMap<number, UploadBatchTarget>): UploadFileStatus {
  const statuses = new Set(transfers.map((transfer) => effectiveUploadTransferStatus(transfer, targetsByID.get(transfer.batchTargetId))));
  return uploadFileStatusOrder.find((status) => statuses.has(status)) ?? 'pending';
}

function uploadTransferDisplay(transfer: UploadTransfer, target?: UploadBatchTarget) {
  const status = effectiveUploadTransferStatus(transfer, target);
  if (status === 'completed') {
    switch (transfer.outcome) {
      case 'created':
        return { className: 'pill ok', label: '已上传' };
      case 'replaced':
        return { className: 'pill ok', label: '已替换' };
      case 'unchanged':
        return { className: 'pill ignored', label: '未变化' };
      case 'skipped':
        return { className: 'pill ignored', label: '已跳过' };
    }
  }
  if (status === 'pending' && transfer.status === 'failed' && target?.status === 'pending' && !uploadTransferIsWaiting(transfer)) {
    return { className: uploadStatusPillClass('pending'), label: '等待自动重试' };
  }
  if (status === 'pending' && uploadTransferIsWaiting(transfer)) {
    return { className: uploadStatusPillClass('pending'), label: '等待中' };
  }
  if (status === 'failed' && target?.retryable) {
    return { className: uploadStatusPillClass('failed'), label: '失败（可重试）' };
  }
  return { className: uploadStatusPillClass(status), label: uploadStatusLabel(status) };
}

function uploadTransferPhaseLabel(phase?: string) {
  const normalized = phase?.trim().toLowerCase() ?? '';
  switch (normalized) {
    case '':
      return '';
    case 'waiting':
    case 'rate_limit_wait':
    case 'throttle_wait':
    case 'throttled':
      return '等待请求间隔';
    case 'checking':
      return '检查远端文件';
    case 'preparing':
      return '准备上传';
    case 'hashing':
      return '计算文件摘要';
    case 'rapid_upload':
      return '尝试秒传';
    case 'upload_info':
      return '获取上传信息';
    case 'uploading':
    case 'oss_upload':
      return '上传数据';
    case 'verifying':
      return '验证远端文件';
    case 'completing':
      return '提交上传结果';
    default:
      return phase?.trim() ?? '';
  }
}

function formatUploadWaitingCountdown(waitingUntil: string | undefined, now: number) {
  const deadline = waitingUntil ? parseStoredTime(waitingUntil) : null;
  if (!deadline) return '';
  const remainingSeconds = Math.ceil((deadline.getTime() - now) / 1000);
  if (remainingSeconds <= 0) return '即将继续';
  if (remainingSeconds < 60) return `${remainingSeconds} 秒后继续`;
  const minutes = Math.floor(remainingSeconds / 60);
  const seconds = remainingSeconds % 60;
  if (minutes < 60) return `${minutes} 分 ${seconds} 秒后继续`;
  const hours = Math.floor(minutes / 60);
  return `${hours} 小时 ${minutes % 60} 分后继续`;
}

function uploadTransferActivity(transfer: UploadTransfer, now: number) {
  const countdown = formatUploadWaitingCountdown(transfer.waitingUntil, now);
  const phase = uploadTransferPhaseLabel(transfer.phase) || (countdown ? '等待请求间隔' : '');
  const message = transfer.statusMessage?.trim() ?? '';
  return [phase, message && message !== phase ? message : '', countdown].filter(Boolean).join(' · ');
}

function renameStatusLabel(status: string) {
  switch (status) {
    case 'ok': return '可执行';
    case 'warning': return '需确认';
    case 'error': return '错误';
    case 'skipped': return '已跳过';
    case 'renamed': return '已完成';
    default: return status;
  }
}

function uploadViewFromPath(pathname: string): UploadView {
  if (pathname === '/uploads/providers') return 'providers';
  if (pathname === '/uploads/notifications') return 'notifications';
  if (pathname === '/uploads/notification-records') return 'notificationRecords';
  return 'batches';
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
  const [desktopPreferences, setDesktopPreferences] = useState<DesktopPreferences | null>(null);
  const [themeMode, setThemeMode] = useState<ThemeMode>(() => readThemeMode());
  const [initialLoading, setInitialLoading] = useState(true);
  const [connectionOnline, setConnectionOnline] = useState(false);
  const [healthCheckedAt, setHealthCheckedAt] = useState<Date | null>(null);
  const [restartRequired, setRestartRequired] = useState(false);
  const [tools, setTools] = useState<ToolStatus[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [taskRuns, setTaskRuns] = useState<TaskRun[]>([]);
  const [taskSummary, setTaskSummary] = useState<TaskSummary>({ total: 0, active: 0, failed: 0 });
  const [uploadSummary, setUploadSummary] = useState<UploadSummary>({ collecting: 0, pending: 0, running: 0, completed: 0, failed: 0 });
  const [uploadBatches, setUploadBatches] = useState<UploadBatch[]>([]);
  const [uploadProviders, setUploadProviders] = useState<UploadProvider[]>([]);
  const [uploadProviderTypes, setUploadProviderTypes] = useState<UploadProviderDescriptor[]>([]);
  const [uploadNotificationTemplates, setUploadNotificationTemplates] = useState<UploadNotificationTemplate[]>([]);
  const [uploadNotificationRecords, setUploadNotificationRecords] = useState<UploadNotificationRecord[]>([]);
  const [uploadView, setUploadView] = useState<UploadView>(() => uploadViewFromPath(window.location.pathname));
  const [uploadProviderUsage, setUploadProviderUsage] = useState<UploadProvider | null>(null);
  const [uploadNotificationTemplateModal, setUploadNotificationTemplateModal] = useState<UploadNotificationTemplate | null>(null);
  const [newUploadNotificationTemplateOpen, setNewUploadNotificationTemplateOpen] = useState(false);
  const [settingsTab, setSettingsTab] = useState<SettingsTab>('basic');
  const [taskTotal, setTaskTotal] = useState(0);
  const [taskPage, setTaskPage] = useState(1);
  const [taskPageSize] = useState(20);
  const [taskStatusFilter, setTaskStatusFilter] = useState<TaskStatusFilter>('all');
  const [taskRunFilter, setTaskRunFilter] = useState('');
  const [taskPathFilter, setTaskPathFilter] = useState('');
  const [taskFromFilter, setTaskFromFilter] = useState('');
  const [taskToFilter, setTaskToFilter] = useState('');
  const [appliedTaskFilters, setAppliedTaskFilters] = useState({ scanRunId: '', path: '', from: '', to: '' });
  const [uploadTotal, setUploadTotal] = useState(0);
  const [uploadPage, setUploadPage] = useState(1);
  const [uploadStatusFilter, setUploadStatusFilter] = useState<UploadStatusFilter>('all');
  const [uploadPathFilter, setUploadPathFilter] = useState('');
  const [appliedUploadPathFilter, setAppliedUploadPathFilter] = useState('');
  const [uploadNotificationTotal, setUploadNotificationTotal] = useState(0);
  const [uploadNotificationPage, setUploadNotificationPage] = useState(1);
  const [uploadNotificationStatusFilter, setUploadNotificationStatusFilter] = useState<UploadNotificationStatusFilter>('all');
  const [uploadNotificationPathFilter, setUploadNotificationPathFilter] = useState('');
  const [appliedUploadNotificationPathFilter, setAppliedUploadNotificationPathFilter] = useState('');
  const [refreshingUploadNotifications, setRefreshingUploadNotifications] = useState(false);
  const [refreshingUploads, setRefreshingUploads] = useState(false);
  const [refreshingUploadProviders, setRefreshingUploadProviders] = useState(false);
  const [savingUploadNotificationTemplate, setSavingUploadNotificationTemplate] = useState(false);
  const [selectedUploadBatch, setSelectedUploadBatch] = useState<UploadBatchDetail | null>(null);
  const [uploadProviderModal, setUploadProviderModal] = useState<UploadProvider | null>(null);
  const [newUploadProviderOpen, setNewUploadProviderOpen] = useState(false);
  const [uploadCookieProvider, setUploadCookieProvider] = useState<UploadProvider | null>(null);
  const [uploadCookieValue, setUploadCookieValue] = useState('');
  const [uploadCookieDevice, setUploadCookieDevice] = useState('web');
  const [cookieAuth, setCookieAuth] = useState<CookieAuthStatus | null>(null);
  const [uploadOpen115Provider, setUploadOpen115Provider] = useState<UploadProvider | null>(null);
  const [open115ClientID, setOpen115ClientID] = useState('');
  const [open115Auth, setOpen115Auth] = useState<Open115AuthStatus | null>(null);
  const [open115Tokens, setOpen115Tokens] = useState({ accessToken: '', refreshToken: '' });
  const [showOpen115Tokens, setShowOpen115Tokens] = useState(false);
  const [uploadBaiduOpenProvider, setUploadBaiduOpenProvider] = useState<UploadProvider | null>(null);
  const [baiduOpenCredentials, setBaiduOpenCredentials] = useState<BaiduOpenCredentials>({ clientID: '', clientSecret: '', accessToken: '', refreshToken: '', accessTokenExpiresAt: '', brokerBaseURL: '', brokerClientID: '', brokerToken: '' });
  const [baiduOpenMode, setBaiduOpenMode] = useState('official');
  const [baiduOpenAuth, setBaiduOpenAuth] = useState<BaiduOpenAuthStatus | null>(null);
  const [baiduOpenAuthConfig, setBaiduOpenAuthConfig] = useState<BaiduOpenAuthConfig | null>(null);
  const [showBaiduOpenTokens, setShowBaiduOpenTokens] = useState(false);
  const [uploadBaiduPCSProvider, setUploadBaiduPCSProvider] = useState<UploadProvider | null>(null);
  const [baiduPCSCredentials, setBaiduPCSCredentials] = useState<BaiduPCSCredentials>({ cookie: '', bdstoken: '' });
  const [showBaiduPCSSecrets, setShowBaiduPCSSecrets] = useState(false);
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
  const [renamePreviewSignature, setRenamePreviewSignature] = useState('');
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
  const [pendingRenamePaths, setPendingRenamePaths] = useState<string[]>([]);
  const [applyingRename, setApplyingRename] = useState(false);
  const [batchEpisodeOpen, setBatchEpisodeOpen] = useState(false);
  const [batchSeason, setBatchSeason] = useState(1);
  const [batchEpisodeMode, setBatchEpisodeMode] = useState<BatchEpisodeMode>('sequence');
  const [batchEpisodeOffset, setBatchEpisodeOffset] = useState(0);
  const [batchEpisodeStart, setBatchEpisodeStart] = useState(1);
  const [applyingBatchEpisode, setApplyingBatchEpisode] = useState(false);
  const [batchEpisodeProgress, setBatchEpisodeProgress] = useState(0);
  const [targetPathEditor, setTargetPathEditor] = useState<{ path: string; value: string; initialValue: string } | null>(null);
  const [renameTemplateEditorOpen, setRenameTemplateEditorOpen] = useState(false);
  const [previewingRename, setPreviewingRename] = useState(false);
  const [directoryPicker, setDirectoryPicker] = useState<{ title: string; value: string; rootPath?: string; onSelect: (path: string) => void } | null>(null);
  const [remoteDirectoryPicker, setRemoteDirectoryPicker] = useState<RemoteDirectoryPickerRequest | null>(null);
  const [newWatchDir, setNewWatchDir] = useState('');
  const [newWatchDirWatchEnabled, setNewWatchDirWatchEnabled] = useState(true);
  const [newWatchDirUseGlobalProcessing, setNewWatchDirUseGlobalProcessing] = useState(true);
  const [newWatchDirProcessing, setNewWatchDirProcessing] = useState<OutputProcessingConfig>(() => defaultOutputProcessing());
  const [newWatchDirUploadConfigs, setNewWatchDirUploadConfigs] = useState<UploadProviderRoute[]>([]);
  const [savingWatchDir, setSavingWatchDir] = useState(false);
  const [addWatchDirOpen, setAddWatchDirOpen] = useState(false);
  const [editingWatchDir, setEditingWatchDir] = useState<WatchDir | null>(null);
  const [editingWatchDirPath, setEditingWatchDirPath] = useState('');
  const [editingWatchDirWatchEnabled, setEditingWatchDirWatchEnabled] = useState(true);
  const [editingWatchDirUseGlobalProcessing, setEditingWatchDirUseGlobalProcessing] = useState(true);
  const [editingWatchDirProcessing, setEditingWatchDirProcessing] = useState<OutputProcessingConfig>(() => defaultOutputProcessing());
  const [editingWatchDirUploadConfigs, setEditingWatchDirUploadConfigs] = useState<UploadProviderRoute[]>([]);
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
  const [savingDesktopPreferences, setSavingDesktopPreferences] = useState(false);
  const [updateCheck, setUpdateCheck] = useState<DesktopUpdateCheckResult | null>(null);
  const [checkingUpdates, setCheckingUpdates] = useState(false);
  const [installingUpdate, setInstallingUpdate] = useState(false);
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
  const requestUploadCookieCloseRef = useRef<() => void>(() => {});
  const requestUploadOpen115CloseRef = useRef<() => void>(() => {});
  const renamePreviewAbortRef = useRef<AbortController | null>(null);
  const refreshingUploadsRef = useRef(false);
  const refreshingUploadProvidersRef = useRef(false);
  const refreshingUploadNotificationsRef = useRef(false);
  const uploadDetailRequestRef = useRef(0);
  const missingAuditRequestRef = useRef(0);
  const embyAuditRequestRef = useRef(0);
  const fileAuditRequestRef = useRef(0);
  const workspaceContentRef = useRef<HTMLDivElement>(null);
  const allowSettingsHistoryNavigationRef = useRef(false);
  const observedTaskRunStatusesRef = useRef(new Map<string, string>());
  const notifiedTaskRunIDsRef = useRef(new Set<string>());
  const taskRunNotificationBaselineReadyRef = useRef(false);
  const taskListRequestRef = useRef(0);
  const uploadListRequestRef = useRef(0);
  const uploadNotificationRequestRef = useRef(0);
  const observedUploadStatusesRef = useRef(new Map<number, string>());
  const lastRenameSelectionIndexRef = useRef<number | null>(null);
  const lastTaskSelectionIndexRef = useRef<number | null>(null);
  const watchDirDraftInitialRef = useRef('');
  const remoteDirectoryCacheRef = useRef(new Map<string, RemoteDirectoryList>());
  const remoteDirectoryRequestCacheRef = useRef(new Map<string, Promise<RemoteDirectoryList>>());
  const activeModalStackRef = useRef('');
  const modalFocusReturnRef = useRef(new Map<string, HTMLElement | null>());
  const modalBusyRef = useRef({ applyingBatchEpisode, rescanning, savingUploadProvider, savingUploadNotificationTemplate, savingEmbyKey, savingWatchDir });
  modalBusyRef.current = { applyingBatchEpisode, rescanning, savingUploadProvider, savingUploadNotificationTemplate, savingEmbyKey, savingWatchDir };
  const displayTimezone = config?.server.timezone || 'Asia/Shanghai';
  const renameBatchConcurrency = previewWorkerCount(config?.renaming?.concurrency ?? 3);
  const renameErrorCount = renamePreview.filter((item) => item.status === 'error' || item.conflict).length;
  const renameWarningCount = renamePreview.filter((item) => item.status === 'warning').length;
  const renameInputSignature = JSON.stringify({
    path: renamePath.trim(),
    template: renameTemplate,
    matchPattern: renameMatchPattern,
    language: renameLanguage,
    releaseGroup: renameReleaseGroup.trim()
  });
  const renamePreviewStale = renamePreview.length > 0 && renamePreviewSignature !== renameInputSignature;
  const pendingRenamePathSet = new Set(pendingRenamePaths);
  const recalculatingRenamePathSet = new Set(recalculatingRenamePaths);
  const renamePendingCount = new Set([...pendingRenamePaths, ...recalculatingRenamePaths]).size;
  const executableRenameItems = renamePreview.filter((item) => isRenameItemExecutable(item, pendingRenamePathSet, recalculatingRenamePathSet));
  const executableRenamePathSet = new Set(executableRenameItems.map((item) => item.path));
  const selectedRenameItemsBlocked = selectedRenamePaths.some((path) => !executableRenamePathSet.has(path));
  const availableToolCount = tools.filter((tool) => tool.available).length;
  const coreToolsReady = ['ffmpeg', 'ffprobe'].every((name) => tools.some((tool) => tool.name === name && tool.available));
  const enabledWatchDirCount = watchDirs.filter((dir) => dir.enabled).length;
  const activeTaskCount = taskSummary.active;
  const failedTaskCount = taskSummary.failed;
  const selectedTasks = tasks.filter((task) => selectedTaskIds.includes(task.id));
  const retryableSelectedTaskIds = selectedTasks.filter((task) => ['failed', 'canceled', 'ignored'].includes(task.status) && task.mediaFileId).map((task) => task.id);
  const ignorableSelectedTaskIds = selectedTasks.filter((task) => task.status === 'failed').map((task) => task.id);
  const configDirty = Boolean(config && savedConfig && JSON.stringify(config) !== JSON.stringify(savedConfig));
  const watchDirDraftDirty = addWatchDirOpen
    ? watchDirDraftInitialRef.current !== watchDirDraftSignature(newWatchDir, newWatchDirWatchEnabled, newWatchDirUseGlobalProcessing, newWatchDirProcessing, newWatchDirUploadConfigs)
    : Boolean(editingWatchDir && watchDirDraftInitialRef.current !== watchDirDraftSignature(editingWatchDirPath, editingWatchDirWatchEnabled, editingWatchDirUseGlobalProcessing, editingWatchDirProcessing, editingWatchDirUploadConfigs));
  const targetPathDraftDirty = Boolean(targetPathEditor && targetPathEditor.value !== targetPathEditor.initialValue);
  const currentPageMeta = pageMeta[activePage];
  requestUploadCookieCloseRef.current = requestCloseUploadCookieModal;
  requestUploadOpen115CloseRef.current = requestCloseUploadOpen115Modal;
  const modalStackKey = [
    remoteDirectoryPicker && 'remote-directory', directoryPicker && 'directory', targetPathEditor && 'target-path', selectedHistoryBatch && 'history-detail', renameHistoryOpen && 'history',
    renameTemplateEditorOpen && 'template', tmdbEpisodeDetail && 'episode-detail', addEmbyKeyOpen && 'emby-key', auditTmdbMatchOpen && 'audit-tmdb', tmdbMatchOpen && 'tmdb',
    uploadOpen115Provider && 'upload-open115', uploadBaiduOpenProvider && 'upload-baiduopen', uploadBaiduPCSProvider && 'upload-baidupcs',
    batchEpisodeOpen && 'batch', uploadCookieProvider && 'upload-cookie', uploadProviderUsage && 'upload-provider-usage', (newUploadProviderOpen || uploadProviderModal) && 'upload-provider',
    (newUploadNotificationTemplateOpen || uploadNotificationTemplateModal) && 'upload-notification-template',
    editingWatchDir && 'edit-dir', addWatchDirOpen && 'add-dir', rescanOpen && 'rescan', selectedUploadBatch && 'upload-detail', selectedTask && 'task-detail',
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
      const [
        healthResult,
        configResult,
        toolsResult,
        tasksResult,
        taskRunsResult,
        taskSummaryResult,
        dirsResult,
        artifactsResult,
        providersResult,
        providerTypesResult,
        notificationTemplatesResult,
        runtimeResult,
        desktopPreferencesResult,
        renameHistoryResult,
        embyKeysResult
      ] = await Promise.allSettled([
        requestJSON<Health>('/api/health', '服务状态'),
        requestJSON<AppConfig>('/api/config', '应用配置'),
        requestJSON<ToolStatus[]>('/api/tools/status', '工具状态'),
        requestJSON<TaskListResponse | Task[]>(`/api/tasks?page=1&pageSize=${taskPageSize}`, '任务列表'),
        requestJSON<TaskRun[] | TaskRunListResponse>('/api/tasks/runs?limit=200', '任务批次'),
        requestJSON<TaskSummary>('/api/tasks/summary', '任务摘要'),
        requestJSON<WatchDir[]>('/api/watch-dirs', '媒体目录'),
        requestJSON<Artifact[]>('/api/artifacts?limit=10', '最近产物'),
        requestJSON<UploadProvider[]>('/api/upload/providers', '上传 Provider'),
        requestJSON<UploadProviderDescriptor[]>('/api/upload/provider-types', 'Provider 类型'),
        requestJSON<UploadNotificationTemplate[]>('/api/upload/notification-templates', '通知模板'),
        getRuntimeInfo(),
        getDesktopPreferences(),
        loadRenameHistory(true),
        loadEmbyAPIKeys(true)
      ]);

      setHealthCheckedAt(new Date());
      if (healthResult.status === 'fulfilled') {
        setHealth(healthResult.value);
        setConnectionOnline(true);
      } else {
        setConnectionOnline(false);
      }
      if (configResult.status === 'fulfilled') {
        setConfig(configResult.value);
        setSavedConfig(structuredClone(configResult.value));
      }
      if (toolsResult.status === 'fulfilled') setTools(asArray<ToolStatus>(toolsResult.value));
      if (tasksResult.status === 'fulfilled') applyTaskList(tasksResult.value);
      if (taskRunsResult.status === 'fulfilled') {
        const value = taskRunsResult.value;
        setTaskRuns(Array.isArray(value) ? value : asArray<TaskRun>(value.items));
      }
      if (taskSummaryResult.status === 'fulfilled') setTaskSummary(taskSummaryResult.value);
      if (dirsResult.status === 'fulfilled') setWatchDirs(asArray<WatchDir>(dirsResult.value));
      if (artifactsResult.status === 'fulfilled') setArtifacts(asArray<Artifact>(artifactsResult.value));
      if (providersResult.status === 'fulfilled') setUploadProviders(asArray<UploadProvider>(providersResult.value));
      if (providerTypesResult.status === 'fulfilled') setUploadProviderTypes(asArray<UploadProviderDescriptor>(providerTypesResult.value));
      if (notificationTemplatesResult.status === 'fulfilled') setUploadNotificationTemplates(asArray<UploadNotificationTemplate>(notificationTemplatesResult.value));
      if (runtimeResult.status === 'fulfilled') setRuntimeInfo(runtimeResult.value);
      if (desktopPreferencesResult.status === 'fulfilled') setDesktopPreferences(desktopPreferencesResult.value);

      const failedSections = [
        healthResult.status === 'rejected' && '服务状态',
        configResult.status === 'rejected' && '应用配置',
        toolsResult.status === 'rejected' && '工具状态',
        tasksResult.status === 'rejected' && '任务列表',
        taskRunsResult.status === 'rejected' && '任务批次',
        taskSummaryResult.status === 'rejected' && '任务摘要',
        dirsResult.status === 'rejected' && '媒体目录',
        artifactsResult.status === 'rejected' && '最近产物',
        providersResult.status === 'rejected' && '上传 Provider',
        providerTypesResult.status === 'rejected' && 'Provider 类型',
        notificationTemplatesResult.status === 'rejected' && '通知模板',
        runtimeResult.status === 'rejected' && '桌面运行信息',
        desktopPreferencesResult.status === 'rejected' && '桌面偏好设置',
        renameHistoryResult.status === 'rejected' && '重命名历史',
        embyKeysResult.status === 'rejected' && 'Emby 密钥'
      ].filter((label): label is string => Boolean(label));
      if (failedSections.length) {
        setError(`部分数据未能加载：${failedSections.join('、')}。其余功能仍可使用，请检查后台服务后重试。`);
      }
      setInitialLoading(false);
    }

    void load();
  }, [taskPageSize]);

  useEffect(() => {
    if (!runtimeInfo) return;
    const refreshTaskData = () => {
      void loadTaskSummary();
      if (activePage !== 'tasks') return;
      void loadTasks(taskPage, taskStatusFilter, appliedTaskFilters);
      void loadTaskRuns();
    };
    const unsubscribeDesktop = runtimeInfo.desktop ? subscribeDesktopTaskChanges(refreshTaskData) : null;
    const events = unsubscribeDesktop || typeof EventSource === 'undefined' ? null : new EventSource('/api/tasks/events');
    events?.addEventListener('tasks-changed', refreshTaskData);
    if (activePage === 'tasks') {
      void loadTasks(taskPage, taskStatusFilter, appliedTaskFilters);
      void loadTaskRuns();
    }
    return () => {
      unsubscribeDesktop?.();
      events?.removeEventListener('tasks-changed', refreshTaskData);
      events?.close();
    };
  }, [runtimeInfo?.desktop, activePage, taskPage, taskStatusFilter, taskPageSize, appliedTaskFilters, displayTimezone]);

  useEffect(() => {
    if (!runtimeInfo) return;
    const refreshUploadData = () => {
      if (activePage !== 'uploads') return;
      if (uploadView === 'batches') {
        void loadUploadSummary().catch(() => {});
        void loadUploadBatches(uploadPage, uploadStatusFilter, appliedUploadPathFilter).catch(() => {});
      } else if (uploadView === 'notificationRecords') {
        void loadUploadNotificationRecords(uploadNotificationPage, uploadNotificationStatusFilter, appliedUploadNotificationPathFilter).catch(() => {});
      }
    };
    const unsubscribeDesktop = runtimeInfo.desktop ? subscribeDesktopUploadChanges(refreshUploadData) : null;
    const events = unsubscribeDesktop || typeof EventSource === 'undefined' ? null : new EventSource('/api/uploads/events');
    events?.addEventListener('uploads-changed', refreshUploadData);
    return () => {
      unsubscribeDesktop?.();
      events?.removeEventListener('uploads-changed', refreshUploadData);
      events?.close();
    };
  }, [runtimeInfo?.desktop, activePage, uploadView, uploadPage, uploadStatusFilter, appliedUploadPathFilter, uploadNotificationPage, uploadNotificationStatusFilter, appliedUploadNotificationPathFilter, taskPageSize]);

  useEffect(() => {
    void loadUploadSummary().catch(() => {
      // The upload badge is best effort during the initial connection.
    });
  }, []);

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

  function requestDiscardChanges(dirty: boolean, close: () => void) {
    if (!dirty) {
      close();
      return;
    }
    requestConfirmation({
      title: '放弃当前编辑？',
      message: '当前弹窗中的未提交内容不会保存。',
      confirmLabel: '放弃并关闭',
      tone: 'danger',
      onConfirm: close
    });
  }

  async function acceptConfirmation() {
    if (!confirmation || confirming) return;
    const action = confirmation.onConfirm;
    setConfirming(true);
    try {
      await action();
      setConfirmation(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : '操作失败，请稍后重试');
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
    if (confirmation) return;
    const previousStack = activeModalStackRef.current.split('|').filter(Boolean);
    const currentStack = modalStackKey.split('|').filter(Boolean);
    let commonDepth = 0;
    while (previousStack[previousStack.length - 1 - commonDepth]
      && previousStack[previousStack.length - 1 - commonDepth] === currentStack[currentStack.length - 1 - commonDepth]) commonDepth++;
    const removedDialogs = previousStack.slice(0, previousStack.length - commonDepth);
    const addedDialogs = currentStack.slice(0, currentStack.length - commonDepth);
    const returnTarget = removedDialogs.length ? modalFocusReturnRef.current.get(removedDialogs[removedDialogs.length - 1]) : null;
    for (const key of removedDialogs) modalFocusReturnRef.current.delete(key);
    if (addedDialogs.length) {
      const opener = returnTarget ?? (document.activeElement instanceof HTMLElement ? document.activeElement : null);
      for (const key of addedDialogs) modalFocusReturnRef.current.set(key, opener);
    }
    activeModalStackRef.current = modalStackKey;

    if (!modalStackKey) {
      const returnTimer = window.setTimeout(() => {
        if (returnTarget?.isConnected) returnTarget.focus();
      }, 0);
      return () => window.clearTimeout(returnTimer);
    }
    setError('');
    setNotice('');
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';

    const focusTimer = addedDialogs.length ? window.setTimeout(() => {
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
    }, 0) : window.setTimeout(() => {
      if (returnTarget?.isConnected) returnTarget.focus();
    }, 0);

    function closeTopModal() {
      if (remoteDirectoryPicker) setRemoteDirectoryPicker(null);
      else if (directoryPicker) setDirectoryPicker(null);
      else if (targetPathEditor) setTargetPathEditor(null);
      else if (selectedHistoryBatch) setSelectedHistoryBatch(null);
      else if (renameHistoryOpen) setRenameHistoryOpen(false);
      else if (renameTemplateEditorOpen) setRenameTemplateEditorOpen(false);
      else if (tmdbEpisodeDetail) setTmdbEpisodeDetail(null);
      else if (addEmbyKeyOpen && !modalBusyRef.current.savingEmbyKey) setAddEmbyKeyOpen(false);
      else if (auditTmdbMatchOpen) setAuditTmdbMatchOpen(false);
      else if (tmdbMatchOpen) setTmdbMatchOpen(false);
      else if (batchEpisodeOpen && !modalBusyRef.current.applyingBatchEpisode) setBatchEpisodeOpen(false);
      else if (uploadCookieProvider && !modalBusyRef.current.savingUploadProvider) requestUploadCookieCloseRef.current();
      else if (uploadOpen115Provider && !modalBusyRef.current.savingUploadProvider) requestUploadOpen115CloseRef.current();
      else if (uploadBaiduOpenProvider && !modalBusyRef.current.savingUploadProvider) setUploadBaiduOpenProvider(null);
      else if (uploadBaiduPCSProvider && !modalBusyRef.current.savingUploadProvider) setUploadBaiduPCSProvider(null);
      else if (uploadProviderUsage) setUploadProviderUsage(null);
      else if ((newUploadProviderOpen || uploadProviderModal) && !modalBusyRef.current.savingUploadProvider) { setNewUploadProviderOpen(false); setUploadProviderModal(null); }
      else if ((newUploadNotificationTemplateOpen || uploadNotificationTemplateModal) && !modalBusyRef.current.savingUploadNotificationTemplate) { setNewUploadNotificationTemplateOpen(false); setUploadNotificationTemplateModal(null); }
      else if (editingWatchDir && !modalBusyRef.current.savingWatchDir) setEditingWatchDir(null);
      else if (addWatchDirOpen && !modalBusyRef.current.savingWatchDir) setAddWatchDirOpen(false);
      else if (rescanOpen && !modalBusyRef.current.rescanning) setRescanOpen(false);
      else if (selectedUploadBatch) setSelectedUploadBatch(null);
      else if (selectedTask) setSelectedTask(null);
      else if (recentArtifactsOpen) setRecentArtifactsOpen(false);
    }

    function handleDialogKey(event: KeyboardEvent) {
      const dialogs = Array.from(document.querySelectorAll<HTMLElement>('.modal-card')).filter((dialog) => dialog.offsetParent !== null);
      const activeDialog = dialogs[dialogs.length - 1];
      if (!activeDialog) return;
      if (event.key === 'Escape') {
        event.preventDefault();
        if (activeDialog.dataset.protectDraft === 'true') {
          requestConfirmation({
            title: '放弃当前编辑？',
            message: '当前弹窗中的未提交内容不会保存。',
            confirmLabel: '放弃并关闭',
            tone: 'danger',
            onConfirm: closeTopModal
          });
          return;
        }
        closeTopModal();
        return;
      }
      if (event.key !== 'Tab') return;
      const focusable = Array.from(activeDialog.querySelectorAll<HTMLElement>('button:not(:disabled), [href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])'));
      const alertClose = document.querySelector<HTMLElement>('.error-toast button:not(:disabled)');
      if (alertClose) focusable.push(alertClose);
      if (!focusable.length) {
        event.preventDefault();
        activeDialog.tabIndex = -1;
        activeDialog.focus();
        return;
      }
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (!focusable.includes(document.activeElement as HTMLElement)) {
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
    };
  }, [modalStackKey, confirmation]);

  async function loadRenameHistory(throwOnError = false) {
    setLoadingRenameHistory(true);
    try {
      const response = await fetch('/api/rename/history');
      if (!response.ok) throw new Error(await readErrorMessage(response));
      const result = await response.json();
      setRenameHistory(asArray<RenameHistoryBatch>(result.items));
    } catch (err) {
      if (throwOnError) throw err;
      setError(err instanceof Error ? err.message : '加载重命名历史失败');
    } finally {
      setLoadingRenameHistory(false);
    }
  }

  async function loadEmbyAPIKeys(throwOnError = false) {
    try {
      const response = await fetch('/api/emby-api-keys');
      if (!response.ok) throw new Error(await readErrorMessage(response));
      setAuditEmbyAPIKeys(asArray<EmbyAPIKey>(await response.json()));
    } catch (err) {
      if (throwOnError) throw err;
      setError(err instanceof Error ? err.message : '加载 Emby API Key 失败');
    }
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
    missingAuditRequestRef.current++;
    setMissingAuditReport(null);
    setAuditingMissing(false);
  }, [auditRoot, auditTmdbId, auditIncludeSeasonZero]);

  useEffect(() => {
    embyAuditRequestRef.current++;
    setEmbyAuditReport(null);
    setAuditingEmby(false);
  }, [auditRoot, auditEmbyItemUrl, auditEmbyApiKey, auditSelectedEmbyKeyId]);

  useEffect(() => {
    fileAuditRequestRef.current++;
    setFileAuditReport(null);
    setAuditingFiles(false);
  }, [fileAuditLocalRoot, fileAuditRemoteRoot, fileAuditSFTPAddr, fileAuditSFTPUser, fileAuditSFTPPassword, fileAuditSFTPKeyPath, fileAuditSFTPKnownHostsPath, fileAuditSFTPInsecure, fileAuditAllowSTRM, fileAuditCompareSize, fileAuditCompareMD5]);

  useEffect(() => {
    function handlePopState() {
      const nextPath = window.location.pathname;
      const nextPage = pageFromPath(nextPath);
      const nextUploadView = uploadViewFromPath(nextPath);
      if (allowSettingsHistoryNavigationRef.current) {
        allowSettingsHistoryNavigationRef.current = false;
        setActivePage(nextPage);
        setUploadView(nextUploadView);
        return;
      }
      if (activePage === 'settings' && nextPage !== 'settings' && configDirty) {
        window.history.pushState(null, '', pagePaths.settings);
        requestConfirmation({
          title: '放弃设置修改？',
          message: '当前页面包含尚未保存的修改。离开后这些修改将无法恢复。',
          confirmLabel: '放弃并离开',
          tone: 'danger',
          onConfirm: () => {
            discardConfigChanges();
            allowSettingsHistoryNavigationRef.current = true;
            window.history.back();
          }
        });
        return;
      }
      setActivePage(nextPage);
      setUploadView(nextUploadView);
    }

    window.addEventListener('popstate', handlePopState);
    return () => window.removeEventListener('popstate', handlePopState);
  }, [activePage, configDirty, uploadView]);

  useEffect(() => {
    const content = workspaceContentRef.current;
    if (!content) return;
    content.scrollTo({ top: 0, behavior: 'auto' });
  }, [activePage, uploadView]);

  useEffect(() => {
    if (!previewingRename || renamePreviewSignature === renameInputSignature) return;
    renamePreviewAbortRef.current?.abort();
  }, [previewingRename, renamePreviewSignature, renameInputSignature]);

  useEffect(() => {
    if (!renameLanguageInitialized && config?.scraping.language) {
      setRenameLanguage(config.scraping.language);
      setRenameLanguageInitialized(true);
    }
  }, [config?.scraping.language, renameLanguageInitialized]);

  function navigate(page: PageKey) {
    if (page === activePage) {
      if (page === 'uploads' && uploadView !== 'batches') navigateUploadView('batches');
      return;
    }
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
    if (page === 'uploads') setUploadView('batches');
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
      setSelectedTaskIds((ids) => ids.filter((id) => value.some((task) => task.id === id)));
      return;
    }
    const items = asArray<Task>(value?.items);
    setTasks(items);
    setTaskTotal(value?.total ?? 0);
    setTaskPage(value?.page ?? 1);
    setSelectedTaskIds((ids) => ids.filter((id) => items.some((task) => task.id === id)));
  }

  async function loadTaskSummary() {
    try {
      const response = await fetch('/api/tasks/summary', { cache: 'no-store' });
      if (!response.ok) return;
      setTaskSummary(await response.json() as TaskSummary);
    } catch {
      // The health poll owns connection status; keep the last known task summary here.
    }
  }

  async function loadTasks(page = taskPage, status = taskStatusFilter, filters = appliedTaskFilters) {
    const requestID = ++taskListRequestRef.current;
    try {
      const params = new URLSearchParams({ page: String(page), pageSize: String(taskPageSize) });
      if (filters.scanRunId) params.set('scanRunId', filters.scanRunId);
      if (filters.path.trim()) params.set('path', filters.path.trim());
      if (status !== 'all') params.set('status', status);
      if (filters.from) params.set('from', zonedInputToUTC(filters.from, displayTimezone, false));
      if (filters.to) params.set('to', zonedInputToUTC(filters.to, displayTimezone, true));
      const response = await fetch(`/api/tasks?${params.toString()}`, { cache: 'no-store' });
      if (!response.ok) {
        if (requestID === taskListRequestRef.current) setError(await readErrorMessage(response));
        return;
      }
      const value = await response.json() as TaskListResponse | Task[];
      if (requestID !== taskListRequestRef.current) return;
      applyTaskList(value);
      void loadTaskSummary();
    } catch (err) {
      if (requestID === taskListRequestRef.current) {
        setError(err instanceof Error ? err.message : '刷新任务列表失败');
      }
    }
  }

  async function loadTaskRuns() {
    try {
      const response = await fetch('/api/tasks/runs?limit=200', { cache: 'no-store' });
      if (!response.ok) return;
      const value = await response.json() as TaskRun[] | TaskRunListResponse;
      setTaskRuns(Array.isArray(value) ? value : asArray<TaskRun>(value.items));
    } catch {
      // Keep the last known batch choices while the main health check handles connectivity.
    }
  }

  function applyUploadBatchList(value: UploadBatchListResponse) {
    const items = asArray<UploadBatch>(value.items);
    observeUploadStatuses(items);
    setUploadBatches(items);
    setUploadTotal(value.total ?? 0);
    setUploadPage(value.page ?? 1);
  }

  function applyUploadNotificationFilters() {
    const path = uploadNotificationPathFilter.trim();
    setAppliedUploadNotificationPathFilter(path);
    void refreshUploadNotificationRecords(1, uploadNotificationStatusFilter, path);
  }

  function resetUploadNotificationFilters() {
    setUploadNotificationPathFilter('');
    setAppliedUploadNotificationPathFilter('');
    setUploadNotificationStatusFilter('all');
    void refreshUploadNotificationRecords(1, 'all', '');
  }

  function observeTaskRunStatuses(items: TaskRun[], baselineStartedAt: number) {
    const observed = observedTaskRunStatusesRef.current;
    const notified = notifiedTaskRunIDsRef.current;
    const establishingBaseline = !taskRunNotificationBaselineReadyRef.current;
    taskRunNotificationBaselineReadyRef.current = true;

    for (const run of items) {
      const previous = observed.get(run.id);
      observed.set(run.id, run.status);
      if (!isTaskRunTerminal(run.status)) continue;
      if (notified.has(run.id)) continue;
      const updatedAt = parseStoredTime(run.updatedAt)?.getTime() ?? 0;
      if (establishingBaseline && updatedAt < baselineStartedAt - 1000) {
        notified.add(run.id);
        continue;
      }
      if (previous !== undefined && !isTaskRunActive(previous)) {
        notified.add(run.id);
        continue;
      }
      if (run.status === 'empty' && !run.errorSummary) {
        notified.add(run.id);
        continue;
      }

      const scopeName = run.scopePath.split(/[\\/]/).filter(Boolean).pop() || run.scopePath || '媒体扫描';
      const counts = run.total === 0 ? ['扫描未完成'] : [`共 ${run.total} 个文件`, `完成 ${run.completed}`];
      if (run.total > 0 && run.failed > 0) counts.push(`失败 ${run.failed}`);
      if (run.total > 0 && run.canceled > 0) counts.push(`取消 ${run.canceled}`);
      if (run.total > 0 && run.ignored > 0) counts.push(`忽略 ${run.ignored}`);
      const needsAttention = run.failed > 0 || run.status === 'failed' || run.status === 'partial';
      const canceled = run.status === 'canceled';
      const title = needsAttention ? '媒体批次需要处理' : canceled ? '媒体批次已取消' : '媒体批次完成';
      const summary = run.errorSummary.trim().replace(/\s+/g, ' ');
      const detail = summary ? `；${summary.slice(0, 160)}${summary.length > 160 ? '…' : ''}` : '';
      notified.add(run.id);
      void notifyDesktop(title, `${scopeName}：${counts.join('，')}${detail}`).catch(() => {});
    }
  }

  function navigateUploadView(view: UploadView) {
    setActivePage('uploads');
    setUploadView(view);
    const path = view === 'providers'
      ? '/uploads/providers'
      : view === 'notifications'
        ? '/uploads/notifications'
        : view === 'notificationRecords'
          ? '/uploads/notification-records'
          : '/uploads';
    if (window.location.pathname !== path) window.history.pushState(null, '', path);
  }

  function observeUploadStatuses(items: UploadBatch[]) {
    const observed = observedUploadStatusesRef.current;
    for (const batch of items) {
      const previous = observed.get(batch.id);
      observed.set(batch.id, batch.status);
      if (!previous || previous === batch.status || !['completed', 'partial', 'failed'].includes(batch.status)) continue;
      const failed = batch.failedTargets > 0 || batch.status === 'failed';
      const partiallyCompleted = batch.status === 'partial' && !failed;
      const name = batch.seriesPath.split(/[\\/]/).pop() || batch.seriesKey || `上传批次 #${batch.id}`;
      const title = failed ? '上传批次需要处理' : partiallyCompleted ? '上传批次部分完成' : '上传批次完成';
      const message = failed && batch.failedTargets > 0 ? `${name}：${batch.failedTargets} 个目标失败` : name;
      void notifyDesktop(title, message).catch(() => {});
    }
  }

  async function loadUploadProviders() {
    const response = await fetch('/api/upload/providers');
    if (!response.ok) throw new Error(await readErrorMessage(response));
    setUploadProviders(asArray<UploadProvider>(await response.json()));
  }

  async function loadWatchDirs() {
    const response = await fetch('/api/watch-dirs');
    if (!response.ok) throw new Error(await readErrorMessage(response));
    setWatchDirs(asArray<WatchDir>(await response.json()));
  }

  async function loadUploadProviderTypes() {
    const response = await fetch('/api/upload/provider-types');
    if (!response.ok) throw new Error(await readErrorMessage(response));
    setUploadProviderTypes(asArray<UploadProviderDescriptor>(await response.json()));
  }

  async function loadUploadNotificationTemplates() {
    const response = await fetch('/api/upload/notification-templates');
    if (!response.ok) throw new Error(await readErrorMessage(response));
    setUploadNotificationTemplates(asArray<UploadNotificationTemplate>(await response.json()));
  }

  async function loadUploadNotificationRecords(page = uploadNotificationPage, status = uploadNotificationStatusFilter, path = appliedUploadNotificationPathFilter) {
    const requestID = ++uploadNotificationRequestRef.current;
    const params = new URLSearchParams({ page: String(page), pageSize: String(taskPageSize) });
    if (status !== 'all') params.set('status', status);
    if (path.trim()) params.set('path', path.trim());
    const response = await fetch(`/api/upload/notifications?${params.toString()}`, { cache: 'no-store' });
    if (!response.ok) throw new Error(await readErrorMessage(response));
    const value = await response.json() as UploadNotificationRecordListResponse;
    if (requestID !== uploadNotificationRequestRef.current) return;
    setUploadNotificationRecords(asArray<UploadNotificationRecord>(value.items));
    setUploadNotificationTotal(value.total ?? 0);
    setUploadNotificationPage(value.page ?? 1);
  }

  async function refreshUploadNotificationRecords(page = uploadNotificationPage, status = uploadNotificationStatusFilter, path = appliedUploadNotificationPathFilter) {
    if (refreshingUploadNotificationsRef.current) return;
    refreshingUploadNotificationsRef.current = true;
    setRefreshingUploadNotifications(true);
    try {
      await loadUploadNotificationRecords(page, status, path);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载通知记录失败');
    } finally {
      refreshingUploadNotificationsRef.current = false;
      setRefreshingUploadNotifications(false);
    }
  }

  async function saveUploadNotificationTemplate(template: UploadNotificationTemplate) {
    setSavingUploadNotificationTemplate(true);
    setError('');
    try {
      const isNew = template.id === 0;
      const response = await fetch(isNew ? '/api/upload/notification-templates' : `/api/upload/notification-templates/${template.id}`, {
        method: isNew ? 'POST' : 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(template)
      });
      if (!response.ok) throw new Error(await readErrorMessage(response));
      setUploadNotificationTemplateModal(null);
      setNewUploadNotificationTemplateOpen(false);
      setNotice(isNew ? '通知模板已添加。' : '通知模板已保存。');
      await Promise.all([loadUploadNotificationTemplates(), loadWatchDirs()]);
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存通知模板失败');
    } finally {
      setSavingUploadNotificationTemplate(false);
    }
  }

  function deleteUploadNotificationTemplate(template: UploadNotificationTemplate) {
    requestConfirmation({
      title: `删除“${template.name}”？`,
      message: '正在被上传配置或活动批次使用的模板不能删除。删除后无法恢复。',
      confirmLabel: '删除模板',
      tone: 'danger',
      onConfirm: async () => {
        const response = await fetch(`/api/upload/notification-templates/${template.id}`, { method: 'DELETE' });
        if (!response.ok) throw new Error(await readErrorMessage(response));
        setNotice('通知模板已删除。');
        await loadUploadNotificationTemplates();
      }
    });
  }

  async function loadUploadSummary() {
    const response = await fetch('/api/uploads/summary', { cache: 'no-store' });
    if (!response.ok) throw new Error(await readErrorMessage(response));
    setUploadSummary(await response.json() as UploadSummary);
  }

  async function loadUploadBatches(page = uploadPage, status = uploadStatusFilter, path = appliedUploadPathFilter) {
    const requestID = ++uploadListRequestRef.current;
    const params = new URLSearchParams({ page: String(page), pageSize: String(taskPageSize) });
    if (status !== 'all') params.set('status', status);
    if (path.trim()) params.set('path', path.trim());
    const response = await fetch(`/api/uploads?${params.toString()}`, { cache: 'no-store' });
    if (!response.ok) throw new Error(await readErrorMessage(response));
    const value = await response.json() as UploadBatchListResponse;
    if (requestID !== uploadListRequestRef.current) return;
    applyUploadBatchList(value);
  }

  async function refreshUploads(page = uploadPage, status = uploadStatusFilter, path = appliedUploadPathFilter) {
    if (refreshingUploadsRef.current) return;
    refreshingUploadsRef.current = true;
    setRefreshingUploads(true);
    try {
      await Promise.all([loadUploadSummary(), loadUploadProviders(), loadUploadProviderTypes(), loadUploadBatches(page, status, path)]);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载上传管理失败');
    } finally {
      refreshingUploadsRef.current = false;
      setRefreshingUploads(false);
    }
  }

  async function refreshUploadProviders() {
    if (refreshingUploadProvidersRef.current) return;
    refreshingUploadProvidersRef.current = true;
    setRefreshingUploadProviders(true);
    try {
      await Promise.all([loadUploadProviders(), loadUploadProviderTypes(), loadWatchDirs()]);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载 Provider 账号失败');
    } finally {
      refreshingUploadProvidersRef.current = false;
      setRefreshingUploadProviders(false);
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

  function browseRemoteDirectory(request: RemoteDirectoryPickerRequest) {
    setError('');
    setRemoteDirectoryPicker(request);
  }

  function clearRemoteDirectoryCache(providerID: number) {
    const prefix = `${providerID}:`;
    for (const key of remoteDirectoryCacheRef.current.keys()) {
      if (key.startsWith(prefix)) remoteDirectoryCacheRef.current.delete(key);
    }
    for (const key of remoteDirectoryRequestCacheRef.current.keys()) {
      if (key.startsWith(prefix)) remoteDirectoryRequestCacheRef.current.delete(key);
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

  async function loadUploadBatchDetail(batchID: number, options: { open?: boolean; signal?: AbortSignal } = {}) {
    const requestID = ++uploadDetailRequestRef.current;
    const open = options.open ?? true;
    try {
      const response = await fetch(`/api/uploads/${batchID}`, { cache: 'no-store', signal: options.signal });
      if (!response.ok) throw new Error(await readErrorMessage(response));
      const detail = await response.json() as UploadBatchDetail;
      if (requestID !== uploadDetailRequestRef.current) return;
      if (open) {
        setSelectedUploadBatch(detail);
      } else {
        setSelectedUploadBatch((current) => current?.batch.id === batchID ? detail : current);
      }
    } catch (err) {
      if (options.signal?.aborted || requestID !== uploadDetailRequestRef.current) return;
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
      if (!response.ok) throw new Error(await readErrorMessage(response));
      const saved = await response.json() as UploadProvider;
      if (!isNew) clearRemoteDirectoryCache(saved.id);
      setUploadProviderModal(null);
      setNewUploadProviderOpen(false);
      setNotice(isNew ? 'Provider 已添加。' : 'Provider 已保存。');
      await Promise.all([loadUploadProviders(), loadUploadProviderTypes()]);
      if (isNew && saved.type === '115cookie' && !saved.hasCookie) {
        setUploadCookieProvider(saved);
        setUploadCookieValue('');
        setUploadCookieDevice(saved.authDevice || preferredUploadAuthDevice(saved.type, uploadProviderTypes));
        setCookieAuth(null);
      }
      if (isNew && saved.type === '115open' && !saved.hasCredentials) {
        setUploadOpen115Provider(saved);
        setOpen115ClientID('');
        setOpen115Auth(null);
      }
      if (isNew && saved.type === 'baidupan' && !saved.hasCredentials) {
        openBaiduOpenCredentials(saved);
      }
      if (isNew && saved.type === 'baidupcs' && !saved.hasCookie) {
        openBaiduPCSAuthorization(saved);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存 Provider 失败');
    } finally {
      setSavingUploadProvider(false);
    }
  }

  function openUploadAuthorization(provider: UploadProvider) {
    if (provider.type === '115open') {
      openUploadOpen115Authorization(provider);
      return;
    }
    if (provider.type === 'baidupan') {
      openBaiduOpenCredentials(provider);
      return;
    }
    if (provider.type === 'baidupcs') {
      openBaiduPCSAuthorization(provider);
      return;
    }
    openUploadCookieAuthorization(provider);
  }

  function openBaiduPCSAuthorization(provider: UploadProvider) {
    setUploadBaiduPCSProvider(provider);
    setBaiduPCSCredentials({ cookie: '', bdstoken: '' });
    setShowBaiduPCSSecrets(false);
  }

  async function saveBaiduPCSAuthorization() {
    if (!uploadBaiduPCSProvider) return;
    const values: Array<[string, string]> = [
      ['cookie', baiduPCSCredentials.cookie],
      ['bdstoken', baiduPCSCredentials.bdstoken]
    ].filter(([, value]) => Boolean(value.trim())) as Array<[string, string]>;
    if (!values.length) return;
    setSavingUploadProvider(true);
    setError('');
    try {
      for (const [key, value] of values) {
        const response = await fetch(`/api/upload/providers/${uploadBaiduPCSProvider.id}/secrets/${key}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ value: value.trim() })
        });
        if (!response.ok) throw new Error(await readErrorMessage(response));
      }
      clearRemoteDirectoryCache(uploadBaiduPCSProvider.id);
      setUploadBaiduPCSProvider(null);
      setNotice('Baidu Pan Web credentials saved');
      await loadUploadProviders();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save Baidu Pan Web credentials');
    } finally {
      setSavingUploadProvider(false);
    }
  }

  function openBaiduOpenCredentials(provider: UploadProvider) {
    setUploadBaiduOpenProvider(provider);
    setBaiduOpenCredentials({ clientID: '', clientSecret: '', accessToken: '', refreshToken: '', accessTokenExpiresAt: '', brokerBaseURL: '', brokerClientID: '', brokerToken: '' });
    setBaiduOpenMode('official');
    setBaiduOpenAuth(null);
    setBaiduOpenAuthConfig(null);
    setShowBaiduOpenTokens(false);
    void loadBaiduOpenAuthConfig(provider);
  }

  async function loadBaiduOpenAuthConfig(provider: UploadProvider) {
    try {
      const response = await fetch(`/api/upload/providers/${provider.id}/auth/baiduopen`);
      if (!response.ok) throw new Error(await readErrorMessage(response));
      const config = await response.json() as BaiduOpenAuthConfig;
      setBaiduOpenAuthConfig(config);
      setBaiduOpenMode(config.authMode || 'official');
      setBaiduOpenCredentials((current) => ({ ...current, clientID: config.clientId || '', brokerBaseURL: config.brokerBaseUrl || '', brokerClientID: config.brokerClientId || '' }));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load Baidu Open authorization settings');
    }
  }

  async function saveBaiduOpenCredentialsLegacy() {
    if (!uploadBaiduOpenProvider) return;
    const values: Array<[string, string]> = [
      ['client_id', baiduOpenCredentials.clientID],
      ['client_secret', baiduOpenCredentials.clientSecret],
      ['access_token', baiduOpenCredentials.accessToken],
      ['refresh_token', baiduOpenCredentials.refreshToken],
      ['access_token_expires_at', baiduOpenCredentials.accessTokenExpiresAt]
    ].filter(([, value]) => Boolean(value.trim())) as Array<[string, string]>;
    if (!values.length) return;
    setSavingUploadProvider(true);
    setError('');
    try {
      for (const [key, value] of values) {
        const response = await fetch(`/api/upload/providers/${uploadBaiduOpenProvider.id}/secrets/${key}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ value: value.trim() })
        });
        if (!response.ok) throw new Error(await readErrorMessage(response));
      }
      clearRemoteDirectoryCache(uploadBaiduOpenProvider.id);
      setUploadBaiduOpenProvider(null);
      setNotice('百度网盘 Open 凭据已保存。');
      await loadUploadProviders();
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存百度网盘 Open 凭据失败');
    } finally {
      setSavingUploadProvider(false);
    }
  }

  async function saveBaiduOpenApplication() {
    if (!uploadBaiduOpenProvider || !baiduOpenCredentials.clientID.trim() || !baiduOpenCredentials.clientSecret.trim()) return;
    setSavingUploadProvider(true);
    setError('');
    try {
      const response = await fetch(`/api/upload/providers/${uploadBaiduOpenProvider.id}/auth/baiduopen`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ clientId: baiduOpenCredentials.clientID.trim(), clientSecret: baiduOpenCredentials.clientSecret.trim() })
      });
      if (!response.ok) throw new Error(await readErrorMessage(response));
      setBaiduOpenAuthConfig(await response.json() as BaiduOpenAuthConfig);
      clearRemoteDirectoryCache(uploadBaiduOpenProvider.id);
      await loadUploadProviders();
      setNotice('Baidu Open application credentials saved');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save Baidu Open application credentials');
    } finally {
      setSavingUploadProvider(false);
    }
  }

  async function saveBaiduOpenAuthorizationSettings() {
    if (!uploadBaiduOpenProvider) return;
    setSavingUploadProvider(true);
    setError('');
    try {
      const modeResponse = await fetch(`/api/upload/providers/${uploadBaiduOpenProvider.id}/auth/baiduopen/mode`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ mode: baiduOpenMode })
      });
      if (!modeResponse.ok) throw new Error(await readErrorMessage(modeResponse));
      if (baiduOpenMode !== 'official' || baiduOpenCredentials.brokerBaseURL.trim() || baiduOpenCredentials.brokerClientID.trim() || baiduOpenCredentials.brokerToken.trim()) {
        const brokerResponse = await fetch(`/api/upload/providers/${uploadBaiduOpenProvider.id}/auth/baiduopen/broker`, {
          method: 'PUT', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ baseUrl: baiduOpenCredentials.brokerBaseURL.trim(), clientId: baiduOpenCredentials.brokerClientID.trim(), token: baiduOpenCredentials.brokerToken.trim() })
        });
        if (!brokerResponse.ok) throw new Error(await readErrorMessage(brokerResponse));
      }
      await loadBaiduOpenAuthConfig(uploadBaiduOpenProvider);
      setNotice('Baidu Open authorization settings saved');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save Baidu Open authorization settings');
    } finally {
      setSavingUploadProvider(false);
    }
  }

  async function startBaiduOpenAuthorization() {
    if (!uploadBaiduOpenProvider) return;
    setSavingUploadProvider(true);
    setError('');
    try {
      if (baiduOpenMode !== 'broker_token_exchange' && baiduOpenCredentials.clientID.trim() && baiduOpenCredentials.clientSecret.trim()) {
        const credentialsResponse = await fetch(`/api/upload/providers/${uploadBaiduOpenProvider.id}/auth/baiduopen`, {
          method: 'PUT', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ clientId: baiduOpenCredentials.clientID.trim(), clientSecret: baiduOpenCredentials.clientSecret.trim() })
        });
        if (!credentialsResponse.ok) throw new Error(await readErrorMessage(credentialsResponse));
      }
      await saveBaiduOpenAuthorizationSettings();
      const response = await fetch(`/api/upload/providers/${uploadBaiduOpenProvider.id}/auth/baiduopen`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ mode: baiduOpenMode })
      });
      if (!response.ok) throw new Error(await readErrorMessage(response));
      const auth = await response.json() as BaiduOpenAuthStatus;
      setBaiduOpenAuth(auth);
      if (auth.authorizationUrl) window.open(auth.authorizationUrl, '_blank', 'noopener,noreferrer');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to start Baidu Open authorization');
    } finally {
      setSavingUploadProvider(false);
    }
  }

  async function importBaiduOpenTokens() {
    if (!uploadBaiduOpenProvider || !baiduOpenCredentials.accessToken.trim() || !baiduOpenCredentials.refreshToken.trim()) return;
    setSavingUploadProvider(true);
    setError('');
    try {
      const response = await fetch(`/api/upload/providers/${uploadBaiduOpenProvider.id}/auth/baiduopen/tokens`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ accessToken: baiduOpenCredentials.accessToken.trim(), refreshToken: baiduOpenCredentials.refreshToken.trim() })
      });
      if (!response.ok) throw new Error(await readErrorMessage(response));
      setBaiduOpenAuthConfig(await response.json() as BaiduOpenAuthConfig);
      setBaiduOpenCredentials((current) => ({ ...current, accessToken: '', refreshToken: '', accessTokenExpiresAt: '' }));
      setBaiduOpenAuth(null);
      clearRemoteDirectoryCache(uploadBaiduOpenProvider.id);
      await loadUploadProviders();
      setNotice('Baidu Open tokens imported');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to import Baidu Open tokens');
    } finally {
      setSavingUploadProvider(false);
    }
  }

  function openUploadCookieAuthorization(provider: UploadProvider) {
    setUploadCookieProvider(provider);
    setUploadCookieValue('');
    setUploadCookieDevice(provider.authDevice || preferredUploadAuthDevice(provider.type, uploadProviderTypes));
    setCookieAuth(null);
  }

  function dismissUploadCookieModal() {
    setUploadCookieProvider(null);
    setCookieAuth(null);
    setUploadCookieValue('');
  }

  function requestCloseUploadCookieModal() {
    if (savingUploadProvider) return;
    if (isCookieAuthorizationActive(cookieAuth)) {
      requestConfirmation({
        title: '结束二维码授权？',
        message: '关闭后会停止检查授权状态，已经扫码的结果可能无法保存。建议等待授权成功或二维码失效后再关闭。',
        confirmLabel: '结束并关闭',
        tone: 'danger',
        onConfirm: dismissUploadCookieModal
      });
      return;
    }
    dismissUploadCookieModal();
  }

  function openUploadOpen115Authorization(provider: UploadProvider) {
    setUploadOpen115Provider(provider);
    setOpen115ClientID('');
    setOpen115Auth(null);
    setOpen115Tokens({ accessToken: '', refreshToken: '' });
    setShowOpen115Tokens(false);
  }

  function dismissUploadOpen115Modal() {
    setUploadOpen115Provider(null);
    setOpen115ClientID('');
    setOpen115Auth(null);
    setOpen115Tokens({ accessToken: '', refreshToken: '' });
    setShowOpen115Tokens(false);
  }

  function requestCloseUploadOpen115Modal() {
    if (savingUploadProvider) return;
    if (isOpen115AuthorizationActive(open115Auth)) {
      requestConfirmation({
        title: '结束 115 Open 授权？',
        message: '关闭后会停止检查授权状态，已经扫码的结果可能无法保存。建议等待授权成功或二维码失效后再关闭。',
        confirmLabel: '结束并关闭',
        tone: 'danger',
        onConfirm: dismissUploadOpen115Modal
      });
      return;
    }
    dismissUploadOpen115Modal();
  }

  async function startOpen115Auth() {
    if (!uploadOpen115Provider || (!open115ClientID.trim() && !uploadOpen115Provider.hasCredentials)) return;
    setSavingUploadProvider(true);
    try {
      const response = await fetch('/api/upload/providers/' + uploadOpen115Provider.id + '/auth/115open', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ clientId: open115ClientID.trim() })
      });
      if (!response.ok) throw new Error(await readErrorMessage(response));
      setOpen115Auth(await response.json() as Open115AuthStatus);
    } catch (err) {
      setError(err instanceof Error ? err.message : '启动 115 Open 授权失败');
    } finally {
      setSavingUploadProvider(false);
    }
  }

  async function importOpen115Tokens() {
    if (!uploadOpen115Provider || (!open115Tokens.accessToken.trim() && !open115Tokens.refreshToken.trim())) return;
    setSavingUploadProvider(true);
    try {
      const response = await fetch('/api/upload/providers/' + uploadOpen115Provider.id + '/auth/115open', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          clientId: open115ClientID.trim(),
          accessToken: open115Tokens.accessToken.trim(),
          refreshToken: open115Tokens.refreshToken.trim()
        })
      });
      if (!response.ok) throw new Error(await readErrorMessage(response));
      const saved = await response.json() as UploadProvider;
      clearRemoteDirectoryCache(saved.id);
      setUploadOpen115Provider(saved);
      setOpen115Tokens({ accessToken: '', refreshToken: '' });
      setShowOpen115Tokens(false);
      setOpen115Auth(null);
      setNotice('115 Open 第三方 Token 已保存。');
      await loadUploadProviders();
    } catch (err) {
      setError(err instanceof Error ? err.message : '导入 115 Open Token 失败');
    } finally {
      setSavingUploadProvider(false);
    }
  }
  function deleteUploadProvider(provider: UploadProvider) {
    requestConfirmation({
      title: `删除“${provider.name}”？`,
      message: '引用该 Provider 的目录上传配置会一并移除。已有上传历史会阻止删除，通常更建议先停用 Provider。',
      confirmLabel: '删除 Provider',
      tone: 'danger',
      onConfirm: () => performDeleteUploadProvider(provider)
    });
  }

  async function performDeleteUploadProvider(provider: UploadProvider) {
    try {
      const response = await fetch(`/api/upload/providers/${provider.id}`, { method: 'DELETE' });
      if (response.status === 409) throw new Error('该 Provider 已有关联的上传历史，不能删除。请编辑并停用它。');
      if (!response.ok) throw new Error(await readErrorMessage(response));
      clearRemoteDirectoryCache(provider.id);
      setNotice('Provider 已删除，关联的目录上传配置已移除。');
      await Promise.all([refreshUploads(), loadWatchDirs()]);
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除 Provider 失败');
    }
  }

  async function saveUploadCookie() {
    if (!uploadCookieProvider || !uploadCookieValue.trim()) return;
    setSavingUploadProvider(true);
    try {
      const response = await fetch(`/api/upload/providers/${uploadCookieProvider.id}/cookie`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ cookie: uploadCookieValue.trim(), authDevice: uploadCookieDevice })
      });
      if (!response.ok) throw new Error(await readErrorMessage(response));
      const saved = await response.json() as UploadProvider;
      clearRemoteDirectoryCache(saved.id);
      setUploadCookieProvider(saved);
      setUploadCookieValue('');
      setCookieAuth(null);
      setNotice(`115 Cookie 已保存，授权设备：${uploadAuthDeviceName(saved.authDevice, saved.type, uploadProviderTypes)}。`);
      await loadUploadProviders();
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存 115 Cookie 失败');
    } finally {
      setSavingUploadProvider(false);
    }
  }

  async function startCookieAuth() {
    if (!uploadCookieProvider) return;
    setSavingUploadProvider(true);
    try {
      const response = await fetch(`/api/upload/providers/${uploadCookieProvider.id}/auth/115cookie`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ terminal: uploadCookieDevice })
      });
      if (!response.ok) throw new Error(await readErrorMessage(response));
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
      if (!response.ok) throw new Error(await readErrorMessage(response));
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
      if (!response.ok) throw new Error(await readErrorMessage(response));
      setNotice(action === 'retry' ? '上传目标已重新排队。' : '上传目标已取消。');
      if (selectedUploadBatch) await loadUploadBatchDetail(selectedUploadBatch.batch.id, { open: false });
      await refreshUploads();
    } catch (err) {
      setError(err instanceof Error ? err.message : '更新上传目标失败');
    } finally {
      setUploadTargetActionID(null);
    }
  }

  useEffect(() => {
    if (activePage !== 'uploads') return;
    if (uploadView === 'providers') {
      void refreshUploadProviders();
      return;
    }
    if (uploadView === 'notifications') {
      void loadUploadNotificationTemplates().catch((err) => setError(err instanceof Error ? err.message : '加载通知模板失败'));
      return;
    }
    if (uploadView === 'notificationRecords') {
      void refreshUploadNotificationRecords(uploadNotificationPage, uploadNotificationStatusFilter, appliedUploadNotificationPathFilter);
      return;
    }
    void refreshUploads(uploadPage, uploadStatusFilter, appliedUploadPathFilter);
  }, [activePage, uploadView, uploadPage, uploadStatusFilter, appliedUploadPathFilter, uploadNotificationPage, uploadNotificationStatusFilter, appliedUploadNotificationPathFilter, taskPageSize]);

  useEffect(() => {
    if (!runtimeInfo?.desktop) return;
    let active = true;
    let polling = false;
    let abortController: AbortController | null = null;
    const baselineStartedAt = Date.now();
    async function pollDesktopNotifications() {
      if (polling) return;
      polling = true;
      abortController = new AbortController();
      try {
        const [taskRunsResult, uploadsResult] = await Promise.allSettled([
          fetch('/api/tasks/runs?limit=50', { signal: abortController.signal }).then(async (response) => response.ok ? await response.json() as TaskRun[] | TaskRunListResponse : null),
          fetch('/api/uploads?page=1&pageSize=50', { signal: abortController.signal }).then(async (response) => response.ok ? await response.json() as UploadBatchListResponse : null)
        ]);
        if (!active) return;
        if (taskRunsResult.status === 'fulfilled' && taskRunsResult.value) {
          const value = taskRunsResult.value;
          observeTaskRunStatuses(Array.isArray(value) ? value : asArray<TaskRun>(value.items), baselineStartedAt);
        }
        if (uploadsResult.status === 'fulfilled' && uploadsResult.value) {
          observeUploadStatuses(asArray<UploadBatch>(uploadsResult.value.items));
        }
      } catch {
        // Notification polling must not replace the main connection status.
      } finally {
        polling = false;
        abortController = null;
      }
    }
    void pollDesktopNotifications();
    const interval = window.setInterval(() => void pollDesktopNotifications(), desktopNotificationPollIntervalMs);
    return () => {
      active = false;
      abortController?.abort();
      window.clearInterval(interval);
    };
  }, [runtimeInfo?.desktop]);

  useEffect(() => {
    if (!selectedUploadBatch) return;
    const batchID = selectedUploadBatch.batch.id;
    let active = true;
    let timer: number | undefined;
    let abortController: AbortController | null = null;
    async function pollUploadBatchDetail() {
      abortController = new AbortController();
      await loadUploadBatchDetail(batchID, { open: false, signal: abortController.signal });
      abortController = null;
      if (active) timer = window.setTimeout(() => void pollUploadBatchDetail(), uploadDetailRefreshIntervalMs);
    }
    timer = window.setTimeout(() => void pollUploadBatchDetail(), uploadDetailRefreshIntervalMs);
    return () => {
      active = false;
      if (timer !== undefined) window.clearTimeout(timer);
      abortController?.abort();
    };
  }, [selectedUploadBatch?.batch.id]);

  useEffect(() => {
    if (!cookieAuth || !uploadCookieProvider || ['authorized', 'expired', 'cancelled', 'error'].includes(cookieAuth.state)) return;
    let active = true;
    const providerID = uploadCookieProvider.id;
    const sessionID = cookieAuth.sessionId;
    let timer: number | undefined;
    async function pollAuthorization() {
      try {
        const response = await fetch(`/api/upload/providers/${providerID}/auth/115cookie?sessionId=${encodeURIComponent(sessionID)}`);
        if (!response.ok) throw new Error(await readErrorMessage(response));
        const next = await response.json() as CookieAuthStatus;
        if (!active) {
          // The backend may persist the cookie while this request is in flight after the modal closes.
          if (next.state === 'authorized') {
            clearRemoteDirectoryCache(providerID);
            await loadUploadProviders();
          }
          return;
        }
        setCookieAuth(next);
        if (next.state === 'authorized') {
          clearRemoteDirectoryCache(providerID);
          setUploadCookieProvider((current) => current && current.id === providerID ? { ...current, hasCookie: true, authDevice: next.terminal } : current);
          setNotice(`115 Cookie 授权成功，设备：${uploadAuthDeviceName(next.terminal, '115cookie', uploadProviderTypes)}。`);
          await loadUploadProviders();
        }
      } catch (err) {
        if (active) setError(err instanceof Error ? err.message : '查询二维码授权状态失败');
      } finally {
        if (active) timer = window.setTimeout(() => void pollAuthorization(), 2000);
      }
    }
    timer = window.setTimeout(() => void pollAuthorization(), 2000);
    return () => {
      active = false;
      if (timer != null) window.clearTimeout(timer);
    };
  }, [cookieAuth?.sessionId, cookieAuth?.state, uploadCookieProvider?.id]);

  useEffect(() => {
    if (!open115Auth || !uploadOpen115Provider || ['authorized', 'expired', 'cancelled', 'error'].includes(open115Auth.state)) return;
    let active = true;
    const providerID = uploadOpen115Provider.id;
    const sessionID = open115Auth.sessionId;
    let timer: number | undefined;
    async function pollOpen115Authorization() {
      let terminal = false;
      try {
        const response = await fetch('/api/upload/providers/' + providerID + '/auth/115open?sessionId=' + encodeURIComponent(sessionID));
        if (!response.ok) throw new Error(await readErrorMessage(response));
        const next = await response.json() as Open115AuthStatus;
        terminal = ['authorized', 'expired', 'cancelled', 'error'].includes(next.state);
        if (!active) {
          if (next.state === 'authorized') {
            clearRemoteDirectoryCache(providerID);
            await loadUploadProviders();
          }
          return;
        }
        setOpen115Auth(next);
        if (next.state === 'authorized') {
          clearRemoteDirectoryCache(providerID);
          setUploadOpen115Provider((current) => current && current.id === providerID ? { ...current, hasCredentials: true } : current);
          setNotice('115 Open 授权成功，Token 已安全保存。');
          await loadUploadProviders();
        }
      } catch (err) {
        if (active) setError(err instanceof Error ? err.message : '查询 115 Open 授权状态失败');
      } finally {
        if (active && !terminal) timer = window.setTimeout(() => void pollOpen115Authorization(), 2000);
      }
    }
    timer = window.setTimeout(() => void pollOpen115Authorization(), 2000);
    return () => {
      active = false;
      if (timer != null) window.clearTimeout(timer);
    };
  }, [open115Auth?.sessionId, open115Auth?.state, uploadOpen115Provider?.id]);

  useEffect(() => {
    if (!baiduOpenAuth || !uploadBaiduOpenProvider || !isBaiduOpenAuthorizationActive(baiduOpenAuth)) return;
    let active = true;
    const providerID = uploadBaiduOpenProvider.id;
    const sessionID = baiduOpenAuth.sessionId;
    let timer: number | undefined;
    async function pollBaiduOpenAuthorization() {
      let terminal = false;
      try {
        const response = await fetch(`/api/upload/providers/${providerID}/auth/baiduopen?sessionId=${encodeURIComponent(sessionID)}`);
        if (!response.ok) throw new Error(await readErrorMessage(response));
        const next = await response.json() as BaiduOpenAuthStatus;
        terminal = !isBaiduOpenAuthorizationActive(next);
        if (!active) return;
        setBaiduOpenAuth((current) => ({ ...next, authorizationUrl: next.authorizationUrl || current?.authorizationUrl }));
        if (next.state === 'authorized') {
          clearRemoteDirectoryCache(providerID);
          setUploadBaiduOpenProvider((current) => current && current.id === providerID ? { ...current, hasCredentials: true } : current);
          setNotice('Baidu Open authorization succeeded');
          await loadUploadProviders();
        } else if (next.state === 'completed') {
          setNotice('Broker authorization completed; import the displayed tokens');
        }
      } catch (err) {
        if (active) setError(err instanceof Error ? err.message : 'Failed to poll Baidu Open authorization');
      } finally {
        if (active && !terminal) timer = window.setTimeout(() => void pollBaiduOpenAuthorization(), 2000);
      }
    }
    timer = window.setTimeout(() => void pollBaiduOpenAuthorization(), 2000);
    return () => {
      active = false;
      if (timer != null) window.clearTimeout(timer);
    };
  }, [baiduOpenAuth?.sessionId, baiduOpenAuth?.state, uploadBaiduOpenProvider?.id]);

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
    setTaskRunFilter('');
    setTaskPathFilter('');
    setTaskFromFilter('');
    setTaskToFilter('');
    const emptyFilters = { scanRunId: '', path: '', from: '', to: '' };
    setAppliedTaskFilters(emptyFilters);
    void loadTasks(1, 'all', emptyFilters);
  }

  function applyTaskFilters() {
    const filters = { scanRunId: taskRunFilter, path: taskPathFilter, from: taskFromFilter, to: taskToFilter };
    setAppliedTaskFilters(filters);
    void loadTasks(1, taskStatusFilter, filters);
  }

  function applyUploadFilters() {
    setAppliedUploadPathFilter(uploadPathFilter);
    void refreshUploads(1, uploadStatusFilter, uploadPathFilter);
  }

  function resetUploadFilters() {
    setUploadPathFilter('');
    setAppliedUploadPathFilter('');
    setUploadStatusFilter('all');
    void refreshUploads(1, 'all', '');
  }

  function selectTaskStatusFilter(status: TaskStatusFilter) {
    setTaskStatusFilter(status);
    void loadTasks(1, status);
  }

  async function checkTools() {
    setCheckingTools(true);
    setError('');
    try {
      const response = await fetch('/api/tools/check', { method: 'POST' });
      setTools(asArray<ToolStatus>(await readJSONResponse<ToolStatus[]>(response, '工具检测')));
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
    setRenamePreviewSignature(renameInputSignature);
    setPreviewingRename(true);
    setError('');
    setNotice('');
    setRenamePreview([]);
    setRenamePreviewCount(0);
    setRenamePreviewTotal(0);
    setSelectedRenamePaths([]);
    setPendingRenamePaths([]);
    lastRenameSelectionIndexRef.current = null;
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
        setError(await readErrorMessage(response));
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
        setError(await readErrorMessage(response));
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

  function markRenameItemPending(path: string, patch: Partial<RenamePreviewItem>) {
    updateRenameItem(path, patch);
    setPendingRenamePaths((paths) => paths.includes(path) ? paths : [...paths, path]);
    setSelectedRenamePaths((paths) => paths.filter((item) => item !== path));
  }

  function replaceRenameItem(next: RenamePreviewItem) {
    setRenamePreview((items) => items.map((item) => item.path === next.path ? next : item));
  }

  function toggleRenameSelection(path: string, checked: boolean, shiftKey = false) {
    const index = renamePreview.findIndex((item) => item.path === path);
    if (checked && !executableRenamePathSet.has(path)) return;
    setSelectedRenamePaths((paths) => {
      if (shiftKey && lastRenameSelectionIndexRef.current !== null && index >= 0) {
        const start = lastRenameSelectionIndexRef.current;
        if (start >= 0) {
          const [from, to] = start < index ? [start, index] : [index, start];
          const range = renamePreview.slice(from, to + 1).filter((item) => executableRenamePathSet.has(item.path)).map((item) => item.path);
          return checked ? [...new Set([...paths, ...range])] : paths.filter((item) => !range.includes(item));
        }
      }
      return checked ? [...new Set([...paths, path])] : paths.filter((item) => item !== path);
    });
    if (index >= 0) lastRenameSelectionIndexRef.current = index;
  }

  function toggleRenameRowSelection(path: string, index: number, shiftKey = false) {
    const selected = selectedRenamePaths.includes(path);
    if (!selected && !executableRenamePathSet.has(path)) return;
    if (shiftKey && lastRenameSelectionIndexRef.current !== null) {
      const [from, to] = lastRenameSelectionIndexRef.current < index ? [lastRenameSelectionIndexRef.current, index] : [index, lastRenameSelectionIndexRef.current];
      const range = renamePreview.slice(from, to + 1).filter((entry) => executableRenamePathSet.has(entry.path)).map((entry) => entry.path);
      setSelectedRenamePaths((paths) => selected ? paths.filter((entry) => !range.includes(entry)) : [...new Set([...paths, ...range])]);
      lastRenameSelectionIndexRef.current = index;
      return;
    }
    setSelectedRenamePaths((paths) => selected ? paths.filter((entry) => entry !== path) : [...new Set([...paths, path])]);
    lastRenameSelectionIndexRef.current = index;
  }

  function handleRenameRowClick(event: ReactMouseEvent<HTMLTableRowElement>, item: RenamePreviewItem, index: number) {
    const target = event.target as HTMLElement;
    if (target.closest('input, button, select, textarea, a')) return;
    toggleRenameRowSelection(item.path, index, event.shiftKey);
  }

  function handleRenameRowKeyDown(event: ReactKeyboardEvent<HTMLTableRowElement>, item: RenamePreviewItem, index: number) {
    if (event.target !== event.currentTarget || !['Enter', ' '].includes(event.key)) return;
    event.preventDefault();
    toggleRenameRowSelection(item.path, index, event.shiftKey);
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
      const message = await readErrorMessage(response);
      setError(message);
      throw new Error(message);
    }
    return await response.json() as RenamePreviewItem;
  }

  async function recalculateRenameItem(item: RenamePreviewItem, options: { tmdbShowId?: number; show?: string; forceTmdb?: boolean; keepManualName?: boolean } = {}) {
    if (recalculatingRenamePathsRef.current.has(item.path)) return false;
    recalculatingRenamePathsRef.current.add(item.path);
    setRecalculatingRenamePaths(Array.from(recalculatingRenamePathsRef.current));
    setSelectedRenamePaths((paths) => paths.filter((path) => path !== item.path));
    try {
      const next = await previewAdjustedRenameItem(item, options);
      if (next) {
        replaceRenameItem(next);
        setPendingRenamePaths((paths) => paths.filter((path) => path !== item.path));
        const executable = isRenameItemExecutable(next, new Set(), new Set());
        if (!executable) {
          setError(next.message || (next.conflict ? '目标文件已存在，请修改目标路径' : '当前预览项不可执行'));
        }
        return executable;
      }
    } catch (err) {
      updateRenameItem(item.path, { status: 'error', message: err instanceof Error ? err.message : '重新预览失败' });
      setPendingRenamePaths((paths) => paths.includes(item.path) ? paths : [...paths, item.path]);
    } finally {
      recalculatingRenamePathsRef.current.delete(item.path);
      setRecalculatingRenamePaths(Array.from(recalculatingRenamePathsRef.current));
    }
    return false;
  }

  async function searchTmdbShows() {
    if (!tmdbQuery.trim()) return;
    setSearchingTmdb(true);
    setError('');
    try {
      const params = new URLSearchParams({ query: tmdbQuery.trim(), language: renameLanguage });
      const response = await fetch(`/api/tmdb/search-tv?${params.toString()}`);
      if (!response.ok) {
        setError(await readErrorMessage(response));
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
    let failed = 0;
    try {
      await runWithConcurrency(targets, renameBatchConcurrency, async (item) => {
        try {
          const applied = await recalculateRenameItem({ ...item, manualName: false }, { tmdbShowId: show.id, show: show.name || show.originalName, forceTmdb: true, keepManualName: false });
          if (!applied) failed++;
        } finally {
          completed++;
          setTmdbApplyProgress(completed);
        }
      });
      setTmdbMatchOpen(false);
      setNotice(failed
        ? `剧集匹配更新完成：${targets.length - failed} 个已更新，${failed} 个需要继续处理。`
        : `已将 ${targets.length} 个文件匹配到“${show.name || show.originalName}”。`);
    } finally {
      applyingTmdbShowRef.current = false;
      setApplyingTmdbShowId(null);
      setTmdbApplyProgress(0);
      setTmdbApplyTotal(0);
    }
  }

  function selectAllRenameItems() {
    setSelectedRenamePaths(executableRenameItems.map((item) => item.path));
  }

  function invertRenameSelection() {
    setSelectedRenamePaths(executableRenameItems.filter((item) => !selectedRenamePaths.includes(item.path)).map((item) => item.path));
  }

  async function applyTargetPathEdit() {
    if (!targetPathEditor) return;
    const item = renamePreview.find((candidate) => candidate.path === targetPathEditor.path);
    if (!item || !targetPathEditor.value.trim()) return;
    const manualTarget = targetPathEditor.value.trim();
    const applied = await recalculateRenameItem({ ...item, newName: manualTarget, newPath: manualTarget, renderedTarget: manualTarget, manualName: true }, { keepManualName: true });
    if (applied) {
      setTargetPathEditor((current) => current?.path === item.path ? null : current);
    } else if (recalculatingRenamePathsRef.current.has(item.path)) {
      setError('该文件正在重新生成目标，请完成后再应用手工路径');
    }
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
    let failed = 0;
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
          if (next) {
            replaceRenameItem(next);
            if (isRenameItemExecutable(next, new Set(), new Set())) {
              setPendingRenamePaths((paths) => paths.filter((path) => path !== item.path));
            } else {
              failed++;
              setSelectedRenamePaths((paths) => paths.filter((path) => path !== item.path));
            }
          }
        } catch (err) {
          failed++;
          updateRenameItem(item.path, { status: 'error', message: err instanceof Error ? err.message : '重新预览失败' });
          setPendingRenamePaths((paths) => paths.includes(item.path) ? paths : [...paths, item.path]);
          setSelectedRenamePaths((paths) => paths.filter((path) => path !== item.path));
        } finally {
          completed++;
          setBatchEpisodeProgress(completed);
        }
      });
      setBatchEpisodeOpen(false);
      setNotice(failed
        ? `批量修正完成：${targets.length - failed} 个已更新，${failed} 个失败并保留为待处理。`
        : `已批量修正 ${targets.length} 个文件的季集并重新预览。`);
    } finally {
      setApplyingBatchEpisode(false);
      setBatchEpisodeProgress(0);
    }
  }

  function applySelectedRenames() {
    if (renamePreviewStale) {
      setError('路径、模板、查询语言或字幕组已变更，请重新生成预览后再执行');
      return;
    }
    if (selectedRenameItemsBlocked) {
      setError('选中项中包含尚未重新生成、正在生成或存在错误的文件，请处理后重新选择');
      return;
    }
    const targets = executableRenameItems.filter((item) => selectedRenamePaths.includes(item.path));
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
        setError(await readErrorMessage(response));
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
      setError(await readErrorMessage(checkResponse));
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
        setError(await readErrorMessage(response));
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
    if (!newWatchDir.trim() || modalBusyRef.current.savingWatchDir) return;
    modalBusyRef.current.savingWatchDir = true;
    setSavingWatchDir(true);
    setError('');
    try {
      const response = await fetch('/api/watch-dirs', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: newWatchDir.trim(), recursive: true, watchEnabled: newWatchDirWatchEnabled, scanOnStart: false, useGlobalProcessing: newWatchDirUseGlobalProcessing, processing: newWatchDirProcessing, uploadConfigs: newWatchDirUploadConfigs })
      });
      if (!response.ok) {
        const message = await readErrorMessage(response);
        if (response.status === 409) {
          try {
            await loadWatchDirs();
            setNewWatchDir('');
            setAddWatchDirOpen(false);
            setNotice(`${message}。目录列表已刷新。`);
            return;
          } catch {
            // Preserve the actionable conflict message when refreshing also fails.
          }
        }
        setError(message);
        return;
      }
      const created = await response.json();
      setWatchDirs((items) => [...items, created]);
      setNewWatchDir('');
      setNewWatchDirWatchEnabled(true);
      setNewWatchDirUseGlobalProcessing(true);
      setNewWatchDirProcessing(outputProcessingFromConfig(config));
      setNewWatchDirUploadConfigs([]);
      setAddWatchDirOpen(false);
      setNotice('媒体目录已添加，自动监听配置已热更新。');
    } catch (err) {
      setError(err instanceof Error ? err.message : '添加媒体目录失败');
    } finally {
      modalBusyRef.current.savingWatchDir = false;
      setSavingWatchDir(false);
    }
  }

  function openEditWatchDir(dir: WatchDir) {
    const processing = dir.processing?.strategy ? dir.processing : outputProcessingFromConfig(config);
    const uploadConfigs = structuredClone(dir.uploadConfigs ?? []);
    watchDirDraftInitialRef.current = watchDirDraftSignature(dir.path, dir.watchEnabled, dir.useGlobalProcessing, processing, uploadConfigs);
    setEditingWatchDir(dir);
    setEditingWatchDirPath(dir.path);
    setEditingWatchDirWatchEnabled(dir.watchEnabled);
    setEditingWatchDirUseGlobalProcessing(dir.useGlobalProcessing);
    setEditingWatchDirProcessing(processing);
    setEditingWatchDirUploadConfigs(uploadConfigs);
  }

  async function submitEditWatchDir() {
    if (!editingWatchDir || !editingWatchDirPath.trim() || modalBusyRef.current.savingWatchDir) return;
    modalBusyRef.current.savingWatchDir = true;
    setSavingWatchDir(true);
    try {
      const updated = await updateWatchDir(editingWatchDir, {
        path: editingWatchDirPath.trim(),
        watchEnabled: editingWatchDirWatchEnabled,
        scanOnStart: false,
        useGlobalProcessing: editingWatchDirUseGlobalProcessing,
        processing: editingWatchDirProcessing,
        uploadConfigs: editingWatchDirUploadConfigs
      });
      if (!updated) return;
      setEditingWatchDir(null);
      setEditingWatchDirPath('');
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存媒体目录失败，请检查后台连接后重试');
    } finally {
      modalBusyRef.current.savingWatchDir = false;
      setSavingWatchDir(false);
    }
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
      setError(await readErrorMessage(response));
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

  function openAddWatchDirModal() {
    const processing = outputProcessingFromConfig(config);
    watchDirDraftInitialRef.current = watchDirDraftSignature('', true, true, processing, []);
    setNewWatchDir('');
    setNewWatchDirWatchEnabled(true);
    setNewWatchDirUseGlobalProcessing(true);
    setNewWatchDirProcessing(processing);
    setNewWatchDirUploadConfigs([]);
    setAddWatchDirOpen(true);
  }

  async function performDeleteWatchDir(id: number) {
    setError('');
    try {
      const response = await fetch(`/api/watch-dirs/${id}`, { method: 'DELETE' });
      if (!response.ok) {
        setError(await readErrorMessage(response));
        return;
      }
      setWatchDirs((items) => items.filter((item) => item.id !== id));
      setNotice('媒体目录已移除，磁盘上的文件未被修改。');
    } catch (err) {
      setError(err instanceof Error ? err.message : '移除媒体目录失败，请检查后台连接后重试');
    }
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
      setRescanOpen(false);
      setNotice('扫描任务已加入队列，可在任务队列中查看进度。');
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
    const requestRef = mode === 'missing' ? missingAuditRequestRef : embyAuditRequestRef;
    const requestID = ++requestRef.current;
    const setAuditing = mode === 'missing' ? setAuditingMissing : setAuditingEmby;
    if (mode === 'missing') setMissingAuditReport(null);
    else setEmbyAuditReport(null);
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
      if (requestRef.current !== requestID) return;
      if (!response.ok) {
        setError(await readErrorMessage(response));
        return;
      }
      const report = await response.json() as AuditReport;
      if (requestRef.current !== requestID) return;
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
      if (requestRef.current === requestID) setError(err instanceof Error ? err.message : '剧集核对失败');
    } finally {
      if (requestRef.current === requestID) setAuditing(false);
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
    const requestID = ++fileAuditRequestRef.current;
    setFileAuditReport(null);
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
      if (fileAuditRequestRef.current !== requestID) return;
      if (!response.ok) {
        setError(await readErrorMessage(response));
        return;
      }
      const report = await response.json() as FileAuditReport;
      if (fileAuditRequestRef.current !== requestID) return;
      setFileAuditReport(report);
      setNotice(`文件对齐检查完成：本地 ${report.localCount} 个，远端 ${report.remoteCount} 个，差异 ${report.issues?.length ?? 0} 项。`);
    } catch (err) {
      if (fileAuditRequestRef.current === requestID) setError(err instanceof Error ? err.message : '文件对齐检查失败');
    } finally {
      if (fileAuditRequestRef.current === requestID) setAuditingFiles(false);
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
        setError(await readErrorMessage(response));
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
      setError(await readErrorMessage(response));
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
      throw new Error(await readErrorMessage(response));
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
    if (!retryableSelectedTaskIds.length) {
      setError('请选择失败、已取消或已忽略的媒体任务进行重试');
      return;
    }
    setRetryingTasks(true);
    setError('');
    try {
      const response = await fetch('/api/tasks/retry', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ids: retryableSelectedTaskIds })
      });
      if (!response.ok) {
        setError(await readErrorMessage(response));
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
    if (!ignorableSelectedTaskIds.length) {
      setError('请先勾选要忽略的失败任务');
      return;
    }
    setIgnoringTasks(true);
    setError('');
    try {
      const response = await fetch('/api/tasks/ignore', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ids: ignorableSelectedTaskIds })
      });
      if (!response.ok) {
        setError(await readErrorMessage(response));
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
      message: '将取消所有待执行和执行中的任务，不受当前筛选条件影响。已经生成的文件不会自动删除。',
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
        setError(await readErrorMessage(response));
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
        setError(await readErrorMessage(response));
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

  async function updateDesktopAutostart(enabled: boolean) {
    setSavingDesktopPreferences(true);
    setError('');
    setNotice('');
    try {
      const preferences = await setDesktopAutostart(enabled);
      setDesktopPreferences(preferences);
      setNotice(enabled ? '已启用登录后后台启动。' : '已关闭登录后自动启动。');
    } catch (err) {
      setError(err instanceof Error ? err.message : '更新开机自启失败');
    } finally {
      setSavingDesktopPreferences(false);
    }
  }

  async function checkForApplicationUpdates() {
    setCheckingUpdates(true);
    setError('');
    setNotice('');
    try {
      const result = await checkDesktopUpdates();
      setUpdateCheck(result);
      if (result.status === 'upToDate') setNotice(`当前已是最新版本 ${result.currentVersion}。`);
    } catch (err) {
      setError(err instanceof Error ? err.message : '检查更新失败');
    } finally {
      setCheckingUpdates(false);
    }
  }

  function requestUpdateInstallation() {
    if (updateCheck?.status !== 'available' || !updateCheck.version) return;
    const targetVersion = updateCheck.version;
    requestConfirmation({
      title: `安装 NyaMediaMetadataTool ${targetVersion}？`,
      message: '安装器下载并通过 SHA-256 校验后会启动，当前应用随后退出。',
      confirmLabel: '下载并安装',
      onConfirm: async () => {
        setInstallingUpdate(true);
        setError('');
        try {
          await downloadAndInstallDesktopUpdate(targetVersion);
        } catch (err) {
          setError(err instanceof Error ? err.message : '无法安装更新');
          setInstallingUpdate(false);
        }
      }
    });
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

  function confirmDiscardConfigChanges() {
    if (!configDirty) return;
    requestConfirmation({
      title: '放弃所有设置修改？',
      message: '当前设置页中的未保存内容将恢复为上次保存的值，且无法撤销。',
      confirmLabel: '放弃修改',
      tone: 'danger',
      onConfirm: discardConfigChanges
    });
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
            <h1>NyaMediaMetadataTool</h1>
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
          </div>
        </header>

        {restartRequired && (
          <section className="restart-banner" role="status">
            <RefreshCw size={18} aria-hidden="true" />
            <div><strong>配置将在重启后完全生效</strong><span>当前任务可以继续运行；完成后关闭并重新打开应用即可。</span></div>
          </section>
        )}
        {notice && <section className="toast-card" role="status"><CheckCircle2 size={18} /><span>{notice}</span><button className="toast-close" type="button" aria-label="关闭通知" onClick={() => setNotice('')}><X size={16} /></button></section>}
        {error && <AlertDialog title="操作失败" message={error} onClose={() => setError('')} />}
        {initialLoading && <InitialLoading />}

        <div ref={workspaceContentRef} className={initialLoading ? 'workspace-content loading' : 'workspace-content'}>

        {activePage === 'dashboard' && (
        <section className="dashboard-page">
          <section className="dashboard-overview dashboard-overview-compact">
            <div className="dashboard-metrics" aria-label="运行概览">
              <DashboardMetric label="服务状态" value={!healthCheckedAt ? '连接中' : connectionOnline ? healthStatusLabel(health?.status) : '未连接'} tone={connectionOnline && health?.status === 'ok' ? 'good' : 'warn'} />
              <DashboardMetric label="任务总数" value={String(taskSummary.total)} />
              <DashboardMetric label="活跃任务" value={String(activeTaskCount)} tone={activeTaskCount ? 'warn' : 'neutral'} />
              <DashboardMetric label="失败任务" value={String(failedTaskCount)} tone={failedTaskCount ? 'bad' : 'good'} />
              <DashboardMetric label="媒体目录" value={`${enabledWatchDirCount}/${watchDirs.length}`} />
              <DashboardMetric label="可用工具" value={`${availableToolCount}/${tools.length || 3}`} tone={tools.length && availableToolCount !== tools.length ? 'bad' : 'good'} />
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
                <SetupRow icon={FolderCog} title="媒体目录" detail={watchDirs.length ? `已添加 ${watchDirs.length} 个目录` : '尚未添加自动扫描目录'} complete={watchDirs.length > 0} actionLabel={watchDirs.length ? undefined : '添加目录'} onAction={openAddWatchDirModal} />
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

            <Card title="工具状态" action={<button className="icon-text-button" onClick={checkTools} disabled={checkingTools}><RefreshCw size={16} />{checkingTools ? '检测中' : '重新检测'}</button>}>
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
            <Card header={<div className="settings-tabs" role="tablist" aria-label="设置分类" aria-orientation="horizontal">
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
            </div>} action={settingsTab === 'about' ? undefined : <div className="inline-actions"><button className="secondary icon-text-button" type="button" onClick={confirmDiscardConfigChanges} disabled={!configDirty || savingConfig}><RefreshCw size={16} />放弃修改</button><button className="icon-text-button" onClick={saveConfig} disabled={savingConfig || !configDirty || !config}><Save size={16} />{savingConfig ? '保存中' : '保存配置'}</button></div>}>
            {config ? (
              <div className="settings-scroll-region">
              <div className="config-form settings-form">
                <section id="settings-panel-basic" className={`settings-section ${settingsTab === 'basic' ? 'active' : ''}`} role="tabpanel" aria-labelledby="settings-tab-basic" hidden={settingsTab !== 'basic'}>
                  <SettingsGroup title="桌面行为">
                    <ThemeSelector value={themeMode} onChange={setThemeMode} />
                    <Toggle
                      label="登录后自动启动（后台）"
                      checked={desktopPreferences?.autostartEnabled ?? false}
                      disabled={!desktopPreferences?.autostartSupported || savingDesktopPreferences}
                      onChange={(enabled) => void updateDesktopAutostart(enabled)}
                    />
                  </SettingsGroup>
                  <SettingsGroup title="本地环境">
                    <label>显示时区<input list="timezone-options" value={config.server.timezone} onChange={(event) => updateConfig((draft) => { draft.server.timezone = event.target.value; })} placeholder="Asia/Shanghai" /></label>
                    <datalist id="timezone-options">
                      {timeZoneOptions.map((timezone) => <option key={timezone} value={timezone} />)}
                    </datalist>
                    <PathField label="ffmpeg" value={config.tools.ffmpeg} onChange={(value) => updateConfig((draft) => { draft.tools.ffmpeg = value; })} onBrowse={() => void browseFile({ title: '选择 ffmpeg', value: config.tools.ffmpeg, onSelect: (value) => updateConfig((draft) => { draft.tools.ffmpeg = value; }) })} />
                    <PathField label="ffprobe" value={config.tools.ffprobe} onChange={(value) => updateConfig((draft) => { draft.tools.ffprobe = value; })} onBrowse={() => void browseFile({ title: '选择 ffprobe', value: config.tools.ffprobe, onSelect: (value) => updateConfig((draft) => { draft.tools.ffprobe = value; }) })} />
                    <PathField label="mediainfo" value={config.tools.mediainfo} onChange={(value) => updateConfig((draft) => { draft.tools.mediainfo = value; })} onBrowse={() => void browseFile({ title: '选择 mediainfo', value: config.tools.mediainfo, onSelect: (value) => updateConfig((draft) => { draft.tools.mediainfo = value; }) })} />
                  </SettingsGroup>
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
                <section id="settings-panel-about" className={`settings-section settings-section-wide ${settingsTab === 'about' ? 'active' : ''}`} role="tabpanel" aria-labelledby="settings-tab-about" hidden={settingsTab !== 'about'}>
                  <SettingsGroup title="版本信息">
                    <dl className="build-info-list">
                      <div><dt>版本</dt><dd>{runtimeInfo?.version || '未知'}</dd></div>
                      <div><dt>提交</dt><dd title={runtimeInfo?.commit}>{runtimeInfo?.commit && runtimeInfo.commit !== 'unknown' ? runtimeInfo.commit.slice(0, 12) : '未知'}</dd></div>
                      <div><dt>构建时间</dt><dd>{formatBuildDate(runtimeInfo?.buildDate)}</dd></div>
                      <div><dt>发布仓库</dt><dd>{runtimeInfo?.updateRepository || '未配置'}</dd></div>
                      <div><dt>平台</dt><dd>{runtimeInfo?.desktop ? `${runtimeInfo.platform} / ${runtimeInfo.arch}` : 'Web 管理模式'}</dd></div>
                    </dl>
                  </SettingsGroup>
                  <SettingsGroup title="应用更新">
                    <div className="application-update-panel">
                      {!updateCheck && <p className="settings-note">尚未检查更新。</p>}
                      {updateCheck?.status === 'unsupported' && <p className="settings-note">{updateSupportMessage(updateCheck.reason)}</p>}
                      {updateCheck?.status === 'upToDate' && <div className="update-status-line"><CheckCircle2 size={18} /><span>当前已是最新版本 {updateCheck.currentVersion}</span></div>}
                      {updateCheck?.status === 'available' && <div className="update-release">
                        <div className="update-status-line"><Download size={18} /><span>发现新版本 {updateCheck.version}</span></div>
                        {updateCheck.releaseName && <strong>{updateCheck.releaseName}</strong>}
                        {updateCheck.publishedAt && <small>{formatBuildDate(updateCheck.publishedAt)}</small>}
                        {updateCheck.releaseNotes && <p className="update-release-notes">{updateCheck.releaseNotes}</p>}
                      </div>}
                      <div className="inline-actions">
                        <button className="secondary icon-text-button" type="button" onClick={() => void checkForApplicationUpdates()} disabled={!runtimeInfo?.desktop || checkingUpdates || installingUpdate}><RefreshCw size={16} />{checkingUpdates ? '检查中' : '检查更新'}</button>
                        {updateCheck?.status === 'available' && <button className="icon-text-button" type="button" onClick={requestUpdateInstallation} disabled={installingUpdate}><Download size={16} />{installingUpdate ? '正在准备安装' : '下载并安装'}</button>}
                      </div>
                    </div>
                  </SettingsGroup>
                </section>
              </div>
              </div>
            ) : <p className="muted">配置加载中。</p>}
          </Card>
        </section>
      )}

        {activePage === 'watchDirs' && (
        <section className="page-grid">
          <Card title="媒体目录" action={<div className="inline-actions"><button className="secondary icon-text-button" onClick={() => openRescanDialog('all')} disabled={rescanning}><SearchCheck size={16} />{rescanning ? '扫描中' : '扫描生成'}</button><button className="icon-text-button" onClick={openAddWatchDirModal}><Plus size={16} />添加媒体目录</button></div>}>
            {watchDirs.length ? watchDirs.map((dir) => {
              const uploadCount = (dir.uploadConfigs ?? []).filter((item) => item.enabled).length;
              const uploadIssueCount = (dir.uploadConfigs ?? []).filter((item) => {
                if (!item.enabled) return false;
                const provider = uploadProviders.find((candidate) => candidate.id === item.providerId);
                return !provider || !provider.enabled || uploadProviderNeedsAuthorization(provider);
              }).length;
              return (
                <div className="dir-item" key={dir.id}>
                  <div>
                    <strong>{dir.path}</strong>
                    <small>{dir.watchEnabled ? '自动监听' : '不监听'} · {dir.useGlobalProcessing ? '跟随全局生成设置' : '独立生成设置'} · {uploadCount ? `${uploadCount} 个上传配置${uploadIssueCount ? `，${uploadIssueCount} 个需处理` : ''}` : '不上传'}</small>
                  </div>
                  <div className="inline-actions">
                    <ActionIconButton label={`编辑媒体目录 ${dir.path}`} icon={Pencil} onClick={() => openEditWatchDir(dir)} />
                    <ActionIconButton label={`扫描生成 ${dir.path}`} icon={SearchCheck} disabled={rescanning} onClick={() => openRescanDialog('dir', dir.path)} />
                    <ActionIconButton label={`删除媒体目录 ${dir.path}`} icon={Trash2} tone="danger" onClick={() => deleteWatchDir(dir.id)} />
                  </div>
                </div>
              );
            }) : <div className="empty-workflow"><span className="empty-workflow-icon" aria-hidden="true"><FolderCog size={22} /></span><div><strong>尚未添加媒体目录</strong><span>添加目录后即可监听新文件并生成元数据。</span></div><button type="button" onClick={openAddWatchDirModal}>添加媒体目录</button></div>}
          </Card>
        </section>
      )}

        {activePage === 'rename' && (
        <section className="page-grid rename-page-grid">
          <Card title="整理命名" action={<button className="secondary icon-text-button" type="button" onClick={() => setRenameHistoryOpen(true)}><History size={16} />重命名历史{renameHistory.length ? ` (${renameHistory.length})` : ''}</button>}>
            <div className="rename-controls">
              <label className="rename-control-primary">目录或文件路径<div className="path-input"><input value={renamePath} onChange={(event) => setRenamePath(event.target.value)} placeholder="D:\\Media\\Anime\\Season 1" /><button type="button" className="icon-text-button" onClick={() => void browseDirectory({ title: '选择整理目录', value: renamePath, onSelect: setRenamePath })}><FolderOpen size={16} />选择</button></div></label>
              <label className="rename-control-primary">命名模板
                <div className={renameTemplateHistory.length ? 'template-input-row' : 'template-input-row no-history'}>
                  <button className="target-path-preview rename-template-preview" type="button" onClick={() => setRenameTemplateEditorOpen(true)}>{renameTemplate || defaultRenameTemplate}</button>
                  {renameTemplateHistory.length > 0 && <div className="template-history-picker">
                    <button className="secondary template-history-trigger" type="button" onClick={() => setRenameTemplateHistoryOpen((value) => !value)} disabled={!renameTemplateHistory.length}>最近模板</button>
                    {renameTemplateHistoryOpen ? <div className="template-history-menu">
                      {renameTemplateHistory.map((template) => <div className="template-history-item" key={template}>
                        <button className="template-history-use" type="button" title={template} onClick={() => { setRenameTemplate(template); setRenameTemplateHistoryOpen(false); }}>{template}</button>
                        <button className="template-history-delete" type="button" title="删除最近模板" aria-label={`删除模板 ${template}`} onClick={() => deleteRenameTemplateHistory(template)}><span aria-hidden="true">&times;</span></button>
                      </div>)}
                    </div> : null}
                  </div>}
                </div>
              </label>
              <SelectField label="查询语言" value={renameLanguage} options={languageOptions} onChange={setRenameLanguage} />
              <label>字幕组<input value={renameReleaseGroup} onChange={(event) => setRenameReleaseGroup(event.target.value)} placeholder="留空则从原文件名识别" /></label>
              <div className="rename-preview-action">
                <button className="secondary" type="button" onClick={previewingRename ? cancelRenamePreview : () => void previewRename(true)} disabled={!previewingRename && (recalculatingRenamePaths.length > 0 || applyingTmdbShowId !== null || applyingBatchEpisode || applyingRename)}>{previewingRename ? '取消预览' : '忽略缓存重新生成'}</button>
                <button type="button" onClick={() => void previewRename()} disabled={previewingRename || recalculatingRenamePaths.length > 0 || applyingTmdbShowId !== null || applyingBatchEpisode || applyingRename}>{previewingRename ? renamePreviewTotal ? `生成预览 ${renamePreviewCount} / ${renamePreviewTotal}` : '正在扫描文件…' : '生成预览'}</button>
              </div>
            </div>
          </Card>

          <Card title="重命名预览" action={<div className="rename-preview-summary"><span>共 <strong>{renamePreview.length}</strong> 项</span><span>已选 <strong>{selectedRenamePaths.length}</strong></span>{renamePendingCount > 0 && <span className="warn">待更新 <strong>{renamePendingCount}</strong></span>}<span className={renameWarningCount ? 'warn' : ''}>警告 <strong>{renameWarningCount}</strong></span><span className={renameErrorCount ? 'bad' : ''}>错误 <strong>{renameErrorCount}</strong></span></div>}>
            {renamePreviewStale && <div className="workflow-warning" role="status"><AlertTriangle size={17} aria-hidden="true" /><span>预览参数已变更。请重新生成预览，确认新文件名后再执行。</span></div>}
            <div className="rename-match-bar">
              <div className="rename-action-row">
                <div className="inline-actions rename-bulk-actions">
                  <button className="secondary" type="button" onClick={selectAllRenameItems} disabled={renamePreviewStale || !executableRenameItems.length || applyingRename}>全选可执行项</button>
                  <button className="secondary" type="button" onClick={invertRenameSelection} disabled={renamePreviewStale || !executableRenameItems.length || applyingRename}>反选</button>
                  <button className="secondary" type="button" onClick={openBatchEpisodeDialog} disabled={renamePreviewStale || selectedRenameItemsBlocked || !selectedRenamePaths.length || applyingRename}>批量修正季集</button>
                  <button className="secondary" type="button" onClick={() => setTmdbMatchOpen(true)} disabled={renamePreviewStale || selectedRenameItemsBlocked || !selectedRenamePaths.length || applyingRename}>更改匹配剧集</button>
                  <span className="rename-preview-stats">并发 {renameBatchConcurrency}</span>
                </div>
                <button className="rename-apply-button" type="button" onClick={applySelectedRenames} disabled={renamePreviewStale || applyingRename || selectedRenameItemsBlocked || !selectedRenamePaths.length}>{applyingRename ? '重命名中' : `执行选中重命名 (${selectedRenamePaths.length})`}</button>
              </div>
            </div>
            {renamePreview.length ? <div className="task-table-wrap">
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
                  {renamePreview.map((item, index) => {
                    const recalculatingItem = recalculatingRenamePaths.includes(item.path);
                    const pendingItem = pendingRenamePaths.includes(item.path);
                    const executableItem = executableRenamePathSet.has(item.path);
                    const selectedItem = selectedRenamePaths.includes(item.path);
                    const rowClassName = ['rename-row', selectedItem && 'selected', pendingItem && 'pending', !executableItem && 'unselectable'].filter(Boolean).join(' ');
                    return (
                    <tr className={rowClassName} key={item.path} tabIndex={executableItem || selectedItem ? 0 : -1} aria-selected={selectedItem} onClick={(event) => handleRenameRowClick(event, item, index)} onKeyDown={(event) => handleRenameRowKeyDown(event, item, index)} title={executableItem ? '点击行选择，Shift+点击连续选择；聚焦后按 Enter 或空格选择' : recalculatingItem ? '正在重新生成目标' : pendingItem ? '季集已修改，请先重新生成目标' : '当前项目不可执行'}>
                      <td><span className={selectedItem ? 'rename-row-index selected' : 'rename-row-index'} aria-hidden="true"><strong>{index + 1}</strong>{(pendingItem || recalculatingItem) && <small>{recalculatingItem ? '生成中' : '待更新'}</small>}</span></td>
                      <td>
                        <div className="rename-status-cell">
                          <span className={`pill ${pendingItem || recalculatingItem ? 'warn' : item.status === 'error' ? 'bad' : item.status === 'ok' ? 'ok' : ''}`}>{recalculatingItem ? '生成中' : pendingItem ? '待重新生成' : renameStatusLabel(item.status)}</span>
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
                            <input type="number" min="0" value={item.season ?? 0} onChange={(event) => markRenameItemPending(item.path, { season: Number(event.target.value) })} onKeyDown={(event) => { if (event.key === 'Enter') void recalculateRenameItem({ ...item, season: Number(event.currentTarget.value), manualName: false }, { forceTmdb: true, keepManualName: false }); }} disabled={recalculatingItem || applyingTmdbShowId !== null || applyingBatchEpisode || applyingRename} title="修改后按回车重新查询 TMDB 并生成目标" />
                          </label>
                          <label className="rename-edit-field">
                            <span>集</span>
                            <input type="number" min="0" value={item.episode ?? 0} onChange={(event) => markRenameItemPending(item.path, { episode: Number(event.target.value) })} onKeyDown={(event) => { if (event.key === 'Enter') void recalculateRenameItem({ ...item, episode: Number(event.currentTarget.value), manualName: false }, { forceTmdb: true, keepManualName: false }); }} disabled={recalculatingItem || applyingTmdbShowId !== null || applyingBatchEpisode || applyingRename} title="修改后按回车重新查询 TMDB 并生成目标" />
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
                        <button className="target-path-preview" type="button" title={getRenameTargetDisplayValue(item)} onClick={() => { const value = getRenameTargetEditorValue(item); setTargetPathEditor({ path: item.path, value, initialValue: value }); }}>
                          <RenameTargetPathDisplay value={getRenameTargetDisplayValue(item)} />
                        </button>
                      </td>
                      <td className="path-cell">{recalculatingItem ? '正在根据当前季集生成新目标…' : pendingItem ? '季集已修改，重新生成前不会执行' : item.conflict ? '目标文件已存在' : item.message || '-'}</td>
                      <td>
                        <div className="inline-actions rename-row-actions">
                          <ActionIconButton label="根据当前季集重新生成" icon={RefreshCw} loading={recalculatingItem} onClick={() => recalculateRenameItem({ ...item, manualName: false }, { forceTmdb: true, keepManualName: false })} disabled={renamePreviewStale || previewingRename || applyingTmdbShowId !== null || applyingBatchEpisode || applyingRename || recalculatingItem} />
                        </div>
                      </td>
                    </tr>
                  );
                  })}
                </tbody>
              </table>
            </div> : <div className="rename-preview-empty"><Tags size={22} aria-hidden="true" /><strong>尚未生成预览</strong></div>}
          </Card>
        </section>
      )}

        {activePage === 'audit' && (
        <section className="page-grid audit-page-grid">
          <div className="page-tabs audit-tabs" role="tablist" aria-label="核对类型">
            <button className={auditTab === 'missing' ? 'status-tab active' : 'status-tab'} type="button" role="tab" aria-selected={auditTab === 'missing'} onClick={() => setAuditTab('missing')}>剧集缺漏</button>
            <button className={auditTab === 'emby' ? 'status-tab active' : 'status-tab'} type="button" role="tab" aria-selected={auditTab === 'emby'} onClick={() => setAuditTab('emby')}>Emby 与本地核对</button>
            <button className={auditTab === 'files' ? 'status-tab active' : 'status-tab'} type="button" role="tab" aria-selected={auditTab === 'files'} onClick={() => setAuditTab('files')}>文件对齐检查</button>
          </div>

          {auditTab === 'missing' && <Card title="剧集缺漏" action={<button className="icon-text-button" onClick={() => runSeriesAudit('missing')} disabled={auditingMissing}><SearchCheck size={16} />{auditingMissing ? '核对中' : '开始核对'}</button>}>
            <fieldset className="workflow-fieldset" disabled={auditingMissing}>
              <div className="audit-controls">
                <label>剧集根目录<div className="path-input"><input value={auditRoot} onChange={(event) => setAuditRoot(event.target.value)} placeholder="D:\Media\TV\Example Show" /><button type="button" className="icon-text-button" onClick={() => void browseDirectory({ title: '选择剧集根目录', value: auditRoot, onSelect: setAuditRoot })}><FolderOpen size={16} />选择</button></div></label>
                <label><FieldLabel label="TMDB 剧集 ID" help="留空时读取 tvshow.nfo 中的 TMDB ID；手动填写或选择剧集会覆盖自动读取的 ID。" /><div className="path-input"><input value={auditTmdbId} onChange={(event) => setAuditTmdbId(event.target.value)} inputMode="numeric" placeholder="可选，优先于 tvshow.nfo" /><button type="button" onClick={openAuditTmdbMatch}>选择剧集</button></div></label>
              </div>
              <div className="audit-option-row">
                <Toggle label={<FieldLabel label="检查 Season 0" help="开启后，Season 0 会参与缺漏判断和产物检查。" />} checked={auditIncludeSeasonZero} onChange={setAuditIncludeSeasonZero} />
              </div>
            </fieldset>
          </Card>}

          {auditTab === 'emby' && <Card title="Emby 与本地核对" action={<button className="icon-text-button" onClick={() => runSeriesAudit('emby')} disabled={auditingEmby}><SearchCheck size={16} />{auditingEmby ? '核对中' : '开始核对'}</button>}>
            <fieldset className="emby-audit-form workflow-fieldset" disabled={auditingEmby}>
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
            </fieldset>
          </Card>}

          {auditTab === 'files' && <Card title="文件对齐检查" action={<button className="icon-text-button" onClick={runFileAudit} disabled={auditingFiles}><SearchCheck size={16} />{auditingFiles ? '检查中' : '开始检查'}</button>}>
            <fieldset className="file-audit-form workflow-fieldset" disabled={auditingFiles}>
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
            </fieldset>
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
          <nav className="page-tabs upload-tabs" role="tablist" aria-label="上传管理子页面">
            <button className={uploadView === 'batches' ? 'status-tab active' : 'status-tab'} type="button" role="tab" aria-selected={uploadView === 'batches'} onClick={() => navigateUploadView('batches')}>上传批次</button>
            <button className={uploadView === 'providers' ? 'status-tab active' : 'status-tab'} type="button" role="tab" aria-selected={uploadView === 'providers'} onClick={() => navigateUploadView('providers')}>Provider 账号</button>
            <button className={uploadView === 'notifications' ? 'status-tab active' : 'status-tab'} type="button" role="tab" aria-selected={uploadView === 'notifications'} onClick={() => navigateUploadView('notifications')}>通知模板</button>
            <button className={uploadView === 'notificationRecords' ? 'status-tab active' : 'status-tab'} type="button" role="tab" aria-selected={uploadView === 'notificationRecords'} onClick={() => navigateUploadView('notificationRecords')}>通知记录</button>
          </nav>

          {uploadView === 'batches' && <>
            <Card title="上传概览">
              <div className="upload-summary-grid" aria-label="上传概览">
                <DashboardMetric label="合并中" value={String(uploadSummary.collecting)} tone={uploadSummary.collecting ? 'warn' : 'neutral'} />
                <DashboardMetric label="等待上传" value={String(uploadSummary.pending)} tone={uploadSummary.pending ? 'warn' : 'neutral'} />
                <DashboardMetric label="上传中" value={String(uploadSummary.running)} tone={uploadSummary.running ? 'warn' : 'neutral'} />
                <DashboardMetric label="已完成" value={String(uploadSummary.completed)} tone="good" />
                <DashboardMetric label="失败/部分失败" value={String(uploadSummary.failed)} tone={uploadSummary.failed ? 'bad' : 'good'} />
              </div>
            </Card>

            <Card title="上传批次" action={<ActionIconButton label="刷新上传批次" icon={RefreshCw} loading={refreshingUploads} disabled={refreshingUploads} onClick={() => void refreshUploads(1)} />}>
              <div className="task-status-tabs" role="group" aria-label="上传批次状态过滤">
                {uploadStatusFilters.map((status) => <button className={uploadStatusFilter === status.value ? 'status-tab active' : 'status-tab'} type="button" key={status.value} aria-pressed={uploadStatusFilter === status.value} disabled={refreshingUploads} onClick={() => { setUploadStatusFilter(status.value); void refreshUploads(1, status.value); }}>{status.label}</button>)}
              </div>
              <form className="task-filters upload-filters" onSubmit={(event) => { event.preventDefault(); applyUploadFilters(); }}>
                <label>番剧路径<input value={uploadPathFilter} onChange={(event) => setUploadPathFilter(event.target.value)} placeholder="输入番剧目录关键字" /></label>
                <div className="filter-actions list-toolbar-icons"><ActionIconButton label="应用过滤" icon={Filter} type="submit" disabled={refreshingUploads} /><ActionIconButton label="重置过滤" icon={RotateCcw} disabled={refreshingUploads} onClick={resetUploadFilters} /></div>
              </form>
              <div className="task-table-wrap">
                <table className="task-table upload-batch-table">
                  <thead><tr><th>ID</th><th>状态</th><th>番剧目录</th><th>文件</th><th>上传到</th><th>进度</th><th>通知</th><th>可上传时间</th><th>操作</th></tr></thead>
                  <tbody>
                    {uploadBatches.length ? uploadBatches.map((batch) => (
                      <tr key={batch.id} tabIndex={0} onClick={() => void loadUploadBatchDetail(batch.id)} onKeyDown={(event) => { if (event.target !== event.currentTarget || !['Enter', ' '].includes(event.key)) return; event.preventDefault(); void loadUploadBatchDetail(batch.id); }} title="按 Enter 或空格打开详情">
                        <td>#{batch.id}</td>
                        <td><span className={uploadStatusPillClass(batch.status)}>{uploadStatusLabel(batch.status)}</span></td>
                        <td className="path-cell">{batch.seriesPath}</td>
                        <td>{batch.fileCount}</td>
                        <td className="upload-batch-destination"><strong>{batch.providerName || '-'}</strong>{batch.remoteRoot && <small>{batch.remoteRoot}</small>}</td>
                        <td><UploadBatchProgress batch={batch} /></td>
                        <td><span className={uploadBatchNotificationSummary(batch).className}>{uploadBatchNotificationSummary(batch).label}</span></td>
                        <td>{formatStoredTime(batch.readyAt, displayTimezone)}</td>
                        <td><ActionIconButton label={`查看上传批次 ${batch.id} 详情`} icon={Eye} onClick={(event) => { event.stopPropagation(); void loadUploadBatchDetail(batch.id); }} /></td>
                      </tr>
                    )) : <tr><td colSpan={9} className="empty-cell">暂无上传批次。</td></tr>}
                  </tbody>
                </table>
              </div>
              <div className="pagination-bar">
                <span>共 {uploadTotal} 条，第 {uploadPage} / {Math.max(1, Math.ceil(uploadTotal / taskPageSize))} 页</span>
                <div className="inline-actions"><ActionIconButton label="上一页" icon={ChevronLeft} disabled={refreshingUploads || uploadPage <= 1} onClick={() => void refreshUploads(uploadPage - 1)} /><ActionIconButton label="下一页" icon={ChevronRight} disabled={refreshingUploads || uploadPage >= Math.ceil(uploadTotal / taskPageSize)} onClick={() => void refreshUploads(uploadPage + 1)} /></div>
              </div>
            </Card>
          </>}

          {uploadView === 'providers' && <Card title="Provider 账号" action={<div className="inline-actions"><ActionIconButton label="刷新 Provider 账号" icon={RefreshCw} loading={refreshingUploadProviders} disabled={refreshingUploadProviders} onClick={() => void refreshUploadProviders()} /><button className="icon-text-button" type="button" onClick={() => setNewUploadProviderOpen(true)}><Plus size={16} />添加 Provider</button></div>}>
            <div className="task-table-wrap">
              <table className="task-table upload-provider-table">
                <thead><tr><th>名称</th><th>类型</th><th>授权</th><th>授权设备</th><th>目录配置</th><th>状态</th><th>操作</th></tr></thead>
                <tbody>
                  {uploadProviders.length ? uploadProviders.map((provider) => {
                    const directoryCount = watchDirs.filter((dir) => (dir.uploadConfigs ?? []).some((item) => item.providerId === provider.id)).length;
                    const needsAuthorization = uploadProviderNeedsAuthorization(provider);
                    return (
                      <tr key={provider.id}>
                        <td><strong>{provider.name}</strong></td>
                        <td>{uploadProviderTypes.find((item) => item.type === provider.type)?.name ?? provider.type}</td>
                        <td>{['115cookie', '115open', 'baidupan', 'baidupcs'].includes(provider.type) ? <span className={needsAuthorization ? 'pill warn' : 'pill ok'}>{needsAuthorization ? '未授权' : '已授权'}</span> : <span className="pill ignored">按类型配置</span>}</td>
                        <td>{provider.type === '115cookie' && provider.hasCookie ? uploadAuthDeviceName(provider.authDevice, provider.type, uploadProviderTypes) : '-'}</td>
                        <td>{directoryCount ? <button className="secondary upload-provider-directory-button" type="button" onClick={() => setUploadProviderUsage(provider)}>{directoryCount} 个目录</button> : <span className="pill ignored">未使用</span>}</td>
                        <td><span className={provider.enabled ? 'pill ok' : 'pill ignored'}>{provider.enabled ? '可用' : '停用'}</span></td>
                        <td><div className="table-actions upload-provider-actions"><ActionIconButton label={`编辑 Provider ${provider.name}`} icon={Pencil} onClick={() => setUploadProviderModal(provider)} />{['115cookie', '115open', 'baidupan', 'baidupcs'].includes(provider.type) && <ActionIconButton label={`${needsAuthorization ? '授权' : '重新授权'} ${provider.name}`} icon={KeyRound} onClick={() => openUploadAuthorization(provider)} />}<ActionIconButton label={`检查连接 ${provider.name}`} icon={CircleGauge} loading={checkingUploadProviderID === provider.id} disabled={checkingUploadProviderID === provider.id || needsAuthorization} onClick={() => void checkUploadProvider(provider)} /><ActionIconButton label={`删除 Provider ${provider.name}`} icon={Trash2} tone="danger" onClick={() => void deleteUploadProvider(provider)} /></div></td>
                      </tr>
                    );
                  }) : <tr><td colSpan={7} className="empty-cell">尚未添加 Provider。先添加并授权账号，再到媒体目录中配置上传步骤。</td></tr>}
                </tbody>
              </table>
            </div>
          </Card>}

          {uploadView === 'notifications' && <Card title="通知模板" action={<div className="inline-actions"><ActionIconButton label="刷新通知模板" icon={RefreshCw} onClick={() => void loadUploadNotificationTemplates()} /><button className="icon-text-button" type="button" onClick={() => setNewUploadNotificationTemplateOpen(true)}><Plus size={16} />添加模板</button></div>}>
            <div className="task-table-wrap">
              <table className="task-table upload-notification-template-table">
                <thead><tr><th>名称</th><th>请求地址</th><th>引用变量</th><th>目录配置</th><th>操作</th></tr></thead>
                <tbody>
                  {uploadNotificationTemplates.length ? uploadNotificationTemplates.map((template) => {
                    const variables = notificationTemplateVariables(template.headersTemplate, template.payloadTemplate);
                    const routeCount = watchDirs.reduce((count, dir) => count + (dir.uploadConfigs ?? []).filter((route) => route.notificationTemplateId === template.id).length, 0);
                    return (
                      <tr key={template.id}>
                        <td><strong>{template.name}</strong></td>
                        <td className="path-cell" title={template.url}>{template.url}</td>
                        <td><code>{['path', ...variables].map((name) => `{{${name}}}`).join(' ')}</code></td>
                        <td>{routeCount ? `${routeCount} 个` : <span className="pill ignored">未使用</span>}</td>
                        <td><div className="table-actions"><ActionIconButton label={`编辑通知模板 ${template.name}`} icon={Pencil} onClick={() => setUploadNotificationTemplateModal(template)} /><ActionIconButton label={`删除通知模板 ${template.name}`} icon={Trash2} tone="danger" onClick={() => deleteUploadNotificationTemplate(template)} /></div></td>
                      </tr>
                    );
                  }) : <tr><td colSpan={5} className="empty-cell">尚未添加通知模板。模板定义请求地址和 JSON payload，之后可在媒体目录的上传配置中选择。</td></tr>}
                </tbody>
              </table>
            </div>
          </Card>}

          {uploadView === 'notificationRecords' && <UploadNotificationRecordsCard
            records={uploadNotificationRecords}
            total={uploadNotificationTotal}
            page={uploadNotificationPage}
            pageSize={taskPageSize}
            status={uploadNotificationStatusFilter}
            path={uploadNotificationPathFilter}
            refreshing={refreshingUploadNotifications}
            timezone={displayTimezone}
            onRefresh={() => void refreshUploadNotificationRecords()}
            onStatusChange={(status) => { setUploadNotificationStatusFilter(status); void refreshUploadNotificationRecords(1, status, appliedUploadNotificationPathFilter); }}
            onPathChange={setUploadNotificationPathFilter}
            onApply={applyUploadNotificationFilters}
            onReset={resetUploadNotificationFilters}
            onPageChange={(page) => void refreshUploadNotificationRecords(page)}
          />}
        </section>
      )}

        {activePage === 'tasks' && (
        <section className="page-grid task-page-grid">
          <Card title="任务列表" action={<div className="inline-actions"><button className="secondary icon-text-button" type="button" onClick={() => setRecentArtifactsOpen(true)}><History size={16} />最近产物</button><button className="secondary icon-text-button" onClick={() => void retrySelectedTasks()} disabled={retryingTasks || retryableSelectedTaskIds.length === 0}><RotateCcw size={16} />{retryingTasks ? '重试中' : `重试${retryableSelectedTaskIds.length ? ` (${retryableSelectedTaskIds.length})` : ''}`}</button><button className="secondary icon-text-button" onClick={() => void ignoreSelectedTasks()} disabled={ignoringTasks || ignorableSelectedTaskIds.length === 0}><Ban size={16} />{ignoringTasks ? '忽略中' : `忽略失败${ignorableSelectedTaskIds.length ? ` (${ignorableSelectedTaskIds.length})` : ''}`}</button><button className="danger icon-text-button" onClick={cancelActiveTasks} disabled={cancelingTasks || activeTaskCount === 0}><X size={16} />{cancelingTasks ? '取消中' : '取消全部活动任务'}</button></div>}>
            <div className="task-status-tabs" role="group" aria-label="任务状态过滤">
              {taskStatusFilters.map((status) => (
                <button className={taskStatusFilter === status.value ? 'status-tab active' : 'status-tab'} type="button" key={status.value} aria-pressed={taskStatusFilter === status.value} onClick={() => selectTaskStatusFilter(status.value)}>
                  {status.label}
                </button>
              ))}
            </div>
            <form className="task-filters" onSubmit={(event) => { event.preventDefault(); applyTaskFilters(); }}>
              <label>任务批次
                <select value={taskRunFilter} onChange={(event) => setTaskRunFilter(event.target.value)}>
                  <option value="">全部批次</option>
                  {taskRuns.map((run) => <option key={run.id} value={run.id} title={run.scopePath}>{taskRunFilterLabel(run, displayTimezone)}</option>)}
                </select>
              </label>
              <label>路径<input value={taskPathFilter} onChange={(event) => setTaskPathFilter(event.target.value)} placeholder="输入路径关键字" /></label>
              <label>开始时间（{displayTimezone}）<input type="datetime-local" value={taskFromFilter} onChange={(event) => setTaskFromFilter(event.target.value)} /></label>
              <label>结束时间（{displayTimezone}）<input type="datetime-local" value={taskToFilter} onChange={(event) => setTaskToFilter(event.target.value)} /></label>
              <div className="filter-actions list-toolbar-icons">
                <ActionIconButton label="应用过滤" icon={Filter} type="submit" />
                <ActionIconButton label="重置过滤" icon={RotateCcw} onClick={resetTaskFilters} />
              </div>
            </form>
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
                      <td><span className={taskStatusPillClass(task.status)}>{taskStatusLabel(task.status)}</span></td>
                      <td>{taskTypeLabel(task.type)}</td>
                      <td className="path-cell">{task.mediaPath || '-'}</td>
                      <td>{formatStoredTime(task.createdAt, displayTimezone)}</td>
                      <td className="path-cell">{task.errorSummary || '-'}</td>
                      <td><div className="table-actions"><ActionIconButton label={`在文件管理器中显示任务 ${task.id}`} icon={FolderOpen} disabled={!task.mediaPath} onClick={(event) => { event.stopPropagation(); void revealPath(task.mediaPath); }} /><ActionIconButton label={`查看任务 ${task.id} 详情`} icon={Eye} onClick={(event) => { event.stopPropagation(); void loadTaskDetail(task.id); }} /></div></td>
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
                <ActionIconButton label="上一页" icon={ChevronLeft} disabled={taskPage <= 1} onClick={() => loadTasks(taskPage - 1)} />
                <ActionIconButton label="下一页" icon={ChevronRight} disabled={taskPage >= Math.ceil(taskTotal / taskPageSize)} onClick={() => loadTasks(taskPage + 1)} />
              </div>
            </div>
          </Card>

          {recentArtifactsOpen && <RecentArtifactsModal artifacts={artifacts} timezone={displayTimezone} onClose={() => setRecentArtifactsOpen(false)} />}
          {selectedTask && <TaskDetailModal detail={selectedTask} timezone={displayTimezone} onClose={() => setSelectedTask(null)} />}
        </section>
      )}
        </div>
      {selectedUploadBatch && <UploadBatchDetailModal detail={selectedUploadBatch} timezone={displayTimezone} actionTargetID={uploadTargetActionID} onClose={() => setSelectedUploadBatch(null)} onRetry={(target) => void actOnUploadTarget(target, 'retry')} onCancel={(target) => void actOnUploadTarget(target, 'cancel')} />}
      {rescanOpen && <RescanModal scope={rescanScope} target={rescanTarget} watchDirId={rescanWatchDirId} useCustomProcessing={rescanUseCustomProcessing} processing={rescanProcessing} directories={watchDirs} rescanning={rescanning} onClose={() => setRescanOpen(false)} onScopeChange={(value) => { setRescanScope(value); setRescanTarget(''); setRescanWatchDirId(''); }} onTargetChange={setRescanTarget} onWatchDirIdChange={(value) => { setRescanWatchDirId(value); setRescanTarget(''); }} onUseCustomProcessingChange={(value) => { setRescanUseCustomProcessing(value); if (value) setRescanProcessing(outputProcessingFromConfig(config)); }} onProcessingChange={(patch) => setRescanProcessing((value) => ({ ...value, ...patch }))} onBrowsePath={() => { const rootPath = rescanScope === 'dir' ? watchDirs.find((dir) => String(dir.id) === rescanWatchDirId)?.path ?? '' : ''; void browseDirectory({ title: '选择扫描路径', value: rescanTarget || rootPath, rootPath: rootPath || undefined, onSelect: setRescanTarget }); }} onSubmit={() => void rescan()} />}
      {addWatchDirOpen && <WatchDirModal title="添加媒体目录" submitLabel="添加" saving={savingWatchDir} dirty={watchDirDraftDirty} path={newWatchDir} watchEnabled={newWatchDirWatchEnabled} useGlobalProcessing={newWatchDirUseGlobalProcessing} processing={newWatchDirProcessing} uploadConfigs={newWatchDirUploadConfigs} providers={uploadProviders} notificationTemplates={uploadNotificationTemplates} onPathChange={setNewWatchDir} onWatchEnabledChange={setNewWatchDirWatchEnabled} onUseGlobalProcessingChange={(value) => { setNewWatchDirUseGlobalProcessing(value); if (!value) setNewWatchDirProcessing(outputProcessingFromConfig(config)); }} onProcessingChange={(patch) => setNewWatchDirProcessing((value) => ({ ...value, ...patch }))} onUploadConfigsChange={setNewWatchDirUploadConfigs} onAddProvider={() => setNewUploadProviderOpen(true)} onAuthorizeProvider={openUploadAuthorization} onBrowseRemoteDirectory={browseRemoteDirectory} onClose={() => requestDiscardChanges(watchDirDraftDirty, () => setAddWatchDirOpen(false))} onBrowsePath={() => void browseDirectory({ title: '选择媒体目录', value: newWatchDir, onSelect: setNewWatchDir })} onSubmit={() => void addWatchDir()} />}
      {editingWatchDir && <WatchDirModal title="编辑媒体目录" submitLabel="保存" saving={savingWatchDir} dirty={watchDirDraftDirty} path={editingWatchDirPath} watchEnabled={editingWatchDirWatchEnabled} useGlobalProcessing={editingWatchDirUseGlobalProcessing} processing={editingWatchDirProcessing} uploadConfigs={editingWatchDirUploadConfigs} providers={uploadProviders} notificationTemplates={uploadNotificationTemplates} onPathChange={setEditingWatchDirPath} onWatchEnabledChange={setEditingWatchDirWatchEnabled} onUseGlobalProcessingChange={(value) => { setEditingWatchDirUseGlobalProcessing(value); if (!value && editingWatchDirUseGlobalProcessing) setEditingWatchDirProcessing(outputProcessingFromConfig(config)); }} onProcessingChange={(patch) => setEditingWatchDirProcessing((value) => ({ ...value, ...patch }))} onUploadConfigsChange={setEditingWatchDirUploadConfigs} onAddProvider={() => setNewUploadProviderOpen(true)} onAuthorizeProvider={openUploadAuthorization} onBrowseRemoteDirectory={browseRemoteDirectory} onClose={() => requestDiscardChanges(watchDirDraftDirty, () => setEditingWatchDir(null))} onBrowsePath={() => void browseDirectory({ title: '选择媒体目录', value: editingWatchDirPath, onSelect: setEditingWatchDirPath })} onSubmit={() => void submitEditWatchDir()} />}
      {uploadProviderUsage && <UploadProviderUsageModal provider={uploadProviderUsage} watchDirs={watchDirs} onClose={() => setUploadProviderUsage(null)} onEditDirectory={(dir) => { setUploadProviderUsage(null); openEditWatchDir(dir); }} />}
      {(newUploadProviderOpen || uploadProviderModal) && <UploadProviderModal provider={uploadProviderModal ?? undefined} providerTypes={uploadProviderTypes} saving={savingUploadProvider} onClose={(dirty) => requestDiscardChanges(dirty, () => { setNewUploadProviderOpen(false); setUploadProviderModal(null); })} onSubmit={(provider) => void saveUploadProvider(provider)} />}
      {(newUploadNotificationTemplateOpen || uploadNotificationTemplateModal) && <UploadNotificationTemplateModal template={uploadNotificationTemplateModal ?? undefined} saving={savingUploadNotificationTemplate} onClose={(dirty) => requestDiscardChanges(dirty, () => { setNewUploadNotificationTemplateOpen(false); setUploadNotificationTemplateModal(null); })} onSubmit={(template) => void saveUploadNotificationTemplate(template)} />}
      {uploadOpen115Provider && <UploadOpen115Modal provider={uploadOpen115Provider} clientID={open115ClientID} auth={open115Auth} tokens={open115Tokens} showTokens={showOpen115Tokens} saving={savingUploadProvider} onClientIDChange={setOpen115ClientID} onTokensChange={setOpen115Tokens} onToggleTokenVisibility={() => setShowOpen115Tokens((value) => !value)} onClose={requestCloseUploadOpen115Modal} onStartAuth={() => void startOpen115Auth()} onImport={() => void importOpen115Tokens()} />}
      {uploadBaiduOpenProvider && <BaiduOpenAuthorizationModal provider={uploadBaiduOpenProvider} credentials={baiduOpenCredentials} mode={baiduOpenMode} auth={baiduOpenAuth} authConfig={baiduOpenAuthConfig} showTokens={showBaiduOpenTokens} saving={savingUploadProvider} onChange={setBaiduOpenCredentials} onModeChange={setBaiduOpenMode} onToggleTokenVisibility={() => setShowBaiduOpenTokens((value) => !value)} onClose={() => setUploadBaiduOpenProvider(null)} onSaveApplication={() => void saveBaiduOpenApplication()} onSaveSettings={() => void saveBaiduOpenAuthorizationSettings()} onStartAuthorization={() => void startBaiduOpenAuthorization()} onImportTokens={() => void importBaiduOpenTokens()} />}
      {uploadBaiduPCSProvider && <BaiduPCSAuthorizationModal provider={uploadBaiduPCSProvider} credentials={baiduPCSCredentials} showSecrets={showBaiduPCSSecrets} saving={savingUploadProvider} onChange={setBaiduPCSCredentials} onToggleVisibility={() => setShowBaiduPCSSecrets((value) => !value)} onClose={() => setUploadBaiduPCSProvider(null)} onSave={() => void saveBaiduPCSAuthorization()} />}
      {uploadCookieProvider && <UploadCookieModal provider={uploadCookieProvider} devices={uploadAuthDevices(uploadCookieProvider.type, uploadProviderTypes)} device={uploadCookieDevice} cookie={uploadCookieValue} auth={cookieAuth} saving={savingUploadProvider} onDeviceChange={setUploadCookieDevice} onCookieChange={setUploadCookieValue} onClose={requestCloseUploadCookieModal} onSave={() => void saveUploadCookie()} onStartAuth={() => void startCookieAuth()} />}
      {batchEpisodeOpen && <BatchEpisodeModal count={selectedRenamePaths.length} season={batchSeason} mode={batchEpisodeMode} offset={batchEpisodeOffset} start={batchEpisodeStart} applying={applyingBatchEpisode} progress={batchEpisodeProgress} onClose={() => setBatchEpisodeOpen(false)} onSeasonChange={setBatchSeason} onModeChange={setBatchEpisodeMode} onOffsetChange={setBatchEpisodeOffset} onStartChange={setBatchEpisodeStart} onSubmit={() => void applyBatchEpisodeFix()} />}
      {tmdbMatchOpen && <TmdbMatchModal count={selectedRenamePaths.length} query={tmdbQuery} results={tmdbResults} searching={searchingTmdb} applyingShowId={applyingTmdbShowId} applyProgress={tmdbApplyProgress} applyTotal={tmdbApplyTotal} onQueryChange={setTmdbQuery} onSearch={() => void searchTmdbShows()} onApply={(show) => void applyTmdbShowToSelected(show)} onClose={() => setTmdbMatchOpen(false)} />}
      {auditTmdbMatchOpen && <TmdbMatchModal title="选择核对剧集" description="选择后会将 TMDB ID 用于剧集缺漏判断。" applyLabel="选择剧集" query={tmdbQuery} results={tmdbResults} searching={searchingTmdb} applyingShowId={null} applyProgress={0} applyTotal={0} onQueryChange={setTmdbQuery} onSearch={() => void searchTmdbShows()} onApply={applyTmdbShowToAudit} onClose={() => setAuditTmdbMatchOpen(false)} />}
      {addEmbyKeyOpen && <AddEmbyKeyModal title={newEmbyKeyTitle} apiKey={newEmbyKeyValue} saving={savingEmbyKey} dirty={Boolean(newEmbyKeyTitle || newEmbyKeyValue)} onTitleChange={setNewEmbyKeyTitle} onAPIKeyChange={setNewEmbyKeyValue} onClose={() => requestDiscardChanges(Boolean(newEmbyKeyTitle || newEmbyKeyValue), () => setAddEmbyKeyOpen(false))} onSubmit={() => void saveEmbyAPIKey()} />}
      {tmdbEpisodeDetail && <TmdbEpisodeDetailModal detail={tmdbEpisodeDetail} language={renameLanguage} refreshing={loadingTmdbEpisodeDetail} onRefresh={() => void openTmdbEpisodeDetail({ tmdbShowId: tmdbEpisodeDetail.showId, season: tmdbEpisodeDetail.season, episode: tmdbEpisodeDetail.episode } as RenamePreviewItem, true)} onClose={() => setTmdbEpisodeDetail(null)} />}
      {renameHistoryOpen && <RenameHistoryModal history={renameHistory} undoingId={undoingHistoryId} loading={loadingRenameHistory} timezone={displayTimezone} onClose={() => setRenameHistoryOpen(false)} onRefresh={() => void loadRenameHistory()} onOpenDetails={setSelectedHistoryBatch} onUndo={(id) => void undoRenameBatch(id)} />}
      {selectedHistoryBatch && <RenameHistoryDetailsModal batch={selectedHistoryBatch} undoCheck={undoCheckResult?.batch?.id === selectedHistoryBatch.id ? undoCheckResult : null} timezone={displayTimezone} onClose={() => setSelectedHistoryBatch(null)} />}
      {renameTemplateEditorOpen && <RenameTemplateEditorModal value={renameTemplate} matchPattern={renameMatchPattern} sample={renamePreview[0]?.currentName || renamePath} placeholders={renamePlaceholders} onChange={setRenameTemplate} onMatchPatternChange={setRenameMatchPattern} onClose={() => setRenameTemplateEditorOpen(false)} />}
      {targetPathEditor && <TargetPathEditorModal value={targetPathEditor.value} dirty={targetPathDraftDirty} saving={recalculatingRenamePaths.includes(targetPathEditor.path)} onChange={(value) => setTargetPathEditor({ ...targetPathEditor, value })} onClose={() => requestDiscardChanges(targetPathDraftDirty, () => setTargetPathEditor(null))} onSubmit={() => void applyTargetPathEdit()} />}
      {directoryPicker && <DirectoryPicker title={directoryPicker.title} initialPath={directoryPicker.value} rootPath={directoryPicker.rootPath} onClose={() => setDirectoryPicker(null)} onSelect={(path) => { directoryPicker.onSelect(path); setDirectoryPicker(null); }} />}
      {remoteDirectoryPicker && <RemoteDirectoryPicker provider={remoteDirectoryPicker.provider} initialPath={remoteDirectoryPicker.value} cache={remoteDirectoryCacheRef.current} requests={remoteDirectoryRequestCacheRef.current} onClose={() => setRemoteDirectoryPicker(null)} onSelect={(path) => { remoteDirectoryPicker.onSelect(path); setRemoteDirectoryPicker(null); }} />}
      {confirmation && <ConfirmDialog request={confirmation} pending={confirming} onCancel={() => setConfirmation(null)} onConfirm={() => void acceptConfirmation()} />}
      </section>
    </main>
  );
}

function newUploadProviderDraft(): UploadProvider {
  return {
    id: 0,
    name: '',
    type: '115cookie',
    enabled: true,
    userAgent: '',
    requestIntervalMs: 500,
    hasCookie: false,
    hasCredentials: false,
    authDevice: '',
    preuploadBeforeRapid: false,
    createdAt: '',
    updatedAt: ''
  };
}

function newUploadNotificationTemplateDraft(): UploadNotificationTemplate {
  return {
    id: 0,
    name: '',
    url: '',
    headersTemplate: `{
  "X-Webhook-Token": "{{webhook_token}}"
}`,
    payloadTemplate: `{
  "event": "change",
  "source_path": "{{path}}",
  "is_dir": true
}`,
    createdAt: '',
    updatedAt: ''
  };
}

function newDirectoryUploadConfig(providerId: number): UploadProviderRoute {
  return {
    providerId,
    enabled: true,
    remoteRoot: '/',
    collisionPolicy: 'fail',
    includeTypes: uploadTypeOptions.map((option) => option.value),
    notificationVariables: {}
  };
}

function notificationTemplateVariables(...templates: string[]) {
  const names = new Set<string>();
  for (const template of templates) {
    for (const match of template.matchAll(/\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}/g)) {
      if (match[1] !== 'path') names.add(match[1]);
    }
  }
  return Array.from(names);
}

function variablesForNotificationTemplate(template: UploadNotificationTemplate | undefined, current: Record<string, string> = {}) {
  if (!template) return {};
  const requiredNames = notificationTemplateVariables(template.headersTemplate, template.payloadTemplate);
  const required = new Set(requiredNames);
  return Object.fromEntries([
    ...requiredNames.map((name) => [name, current[name] ?? '']),
    ...Object.entries(current).filter(([name]) => !required.has(name))
  ]);
}

function orderedNotificationVariableEntries(template: UploadNotificationTemplate | undefined, current: Record<string, string>) {
  const requiredNames = template ? notificationTemplateVariables(template.headersTemplate, template.payloadTemplate) : [];
  const required = new Set(requiredNames);
  return [
    ...requiredNames.filter((name) => Object.prototype.hasOwnProperty.call(current, name)).map((name) => ({ name, value: current[name], required: true })),
    ...Object.entries(current).filter(([name]) => !required.has(name)).map(([name, value]) => ({ name, value, required: false }))
  ];
}

function uploadNotificationConfigError(route: UploadProviderRoute, templates: UploadNotificationTemplate[]) {
  if (!route.notificationTemplateId) return '';
  const template = templates.find((item) => item.id === route.notificationTemplateId);
  if (!template) return '所选通知模板不存在。';
  const variables = route.notificationVariables ?? {};
  const invalid = Object.keys(variables).find((name) => !/^[A-Za-z_][A-Za-z0-9_]*$/.test(name) || name === 'path');
  if (invalid) return `变量名“${invalid}”无效。`;
  const missing = notificationTemplateVariables(template.headersTemplate, template.payloadTemplate).filter((name) => !(name in variables));
  return missing.length ? `缺少模板变量：${missing.join('、')}。` : '';
}

function uploadCollisionPolicyLabel(value: UploadCollisionPolicy) {
  switch (value) {
    case 'replace':
      return '替换';
    case 'skip':
      return '跳过';
    case 'fail':
      return '冲突失败';
    default:
      return value || '-';
  }
}

function uploadContentTypeLabel(value: string) {
  return uploadTypeOptions.find((option) => option.value === value)?.label ?? value;
}

function UploadProviderUsageModal(props: { provider: UploadProvider; watchDirs: WatchDir[]; onClose: () => void; onEditDirectory: (dir: WatchDir) => void }) {
  const routes = props.watchDirs.flatMap((dir) => (dir.uploadConfigs ?? [])
    .filter((route) => route.providerId === props.provider.id)
    .map((route) => ({ dir, route })));

  return (
    <div className="modal-backdrop" role="presentation" onClick={props.onClose}>
      <section className="modal-card upload-provider-usage-modal" role="dialog" aria-modal="true" aria-labelledby="upload-provider-usage-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <div><h2 id="upload-provider-usage-title">目录配置</h2><small>{props.provider.name} · 汇总所有媒体目录中的上传路由</small></div>
          <IconCloseButton onClick={props.onClose} />
        </div>
        <div className="upload-provider-usage-summary">
          <strong>{routes.length}</strong>
          <span>个媒体目录正在引用此 Provider</span>
        </div>
        {routes.length ? (
          <div className="task-table-wrap">
            <table className="task-table upload-provider-usage-table">
              <thead><tr><th>媒体目录</th><th>目标路径</th><th>上传内容</th><th>冲突策略</th><th>状态</th><th>操作</th></tr></thead>
              <tbody>
                {routes.map(({ dir, route }) => {
                  const enabled = route.enabled && props.provider.enabled;
                  const statusLabel = !props.provider.enabled ? 'Provider 停用' : route.enabled ? '启用' : '步骤停用';
                  return <tr key={`${dir.id}-${route.id ?? route.providerId}`}>
                    <td className="path-cell" title={dir.path}>{dir.path}</td>
                    <td><code>{route.remoteRoot || '/'}</code></td>
                    <td className="upload-provider-usage-content" title={(route.includeTypes ?? []).map(uploadContentTypeLabel).join('、')}>{(route.includeTypes ?? []).length ? (route.includeTypes ?? []).map(uploadContentTypeLabel).join('、') : '-'}</td>
                    <td>{uploadCollisionPolicyLabel(route.collisionPolicy)}</td>
                    <td><span className={enabled ? 'pill ok' : 'pill ignored'}>{statusLabel}</span></td>
                    <td><ActionIconButton label={`编辑媒体目录 ${dir.path}`} icon={Pencil} onClick={() => props.onEditDirectory(dir)} /></td>
                  </tr>
                })}
              </tbody>
            </table>
          </div>
        ) : <p className="empty-cell">当前没有媒体目录引用此 Provider。</p>}
        <div className="inline-actions modal-actions"><button className="secondary" type="button" onClick={props.onClose}>关闭</button></div>
      </section>
    </div>
  );
}

function nextUploadTypeSelection(current: string[], value: string, checked: boolean) {
  const types = new Set(current);
  if (checked) types.add(value);
  else types.delete(value);
  return Array.from(types);
}

function UploadRouteProfile(props: { route: UploadProviderRoute; provider?: UploadProvider; notificationTemplates: UploadNotificationTemplate[]; onChange: (patch: Partial<UploadProviderRoute>) => void; onBrowseRemoteDirectory: (request: RemoteDirectoryPickerRequest) => void }) {
  const needsAuthorization = uploadProviderNeedsAuthorization(props.provider);
  const browseDisabled = !props.provider || needsAuthorization;
  const notificationTemplate = props.notificationTemplates.find((item) => item.id === props.route.notificationTemplateId);
  const notificationVariables = props.route.notificationVariables ?? {};
  const notificationVariableEntries = orderedNotificationVariableEntries(notificationTemplate, notificationVariables);
  const notificationError = uploadNotificationConfigError(props.route, props.notificationTemplates);

  function selectNotificationTemplate(value: string) {
    const notificationTemplateId = Number(value) || undefined;
    const template = props.notificationTemplates.find((item) => item.id === notificationTemplateId);
    props.onChange({
      notificationTemplateId,
      notificationVariables: variablesForNotificationTemplate(template, notificationVariables)
    });
  }

  function updateNotificationVariable(previousName: string, name: string, value: string) {
    const entries = Object.entries(notificationVariables).map(([key, currentValue]) => key === previousName ? [name, value] : [key, currentValue]);
    props.onChange({ notificationVariables: Object.fromEntries(entries) });
  }

  function addNotificationVariable() {
    let index = Object.keys(notificationVariables).length + 1;
    while (`variable_${index}` in notificationVariables) index++;
    props.onChange({ notificationVariables: { ...notificationVariables, [`variable_${index}`]: '' } });
  }

  return (
    <div className="upload-route-profile">
      <label>远端根目录<div className="path-input remote-root-input"><input aria-label="远端根目录" value={props.route.remoteRoot} placeholder="/Anime" readOnly required title={props.route.remoteRoot} /><button className="icon-text-button" type="button" aria-label="选择远端根目录" disabled={browseDisabled} title={needsAuthorization ? '请先授权 Provider' : '选择远端根目录'} onClick={() => props.provider && props.onBrowseRemoteDirectory({ provider: props.provider, value: props.route.remoteRoot, onSelect: (remoteRoot) => props.onChange({ remoteRoot }) })}><FolderOpen size={16} />选择</button></div><small>{needsAuthorization ? '请先授权 Provider，再选择已存在的远端目录。' : '只能从已存在的远端目录中选择；暂不支持单独映射本地子目录。'}</small></label><label>碰撞策略<select value={props.route.collisionPolicy} onChange={(event) => props.onChange({ collisionPolicy: event.target.value as UploadCollisionPolicy })}><option value="replace">同名内容不同则替换</option><option value="skip">同名内容不同则跳过</option><option value="fail">同名内容不同则失败</option></select></label>
      <details className="upload-content-details">
        <summary>上传内容 <span>{props.route.includeTypes?.length ?? 0} / {uploadTypeOptions.length}</span></summary>
        <fieldset className="upload-route-type-fieldset">
          <legend>文件类型</legend>
          <div className="upload-type-grid">
            {uploadTypeOptions.map((option) => <label className="checkbox-label" key={option.value}><input type="checkbox" checked={(props.route.includeTypes ?? []).includes(option.value)} onChange={(event) => props.onChange({ includeTypes: nextUploadTypeSelection(props.route.includeTypes ?? [], option.value, event.target.checked) })} />{option.label}</label>)}
          </div>
          <small>只上传所选类型；至少选择一项。</small>
        </fieldset>
      </details>
      <details className="upload-content-details upload-notification-details">
        <summary><span className="upload-detail-title"><Webhook size={15} aria-hidden="true" />上传完成通知</span><span>{notificationTemplate?.name ?? '不通知'}</span></summary>
        <div className="upload-notification-config">
          <label>通知模板<select value={props.route.notificationTemplateId ?? ''} onChange={(event) => selectNotificationTemplate(event.target.value)}>
            <option value="">不发送通知</option>
            {props.notificationTemplates.map((template) => <option key={template.id} value={template.id}>{template.name}</option>)}
          </select></label>
          {props.route.notificationTemplateId && <>
            <div className="notification-variable-heading"><strong>模板变量</strong><button className="secondary icon-text-button" type="button" onClick={addNotificationVariable}><Plus size={15} />添加变量</button></div>
            <div className="notification-variable-list">
              <div className="notification-variable-row builtin">
                <input aria-label="内置变量名" value="path" readOnly />
                <input aria-label="内置变量说明" value="远端番剧目录（系统自动填入）" readOnly />
                <span className="pill ok">内置</span>
              </div>
              {notificationVariableEntries.map(({ name, value, required }, index) => (
                <div className={required ? 'notification-variable-row builtin' : 'notification-variable-row'} key={required ? `template:${name}` : `custom:${index}`}>
                  <input aria-label="变量名" value={name} readOnly={required} onChange={(event) => updateNotificationVariable(name, event.target.value, value)} placeholder="provider_id" />
                  <input aria-label={`${name || '自定义'} 变量值`} value={value} onChange={(event) => updateNotificationVariable(name, name, event.target.value)} placeholder="此上传配置使用的值" />
                  {required ? <span className="pill ok">模板</span> : <button className="icon-button" type="button" title={`删除变量 ${name}`} aria-label={`删除变量 ${name}`} onClick={() => props.onChange({ notificationVariables: Object.fromEntries(Object.entries(notificationVariables).filter(([key]) => key !== name)) })}><Trash2 size={15} /></button>}
                </div>
              ))}
            </div>
            {notificationError && <small className="upload-selection-warning">{notificationError}</small>}
          </>}
        </div>
      </details>
    </div>
  );
}

function DirectoryUploadConfigsEditor(props: { configs: UploadProviderRoute[]; providers: UploadProvider[]; notificationTemplates: UploadNotificationTemplate[]; onChange: (configs: UploadProviderRoute[]) => void; onAddProvider: () => void; onAuthorizeProvider: (provider: UploadProvider) => void; onBrowseRemoteDirectory: (request: RemoteDirectoryPickerRequest) => void }) {
  useEffect(() => {
    let changed = false;
    const configs = props.configs.map((config) => {
      const template = props.notificationTemplates.find((item) => item.id === config.notificationTemplateId);
      if (!template) return config;
      const variables = variablesForNotificationTemplate(template, config.notificationVariables);
      if (Object.keys(variables).length === Object.keys(config.notificationVariables ?? {}).length) return config;
      changed = true;
      return { ...config, notificationVariables: variables };
    });
    if (changed) props.onChange(configs);
  }, [props.configs, props.notificationTemplates, props.onChange]);

  function addConfig() {
    const provider = props.providers.find((item) => item.enabled) ?? props.providers[0];
    if (!provider) return;
    props.onChange([...props.configs, newDirectoryUploadConfig(provider.id)]);
  }

  function updateConfig(index: number, patch: Partial<UploadProviderRoute>) {
    props.onChange(props.configs.map((config, currentIndex) => currentIndex === index ? { ...config, ...patch } : config));
  }

  return (
    <section className="directory-upload-step" aria-labelledby="directory-upload-step-title">
      <div className="directory-upload-step-header">
        <div><strong id="directory-upload-step-title"><CloudUpload size={16} aria-hidden="true" />上传</strong><small>{props.configs.length ? `${props.configs.length} 个目录级配置` : '未配置，不会上传'}</small></div>
        <div className="directory-upload-step-actions">
          <button className="secondary icon-text-button" type="button" onClick={props.onAddProvider}><Plus size={16} />添加 Provider</button>
          <button className="secondary icon-text-button" type="button" onClick={addConfig} disabled={!props.providers.length}><Plus size={16} />添加配置</button>
        </div>
      </div>
      {!props.providers.length && <div className="directory-upload-empty-action"><p className="settings-note">尚未添加 Provider。添加并授权账号后，即可为当前目录配置上传。</p></div>}
      {props.providers.length > 0 && !props.configs.length && <p className="settings-note">上传是当前媒体目录的独立处理步骤；添加配置后，处理完成的文件会发送到指定 Provider。</p>}
      <div className="directory-upload-configs">
        {props.configs.map((config, index) => {
          const provider = props.providers.find((item) => item.id === config.providerId);
          const needsAuthorization = uploadProviderNeedsAuthorization(provider);
          const providerDisabled = provider != null && !provider.enabled;
          return (
            <section className="directory-upload-config" key={config.id ?? `${config.providerId}-${index}`}>
              <div className="directory-upload-config-header">
                <label>Provider<select value={config.providerId ?? ''} onChange={(event) => updateConfig(index, { providerId: Number(event.target.value), remoteRoot: '/' })}>
                  {props.providers.map((item) => <option key={item.id} value={item.id}>{item.name} · {item.type}{item.enabled ? '' : '（已停用）'}</option>)}
                </select></label>
                <div className="directory-upload-config-state">
                  {needsAuthorization && <span className="pill warn">待授权</span>}
                  {providerDisabled && <span className="pill ignored">Provider 已停用</span>}
                  {!provider && <span className="pill bad">Provider 不存在</span>}
                  {needsAuthorization && provider && <button className="secondary directory-upload-auth-button" type="button" onClick={() => props.onAuthorizeProvider(provider)}>授权</button>}
                  <Toggle label="启用" checked={config.enabled} onChange={(enabled) => updateConfig(index, { enabled })} />
                  <button className="icon-button" type="button" title="删除上传配置" aria-label="删除上传配置" onClick={() => props.onChange(props.configs.filter((_, currentIndex) => currentIndex !== index))}><Trash2 size={16} /></button>
                </div>
              </div>
              <UploadRouteProfile route={config} provider={provider} notificationTemplates={props.notificationTemplates} onChange={(patch) => updateConfig(index, patch)} onBrowseRemoteDirectory={props.onBrowseRemoteDirectory} />
              {needsAuthorization && <small className="upload-config-warning">该 Provider 尚未授权，启用后上传会失败。</small>}
            </section>
          );
        })}
      </div>
    </section>
  );
}

function UploadProviderModal(props: { provider?: UploadProvider; providerTypes: UploadProviderDescriptor[]; saving: boolean; onClose: (dirty: boolean) => void; onSubmit: (provider: UploadProvider) => void }) {
  const initialDraftRef = useRef<UploadProvider>(props.provider ? { ...props.provider } : newUploadProviderDraft());
  const [draft, setDraft] = useState<UploadProvider>(() => ({ ...initialDraftRef.current }));
  const editing = draft.id > 0;
  const dirty = JSON.stringify(draft) !== JSON.stringify(initialDraftRef.current);
  const descriptors = props.providerTypes.length ? props.providerTypes : [{ type: '115cookie', name: '115 Cookie', implemented: true, secretKeys: ['cookie'] }];
  const providerTypes = descriptors.some((descriptor) => descriptor.type === draft.type) ? descriptors : [...descriptors, { type: draft.type, name: draft.type, implemented: false, secretKeys: [] }];
  const selectedProviderType = providerTypes.find((descriptor) => descriptor.type === draft.type);
  const providerTypeUsable = !selectedProviderType || selectedProviderType.implemented || !draft.enabled;
  const canSubmit = !props.saving && !!draft.name.trim() && providerTypeUsable;

  return (
    <div className="modal-backdrop" role="presentation">
      <section className="modal-card upload-provider-modal" data-protect-draft={dirty ? 'true' : undefined} role="dialog" aria-modal="true" aria-labelledby="upload-provider-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <div><h2 id="upload-provider-title">{editing ? '编辑 Provider' : '添加 Provider'}</h2><small>Provider 代表一个独立账号实例；目录映射在各媒体目录中配置。</small></div>
          <IconCloseButton onClick={() => props.onClose(dirty)} disabled={props.saving} />
        </div>
        <form className="config-form" onSubmit={(event) => { event.preventDefault(); if (canSubmit) props.onSubmit(draft); }}>
          <label>显示名称<input autoFocus value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} placeholder="例如：115 主归档" required /></label>
          <label>Provider 类型<select value={draft.type} disabled={editing || props.saving} onChange={(event) => setDraft({ ...draft, type: event.target.value })}>{providerTypes.map((providerType) => <option key={providerType.type} value={providerType.type} disabled={!providerType.implemented && providerType.type !== draft.type}>{providerType.name}{providerType.implemented ? '' : '（尚未安装）'}</option>)}</select><small>{editing ? 'Provider 类型在创建后固定。' : (selectedProviderType?.implemented ? '已安装的 Provider 可立即配置授权。' : '此 Provider 类型已预留，但尚未安装上传实现。')}</small></label>
          <label>自定义 User-Agent（可选）<input value={draft.userAgent} onChange={(event) => setDraft({ ...draft, userAgent: event.target.value })} placeholder="Mozilla/5.0" /></label>
          {['115open', 'baidupcs'].includes(draft.type) && <label>上传 API 请求间隔（毫秒）<input type="number" min={250} max={10000} required value={draft.requestIntervalMs} onChange={(event) => setDraft({ ...draft, requestIntervalMs: Number(event.target.value) })} /><small>限制 250–10000 毫秒；默认 500 毫秒。</small></label>}
          {draft.type === 'baidupcs' && <div className="config-toggle-field"><Toggle label="MD5 计算完成前预先上传分片" checked={draft.preuploadBeforeRapid} onChange={(enabled) => setDraft({ ...draft, preuploadBeforeRapid: enabled })} /><small>开启后会在计算完整 MD5 的同时按网页端策略并发上传分片；默认关闭，优先尝试 rapidupload 秒传。</small></div>}
          <Toggle label="启用此 Provider" checked={draft.enabled} onChange={(enabled) => setDraft({ ...draft, enabled })} />
          <div className="inline-actions modal-actions"><button className="secondary" type="button" onClick={() => props.onClose(dirty)} disabled={props.saving}>取消</button><button type="submit" disabled={!canSubmit}>{props.saving ? '保存中' : (!editing && ['115cookie', '115open', 'baidupan', 'baidupcs'].includes(draft.type) ? '保存并授权' : '保存 Provider')}</button></div>
        </form>
      </section>
    </div>
  );
}

function UploadNotificationTemplateModal(props: { template?: UploadNotificationTemplate; saving: boolean; onClose: (dirty: boolean) => void; onSubmit: (template: UploadNotificationTemplate) => void }) {
  const initialDraftRef = useRef<UploadNotificationTemplate>(props.template ? { ...props.template } : newUploadNotificationTemplateDraft());
  const [draft, setDraft] = useState<UploadNotificationTemplate>(() => ({ ...initialDraftRef.current }));
  const dirty = JSON.stringify(draft) !== JSON.stringify(initialDraftRef.current);
  let payloadValid = false;
  try {
    const parsed = JSON.parse(draft.payloadTemplate);
    payloadValid = parsed != null && !Array.isArray(parsed) && typeof parsed === 'object';
  } catch {
    payloadValid = false;
  }
  let headersValid = false;
  let headerBindings: Array<{ name: string; variables: string[] }> = [];
  try {
    const parsed = JSON.parse(draft.headersTemplate || '{}');
    const headerNamePattern = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/;
    headersValid = parsed != null
      && !Array.isArray(parsed)
      && typeof parsed === 'object'
      && Object.entries(parsed).every(([name, value]) => headerNamePattern.test(name) && typeof value === 'string' && !/[\r\n]/.test(value));
    if (headersValid) {
      headerBindings = Object.entries(parsed as Record<string, string>).map(([name, value]) => ({
        name,
        variables: notificationTemplateVariables(value)
      }));
    }
  } catch {
    headersValid = false;
  }
  let urlValid = false;
  try {
    const parsed = new URL(draft.url);
    urlValid = parsed.protocol === 'http:' || parsed.protocol === 'https:';
  } catch {
    urlValid = false;
  }
  const variables = ['path', ...notificationTemplateVariables(draft.headersTemplate, draft.payloadTemplate)];
  const canSubmit = !props.saving && Boolean(draft.name.trim()) && urlValid && headersValid && payloadValid;

  return (
    <div className="modal-backdrop" role="presentation">
      <section className="modal-card upload-notification-template-modal" data-protect-draft={dirty ? 'true' : undefined} role="dialog" aria-modal="true" aria-busy={props.saving} aria-labelledby="upload-notification-template-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <div><h2 id="upload-notification-template-title">{draft.id ? '编辑通知模板' : '添加通知模板'}</h2><small>HTTP POST · application/json</small></div>
          <IconCloseButton onClick={() => props.onClose(dirty)} disabled={props.saving} />
        </div>
        <form className="config-form notification-template-form" onSubmit={(event) => { event.preventDefault(); if (canSubmit) props.onSubmit(draft); }}>
          <label>模板名称<input autoFocus value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} placeholder="例如：媒体库刷新" required /></label>
          <label>请求地址<input type="url" value={draft.url} onChange={(event) => setDraft({ ...draft, url: event.target.value })} placeholder="https://example.com/api/notify" required /></label>
          <label>Header JSON<textarea className="code-textarea" value={draft.headersTemplate} onChange={(event) => setDraft({ ...draft, headersTemplate: event.target.value })} rows={6} spellCheck={false} aria-invalid={!headersValid} placeholder={'{\n  "X-Webhook-Token": "{{webhook_token}}"\n}'} /></label>
          {headersValid && headerBindings.length > 0 && <div className="notification-template-variables"><span>Header 绑定</span>{headerBindings.map(({ name, variables: headerVariables }) => <code key={name}>{headerVariables.length ? `${name}: ${headerVariables.map((variable) => `{{${variable}}}`).join(', ')}` : `${name}: 固定值`}</code>)}</div>}
          <label>JSON payload<textarea className="code-textarea" value={draft.payloadTemplate} onChange={(event) => setDraft({ ...draft, payloadTemplate: event.target.value })} rows={13} spellCheck={false} aria-invalid={!payloadValid} /></label>
          <div className="notification-template-variables"><span>变量</span>{variables.map((name) => <code key={name}>{`{{${name}}}`}</code>)}</div>
          {!headersValid && <small className="upload-selection-warning">Header 必须是有效的 JSON 对象，并且每个值都必须是字符串。</small>}
          {!payloadValid && <small className="upload-selection-warning">Payload 必须是有效的 JSON 对象。</small>}
          <div className="inline-actions modal-actions"><button className="secondary" type="button" onClick={() => props.onClose(dirty)} disabled={props.saving}>取消</button><button type="submit" disabled={!canSubmit}>{props.saving ? '保存中' : '保存模板'}</button></div>
        </form>
      </section>
    </div>
  );
}

function UploadOpen115Modal(props: { provider: UploadProvider; clientID: string; auth: Open115AuthStatus | null; tokens: { accessToken: string; refreshToken: string }; showTokens: boolean; saving: boolean; onClientIDChange: (value: string) => void; onTokensChange: (value: { accessToken: string; refreshToken: string }) => void; onToggleTokenVisibility: () => void; onClose: () => void; onStartAuth: () => void; onImport: () => void }) {
  const qrURL = props.auth ? '/api/upload/providers/' + props.provider.id + '/auth/115open/' + encodeURIComponent(props.auth.sessionId) + '/qrcode' : '';
  const terminal = Boolean(props.auth && ['authorized', 'expired', 'cancelled', 'error'].includes(props.auth.state));
  const authActive = isOpen115AuthorizationActive(props.auth);
  const canStart = Boolean(props.clientID.trim() || props.provider.hasCredentials);
  return (
    <div className="modal-backdrop" role="presentation">
      <section className="modal-card upload-cookie-modal" role="dialog" aria-modal="true" aria-busy={props.saving} aria-labelledby="upload-open115-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <div><h2 id="upload-open115-title">115 Open 授权</h2><small>{props.provider.name}</small></div>
          <IconCloseButton onClick={props.onClose} disabled={props.saving} />
        </div>
        <div className="upload-auth-grid">
          <section className="upload-auth-panel">
            <h3>开放平台应用</h3>
            <label>Client ID
              <input autoFocus value={props.clientID} onChange={(event) => props.onClientIDChange(event.target.value)} placeholder={props.provider.hasCredentials ? '留空则使用已保存的 Client ID' : '填写自己的 115 Open AppID'} disabled={props.saving || authActive} />
            </label>
            <p className="settings-note">使用 PKCE 设备授权，不需要填写 AppSecret。</p>
            {!props.auth && <button type="button" onClick={props.onStartAuth} disabled={props.saving || !canStart}>{props.saving ? '请求中' : '开始扫码授权'}</button>}
            {props.auth && terminal && <button type="button" className="secondary" onClick={props.onStartAuth} disabled={props.saving || !canStart}>重新获取二维码</button>}
          </section>
          <section className="upload-auth-panel">
            <h3>扫码确认</h3>
            {props.auth ? (
              <>
                <img className="upload-auth-qr" src={qrURL} alt="115 Open 授权二维码" />
                <p className="settings-note" aria-live="polite" aria-atomic="true">{props.auth.message || props.auth.state}</p>
                {!terminal && <span className="pill running" role="status">授权进行中</span>}
              </>
            ) : <p className="settings-note">提交 Client ID 后会在此显示授权二维码。</p>}
          </section>
          <section className="upload-auth-panel upload-auth-import-panel">
            <h3>直接导入 Token</h3>
            <p className="settings-note">使用 OpenList、api.oplist.org 或其他 Client ID 获取的第三方凭据。</p>
            <label>Access Token
              <input type={props.showTokens ? 'text' : 'password'} value={props.tokens.accessToken} onChange={(event) => props.onTokensChange({ ...props.tokens, accessToken: event.target.value })} autoComplete="off" disabled={props.saving || authActive} />
            </label>
            <label>Refresh Token
              <input type={props.showTokens ? 'text' : 'password'} value={props.tokens.refreshToken} onChange={(event) => props.onTokensChange({ ...props.tokens, refreshToken: event.target.value })} autoComplete="off" disabled={props.saving || authActive} />
            </label>
            <div className="inline-actions">
              <button type="button" onClick={props.onImport} disabled={props.saving || authActive || (!props.tokens.accessToken.trim() && !props.tokens.refreshToken.trim())}>{props.saving ? '保存中' : '导入 Token'}</button>
              <button type="button" className="secondary" onClick={props.onToggleTokenVisibility} disabled={props.saving || authActive}>{props.showTokens ? '隐藏 Token' : '显示 Token'}</button>
            </div>
            <p className="settings-note">可从 <a href="https://api.oplist.org" target="_blank" rel="noreferrer">api.oplist.org</a> 等服务获取。建议同时填写两种 Token；后续刷新不需要 Client ID 或 AppKey。</p>
          </section>
        </div>
        <div className="inline-actions modal-actions"><button type="button" className={authActive ? 'danger' : 'secondary'} onClick={props.onClose} disabled={props.saving}>{authActive ? '结束授权并关闭' : '关闭'}</button></div>
      </section>
    </div>
  );
}

function BaiduOpenAuthorizationModal(props: { provider: UploadProvider; credentials: BaiduOpenCredentials; mode: string; auth: BaiduOpenAuthStatus | null; authConfig: BaiduOpenAuthConfig | null; showTokens: boolean; saving: boolean; onChange: (value: BaiduOpenCredentials) => void; onModeChange: (value: string) => void; onToggleTokenVisibility: () => void; onClose: () => void; onSaveApplication: () => void; onSaveSettings: () => void; onStartAuthorization: () => void; onImportTokens: () => void }) {
  const values = props.credentials;
  const update = (patch: Partial<BaiduOpenCredentials>) => props.onChange({ ...values, ...patch });
  const authActive = isBaiduOpenAuthorizationActive(props.auth);
  const authTerminal = Boolean(props.auth && !authActive);
  const canSaveApplication = Boolean(values.clientID.trim() && values.clientSecret.trim());
  const canStart = props.mode === 'broker_token_exchange'
    ? Boolean(values.brokerBaseURL.trim() && values.brokerClientID.trim() && (values.brokerToken.trim() || props.authConfig?.brokerTokenConfigured))
    : Boolean(values.clientID.trim() && (values.clientSecret.trim() || props.authConfig?.clientSecretConfigured));
  const canImport = Boolean(values.accessToken.trim() && values.refreshToken.trim());
  const localCallbackURL = props.authConfig?.callbackUrl || 'Loading callback URL...';
  const brokerCallbackURL = values.brokerBaseURL.trim()
    ? `${values.brokerBaseURL.trim().replace(/\/+$/, '')}/v1/callbacks/baidu`
    : (props.authConfig?.brokerCallbackUrl || 'Enter Broker base URL to calculate this address');
  return (
    <div className="modal-backdrop" role="presentation">
      <section className="modal-card upload-cookie-modal baiduopen-auth-modal" role="dialog" aria-modal="true" aria-busy={props.saving} aria-labelledby="upload-baiduopen-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header"><div><h2 id="upload-baiduopen-title">Baidu Open authorization</h2><small>{props.provider.name}</small></div><IconCloseButton onClick={props.onClose} disabled={props.saving} /></div>
        <div className="upload-auth-grid baiduopen-auth-grid">
          <section className="upload-auth-panel baiduopen-mode-panel">
            <h3>Authorization mode</h3>
            <label>Mode<select value={props.mode} onChange={(event) => props.onModeChange(event.target.value)} disabled={props.saving || authActive}><option value="official">Official direct</option><option value="broker_relay">OAuth Broker relay</option><option value="broker_token_exchange">OAuth Broker token exchange</option></select></label>
            <p className="settings-note">{props.mode === 'official' ? 'Register the direct callback address shown below in Baidu.' : props.mode === 'broker_relay' ? 'Register the Broker callback in Baidu and the local callback as the Broker return_uri allowlist.' : 'This mode completes inside the Broker and does not require a callback whitelist.'}</p>
          </section>
          <section className="upload-auth-panel baiduopen-callback-panel">
            <h3>Callback addresses</h3>
            <div className="baiduopen-callback-list">
              {props.mode === 'official' ? <div className="baiduopen-callback-item">
                <span>Baidu official redirect URI</span>
                <code>{localCallbackURL}</code>
              </div> : props.mode === 'broker_relay' ? <>
                <div className="baiduopen-callback-item">
                  <span>Baidu app callback whitelist</span>
                  <code>{brokerCallbackURL}</code>
                </div>
                <div className="baiduopen-callback-item">
                  <span>Broker return_uri allowlist</span>
                  <code>{localCallbackURL}</code>
                </div>
              </> : <div className="baiduopen-callback-empty">No callback address is required for Broker token exchange.</div>}
            </div>
          </section>
          <section className="upload-auth-panel baiduopen-application-panel">
            <h3>Baidu application</h3>
            <label>Client ID<input autoFocus value={values.clientID} onChange={(event) => update({ clientID: event.target.value })} autoComplete="off" disabled={props.saving || authActive} /></label>
            <label>Client Secret<input type={props.showTokens ? 'text' : 'password'} value={values.clientSecret} onChange={(event) => update({ clientSecret: event.target.value })} autoComplete="off" disabled={props.saving || authActive} placeholder={props.authConfig?.clientSecretConfigured ? 'Saved; leave blank to keep it' : ''} /></label>
            <button type="button" onClick={props.onSaveApplication} disabled={props.saving || authActive || !canSaveApplication}>{props.saving ? 'Saving...' : 'Save application credentials'}</button>
          </section>
          {props.mode !== 'official' && <section className="upload-auth-panel baiduopen-broker-panel">
            <h3>OAuth Broker</h3>
            <label>Broker base URL<input type="url" value={values.brokerBaseURL} onChange={(event) => update({ brokerBaseURL: event.target.value })} disabled={props.saving || authActive} placeholder="https://broker.example" /></label>
            <label>Broker client ID<input value={values.brokerClientID} onChange={(event) => update({ brokerClientID: event.target.value })} autoComplete="off" disabled={props.saving || authActive} /></label>
            <label>Broker token<input type={props.showTokens ? 'text' : 'password'} value={values.brokerToken} onChange={(event) => update({ brokerToken: event.target.value })} autoComplete="off" disabled={props.saving || authActive} placeholder={props.authConfig?.brokerTokenConfigured ? 'Saved; leave blank to keep it' : ''} /></label>
            <button type="button" onClick={props.onSaveSettings} disabled={props.saving || authActive || !canStart}>{props.saving ? 'Saving...' : 'Save mode and Broker'}</button>
          </section>}
          <section className="upload-auth-panel baiduopen-authorization-panel">
            <h3>Authorization</h3>
            {props.auth && <p className="settings-note" aria-live="polite">{props.auth.message || props.auth.state}</p>}
            {props.auth?.authorizationUrl && props.mode === 'broker_token_exchange' && <button type="button" onClick={() => window.open(props.auth?.authorizationUrl, '_blank', 'noopener,noreferrer')} disabled={props.saving || authActive}>Open Broker</button>}
            <button type="button" onClick={props.onStartAuthorization} disabled={props.saving || authActive || !canStart}>{authTerminal ? 'Restart authorization' : 'Start authorization'}</button>
            {authActive && <span className="pill running" role="status">Authorization in progress</span>}
          </section>
          <section className="upload-auth-panel upload-auth-import-panel baiduopen-import-panel">
            <h3>Import tokens</h3>
            <p className="settings-note">Required for Broker token exchange. The server validates both tokens and refreshes once before saving.</p>
            <label>Access Token<input type={props.showTokens ? 'text' : 'password'} value={values.accessToken} onChange={(event) => update({ accessToken: event.target.value })} autoComplete="off" disabled={props.saving || authActive} /></label>
            <label>Refresh Token<input type={props.showTokens ? 'text' : 'password'} value={values.refreshToken} onChange={(event) => update({ refreshToken: event.target.value })} autoComplete="off" disabled={props.saving || authActive} /></label>
            <div className="inline-actions"><button type="button" onClick={props.onImportTokens} disabled={props.saving || authActive || !canImport}>{props.saving ? 'Checking...' : 'Validate and import'}</button><button type="button" className="secondary" onClick={props.onToggleTokenVisibility} disabled={props.saving || authActive}>{props.showTokens ? 'Hide secrets' : 'Show secrets'}</button></div>
          </section>
        </div>
        <div className="inline-actions modal-actions"><button type="button" className="secondary" onClick={props.onClose} disabled={props.saving}>Close</button></div>
      </section>
    </div>
  );
}

function BaiduOpenCredentialsModal(props: { provider: UploadProvider; credentials: BaiduOpenCredentials; showTokens: boolean; saving: boolean; onChange: (value: BaiduOpenCredentials) => void; onToggleTokenVisibility: () => void; onClose: () => void; onSave: () => void }) {
  const values = props.credentials;
  const canSave = Boolean(values.clientID.trim() || values.clientSecret.trim() || values.accessToken.trim() || values.refreshToken.trim() || values.accessTokenExpiresAt.trim());
  const update = (patch: Partial<BaiduOpenCredentials>) => props.onChange({ ...values, ...patch });
  return (
    <div className="modal-backdrop" role="presentation">
      <section className="modal-card upload-cookie-modal" role="dialog" aria-modal="true" aria-busy={props.saving} aria-labelledby="upload-baiduopen-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <div><h2 id="upload-baiduopen-title">百度网盘 Open 凭据</h2><small>{props.provider.name}</small></div>
          <IconCloseButton onClick={props.onClose} disabled={props.saving} />
        </div>
        <div className="upload-auth-grid">
          <section className="upload-auth-panel">
            <h3>开放平台应用</h3>
            <label>Client ID<input autoFocus value={values.clientID} onChange={(event) => update({ clientID: event.target.value })} autoComplete="off" disabled={props.saving} /></label>
            <label>Client Secret<input type={props.showTokens ? 'text' : 'password'} value={values.clientSecret} onChange={(event) => update({ clientSecret: event.target.value })} autoComplete="off" disabled={props.saving} /></label>
            <p className="settings-note">Client ID 和 Client Secret 用于 access token 过期后的自动刷新。已有凭据留空即可保持不变。</p>
          </section>
          <section className="upload-auth-panel upload-auth-import-panel">
            <h3>Token</h3>
            <label>Access Token<input type={props.showTokens ? 'text' : 'password'} value={values.accessToken} onChange={(event) => update({ accessToken: event.target.value })} autoComplete="off" disabled={props.saving} /></label>
            <label>Refresh Token<input type={props.showTokens ? 'text' : 'password'} value={values.refreshToken} onChange={(event) => update({ refreshToken: event.target.value })} autoComplete="off" disabled={props.saving} /></label>
            <label>Access Token 过期时间（可选）<input value={values.accessTokenExpiresAt} onChange={(event) => update({ accessTokenExpiresAt: event.target.value })} placeholder="例如：2027-01-01T00:00:00Z" autoComplete="off" disabled={props.saving} /></label>
            <div className="inline-actions">
              <button type="button" onClick={props.onSave} disabled={props.saving || !canSave}>{props.saving ? '保存中' : '保存凭据'}</button>
              <button type="button" className="secondary" onClick={props.onToggleTokenVisibility} disabled={props.saving}>{props.showTokens ? '隐藏凭据' : '显示凭据'}</button>
            </div>
          </section>
        </div>
        <div className="inline-actions modal-actions"><button type="button" className="secondary" onClick={props.onClose} disabled={props.saving}>关闭</button></div>
      </section>
    </div>
  );
}

function BaiduPCSAuthorizationModal(props: { provider: UploadProvider; credentials: BaiduPCSCredentials; showSecrets: boolean; saving: boolean; onChange: (value: BaiduPCSCredentials) => void; onToggleVisibility: () => void; onClose: () => void; onSave: () => void }) {
  const values = props.credentials;
  const canSave = Boolean(values.cookie.trim() || (props.provider.hasCookie && values.bdstoken.trim()));
  const update = (patch: Partial<BaiduPCSCredentials>) => props.onChange({ ...values, ...patch });
  return (
    <div className="modal-backdrop" role="presentation">
      <section className="modal-card upload-cookie-modal" role="dialog" aria-modal="true" aria-busy={props.saving} aria-labelledby="upload-baidupcs-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header"><div><h2 id="upload-baidupcs-title">Baidu Pan Web credentials</h2><small>{props.provider.name}</small></div><IconCloseButton onClick={props.onClose} disabled={props.saving} /></div>
        <div className="upload-auth-grid">
          <section className="upload-auth-panel">
            <h3>Browser Cookie</h3>
            <p className="settings-note">Paste the Cookie header from a signed-in pan.baidu.com session. It is stored encrypted with the other provider secrets and is never read back into the UI.</p>
            <textarea value={values.cookie} onChange={(event) => update({ cookie: event.target.value })} placeholder="BDUSS=...; STOKEN=..." rows={7} disabled={props.saving} />
          </section>
          <section className="upload-auth-panel upload-auth-import-panel">
            <h3>Optional bdstoken</h3>
            <label>bdstoken<input type={props.showSecrets ? 'text' : 'password'} value={values.bdstoken} onChange={(event) => update({ bdstoken: event.target.value })} autoComplete="off" disabled={props.saving} /></label>
            <p className="settings-note">Leave it empty to let the provider request it through gettemplatevariable.</p>
            <div className="inline-actions">
              <button type="button" onClick={props.onSave} disabled={props.saving || !canSave}>{props.saving ? 'Saving' : 'Save credentials'}</button>
              <button type="button" className="secondary" onClick={props.onToggleVisibility} disabled={props.saving}>{props.showSecrets ? 'Hide secret' : 'Show secret'}</button>
            </div>
          </section>
        </div>
        <div className="inline-actions modal-actions"><button type="button" className="secondary" onClick={props.onClose} disabled={props.saving}>Close</button></div>
      </section>
    </div>
  );
}

function UploadCookieModal(props: { provider: UploadProvider; devices: UploadAuthDevice[]; device: string; cookie: string; auth: CookieAuthStatus | null; saving: boolean; onDeviceChange: (value: string) => void; onCookieChange: (value: string) => void; onClose: () => void; onSave: () => void; onStartAuth: () => void }) {
  const qrURL = props.auth ? `/api/upload/providers/${props.provider.id}/auth/115cookie/${encodeURIComponent(props.auth.sessionId)}/qrcode` : '';
  const terminal = props.auth && ['authorized', 'expired', 'cancelled', 'error'].includes(props.auth.state);
  const authActive = isCookieAuthorizationActive(props.auth);
  const selectedDeviceName = props.devices.find((device) => device.code === props.device)?.name ?? props.device;
  const recordedDeviceName = !props.provider.hasCookie ? '未授权' : !props.provider.authDevice ? '未记录' : (props.devices.find((device) => device.code === props.provider.authDevice)?.name ?? props.provider.authDevice);
  return (
    <div className="modal-backdrop" role="presentation">
      <section className="modal-card upload-cookie-modal" role="dialog" aria-modal="true" aria-busy={props.saving} aria-labelledby="upload-cookie-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header"><div><h2 id="upload-cookie-title">115 Cookie 授权</h2><small>{props.provider.name}</small></div><IconCloseButton onClick={props.onClose} disabled={props.saving} /></div>
        <div className="upload-auth-device-bar">
          <label>授权设备
            <select value={props.device} onChange={(event) => props.onDeviceChange(event.target.value)} disabled={props.saving || authActive}>
              {props.devices.map((device) => <option key={device.code} value={device.code}>{device.name}</option>)}
            </select>
          </label>
          <div className="upload-auth-record"><span>当前授权记录</span><strong>{recordedDeviceName}</strong></div>
        </div>
        <div className="upload-auth-grid">
          <section className="upload-auth-panel">
            <h3>粘贴 Cookie</h3>
            <p className="settings-note">保存设备：{selectedDeviceName}</p>
            <textarea value={props.cookie} onChange={(event) => props.onCookieChange(event.target.value)} placeholder="粘贴 115 Cookie" rows={7} disabled={props.saving || authActive} />
            <button type="button" disabled={props.saving || authActive || !props.cookie.trim()} onClick={props.onSave}>{props.saving ? '保存中' : '保存 Cookie'}</button>
          </section>
          <section className="upload-auth-panel">
            <h3>二维码授权</h3>
            <p className="settings-note">本次设备：{props.auth ? (props.devices.find((device) => device.code === props.auth?.terminal)?.name ?? props.auth.terminal) : selectedDeviceName}</p>
            {props.auth ? <><img className="upload-auth-qr" src={qrURL} alt="115 登录二维码" /><p className="settings-note" aria-live="polite" aria-atomic="true">{props.auth.message || props.auth.state}</p>{terminal ? <button type="button" className="secondary" onClick={props.onStartAuth} disabled={props.saving}>重新获取二维码</button> : <span className="pill running" role="status">授权进行中</span>}</> : <button type="button" onClick={props.onStartAuth} disabled={props.saving}>获取登录二维码</button>}
          </section>
        </div>
        <div className="inline-actions modal-actions"><button type="button" className={authActive ? 'danger' : 'secondary'} onClick={props.onClose} disabled={props.saving}>{authActive ? '结束授权并关闭' : '关闭'}</button></div>
      </section>
    </div>
  );
}

function UploadBatchProgress(props: { batch: UploadBatch }) {
  const transferCount = Math.max(0, Number(props.batch.transferCount) || 0);
  const completedTransfers = Math.min(transferCount, Math.max(0, Number(props.batch.completedTransfers) || 0));
  const failedTransfers = Math.max(0, Number(props.batch.failedTransfers) || 0);
  if (transferCount === 0) {
    const emptyLabel = props.batch.status === 'collecting'
      ? '正在收集文件'
      : props.batch.status === 'completed'
        ? '无需传输'
        : props.batch.status === 'canceled'
          ? '已取消'
          : ['failed', 'partial'].includes(props.batch.status)
            ? '没有可完成的传输'
            : '等待传输信息';
    return (
      <div className="upload-batch-progress empty">
        <span>{emptyLabel}</span>
      </div>
    );
  }
  const collecting = props.batch.status === 'collecting';
  const hasFailures = failedTransfers > 0 || props.batch.failedTargets > 0;
  return (
    <div className={hasFailures ? 'upload-batch-progress has-failures' : 'upload-batch-progress'}>
      <div className="upload-batch-progress-label">
        <span><strong>{completedTransfers}</strong> / {transferCount}</span>
        {failedTransfers > 0 && <span className="upload-batch-progress-failed">失败 {failedTransfers}</span>}
      </div>
      {collecting
        ? <progress max={transferCount} aria-label="批次仍在收集文件，传输总数可能变化" />
        : <progress max={transferCount} value={completedTransfers} aria-label={`已完成 ${completedTransfers} / ${transferCount} 个传输`} />}
    </div>
  );
}

function UploadNotificationRecordsCard(props: {
  records: UploadNotificationRecord[];
  total: number;
  page: number;
  pageSize: number;
  status: UploadNotificationStatusFilter;
  path: string;
  refreshing: boolean;
  timezone: string;
  onRefresh: () => void;
  onStatusChange: (status: UploadNotificationStatusFilter) => void;
  onPathChange: (path: string) => void;
  onApply: () => void;
  onReset: () => void;
  onPageChange: (page: number) => void;
}) {
  const totalPages = Math.max(1, Math.ceil(props.total / props.pageSize));
  return (
    <Card title="通知记录" action={<ActionIconButton label="刷新通知记录" icon={RefreshCw} loading={props.refreshing} disabled={props.refreshing} onClick={props.onRefresh} />}>
      <div className="task-status-tabs" role="group" aria-label="通知状态过滤">
        {uploadNotificationStatusFilters.map((status) => (
          <button className={props.status === status.value ? 'status-tab active' : 'status-tab'} type="button" key={status.value} aria-pressed={props.status === status.value} disabled={props.refreshing} onClick={() => props.onStatusChange(status.value)}>
            {status.label}
          </button>
        ))}
      </div>
      <form className="task-filters upload-filters" onSubmit={(event) => { event.preventDefault(); props.onApply(); }}>
        <label>番剧目录<input value={props.path} onChange={(event) => props.onPathChange(event.target.value)} placeholder="输入番剧目录关键词" /></label>
        <div className="filter-actions list-toolbar-icons"><ActionIconButton label="应用过滤" icon={Filter} type="submit" disabled={props.refreshing} /><ActionIconButton label="重置过滤" icon={RotateCcw} disabled={props.refreshing} onClick={props.onReset} /></div>
      </form>
      <div className="task-table-wrap">
        <table className="task-table upload-notification-record-table">
          <thead><tr><th>ID</th><th>上传批次</th><th>Provider</th><th>模板 / 地址</th><th>状态</th><th>尝试</th><th>HTTP</th><th>时间</th><th>错误</th></tr></thead>
          <tbody>
            {props.records.length ? props.records.map((record) => (
              <tr key={record.id}>
                <td>#{record.id}</td>
                <td><strong>#{record.batchId}</strong><small className="upload-notification-record-path">{record.seriesPath}</small></td>
                <td>{record.providerName || '-'}</td>
                <td><strong>{record.templateName || '-'}</strong><small className="upload-notification-record-url" title={record.url}>{record.url || '-'}</small></td>
                <td><span className={uploadNotificationStatusPillClass(record.status)}>{uploadNotificationStatusLabel(record.status)}</span></td>
                <td>{record.attempts}</td>
                <td>{record.responseStatus || '-'}</td>
                <td>{formatStoredTime(record.deliveredAt || record.updatedAt || record.createdAt, props.timezone)}</td>
                <td className="path-cell upload-error-cell">{record.errorSummary || '-'}</td>
              </tr>
            )) : <tr><td colSpan={9} className="empty-cell">暂无通知记录。</td></tr>}
          </tbody>
        </table>
      </div>
      <div className="pagination-bar">
        <span>共 {props.total} 条，第 {props.page} / {totalPages} 页</span>
        <div className="inline-actions"><ActionIconButton label="上一页" icon={ChevronLeft} disabled={props.refreshing || props.page <= 1} onClick={() => props.onPageChange(props.page - 1)} /><ActionIconButton label="下一页" icon={ChevronRight} disabled={props.refreshing || props.page >= totalPages} onClick={() => props.onPageChange(props.page + 1)} /></div>
      </div>
    </Card>
  );
}

function UploadBatchDetailModal(props: { detail: UploadBatchDetail; timezone: string; actionTargetID: number | null; onClose: () => void; onRetry: (target: UploadBatchTarget) => void; onCancel: (target: UploadBatchTarget) => void }) {
  const [fileStatusFilter, setFileStatusFilter] = useState<UploadFileStatusFilter>('all');
  const [filePage, setFilePage] = useState(1);
  const [countdownNow, setCountdownNow] = useState(() => Date.now());
  const targetsByID = useMemo(() => new Map(props.detail.targets.map((target) => [target.id, target])), [props.detail.targets]);
  const transfersByFileID = useMemo(() => {
    const grouped = new Map<number, UploadTransfer[]>();
    for (const transfer of props.detail.transfers) {
      const transfers = grouped.get(transfer.batchFileId);
      if (transfers) transfers.push(transfer);
      else grouped.set(transfer.batchFileId, [transfer]);
    }
    return grouped;
  }, [props.detail.transfers]);
  const fileRows = useMemo(() => props.detail.files.map((file, index) => {
    const transfers = transfersByFileID.get(file.id) ?? [];
    return {
      file,
      transfers,
      status: aggregateUploadFileStatus(transfers, targetsByID),
      active: transfers.some(uploadTransferIsActive),
      queueOrder: uploadTransferQueueOrder(transfers),
      index
    };
  }).sort((left, right) => {
    if (left.active !== right.active) return left.active ? -1 : 1;
    const statusOrder = uploadFileStatusOrder.indexOf(left.status) - uploadFileStatusOrder.indexOf(right.status);
    if (statusOrder !== 0) return statusOrder;
    const queueOrder = left.queueOrder - right.queueOrder;
    return queueOrder || left.index - right.index;
  }), [props.detail.files, targetsByID, transfersByFileID]);
  const filteredFileRows = useMemo(
    () => fileStatusFilter === 'all' ? fileRows : fileRows.filter((row) => row.status === fileStatusFilter),
    [fileRows, fileStatusFilter]
  );
  const fileStatusCounts = useMemo<Record<UploadFileStatusFilter, number>>(() => {
    const counts: Record<UploadFileStatusFilter, number> = {
      all: fileRows.length,
      running: 0,
      pending: 0,
      failed: 0,
      canceled: 0,
      completed: 0
    };
    for (const row of fileRows) counts[row.status] += 1;
    return counts;
  }, [fileRows]);
  const filePageCount = Math.max(1, Math.ceil(filteredFileRows.length / uploadDetailPageSize));
  const safeFilePage = Math.min(filePage, filePageCount);
  const pagedFileRows = filteredFileRows.slice((safeFilePage - 1) * uploadDetailPageSize, safeFilePage * uploadDetailPageSize);
  const visibleActiveSignature = filteredFileRows.filter((row) => row.active).map((row) => row.file.id).join(',');
  const waitingTransferCount = props.detail.transfers.filter(uploadTransferIsWaiting).length;
  const waitingRetryCount = props.detail.targets.filter((target) => target.status === 'pending' && target.errorSummary).length;
  const notificationSummary = uploadBatchNotificationDetailSummary(props.detail);

  useEffect(() => {
    setFileStatusFilter('all');
    setFilePage(1);
  }, [props.detail.batch.id]);

  useEffect(() => {
    if (filePage > filePageCount) setFilePage(filePageCount);
  }, [filePage, filePageCount]);

  useEffect(() => {
    if (visibleActiveSignature) setFilePage(1);
  }, [visibleActiveSignature]);

  useEffect(() => {
    setCountdownNow(Date.now());
    const interval = window.setInterval(() => setCountdownNow(Date.now()), 1000);
    return () => window.clearInterval(interval);
  }, [props.detail.batch.id]);

  return (
    <div className="modal-backdrop" role="presentation" onClick={props.onClose}>
      <section className="modal-card upload-batch-detail-modal" role="dialog" aria-modal="true" aria-labelledby="upload-batch-detail-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header"><div><h2 id="upload-batch-detail-title">上传批次 #{props.detail.batch.id}</h2><small>{props.detail.batch.seriesPath}</small></div><IconCloseButton onClick={props.onClose} /></div>
        <div className="upload-batch-detail-content">
          <div className="upload-detail-summary">
            <Row label="状态" value={uploadStatusLabel(props.detail.batch.status)} />
            <Row label="可上传时间" value={formatStoredTime(props.detail.batch.readyAt, props.timezone)} />
            <Row label="文件 / 目标" value={`${props.detail.files.length} / ${props.detail.targets.length}`} />
          </div>
          {(waitingTransferCount > 0 || waitingRetryCount > 0) && (
            <p className="upload-detail-refresh-note retrying" role="status">
              <RefreshCw size={14} aria-hidden="true" />
              {waitingTransferCount > 0
                ? `${waitingTransferCount} 个传输正在等待 115 请求间隔；倒计时见文件状态。`
                : `${waitingRetryCount} 个目标正在等待自动重试；会保留上次错误供排查。`}
            </p>
          )}
          <section className="upload-detail-section">
            <h3>目标</h3>
            <div className="task-table-wrap">
              <table className="task-table upload-target-table">
                <thead><tr><th>目标</th><th>状态 / 调度</th><th>尝试</th><th>最近错误</th><th>操作</th></tr></thead>
                <tbody>{props.detail.targets.map((target) => {
                  const scheduleLabel = uploadTargetScheduleLabel(target, props.timezone);
                  return <tr key={target.id}>
                    <td><strong>{target.providerName}</strong><small>{target.remoteRoot}</small></td>
                    <td><div className="upload-target-status"><span className={uploadStatusPillClass(target.status)}>{uploadTargetStatusLabel(target)}</span>{scheduleLabel && <small>{scheduleLabel}</small>}</div></td>
                    <td>{target.attempts}</td>
                    <td className="path-cell upload-error-cell">{target.errorSummary || '-'}</td>
                    <td><div className="table-actions">{['failed', 'canceled'].includes(target.status) && target.retryable && <ActionIconButton label={`重试目标 ${target.providerName}`} icon={RotateCcw} loading={props.actionTargetID === target.id} disabled={props.actionTargetID === target.id} onClick={() => props.onRetry(target)} />}{['failed', 'canceled'].includes(target.status) && !target.retryable && <span className="pill ignored">不可重试</span>}{['waiting', 'pending', 'running'].includes(target.status) && <ActionIconButton label={`取消目标 ${target.providerName}`} icon={X} tone="danger" loading={props.actionTargetID === target.id} disabled={props.actionTargetID === target.id} onClick={() => props.onCancel(target)} />}</div></td>
                  </tr>;
                })}</tbody>
              </table>
            </div>
          </section>
          <section className="upload-detail-section">
            <div className="upload-detail-section-header">
              <h3>通知</h3>
              <small>{notificationSummary.label}</small>
            </div>
            {props.detail.notifications?.length ? <div className="task-table-wrap">
              <table className="task-table upload-notification-detail-table">
                <thead><tr><th>Provider</th><th>模板</th><th>状态</th><th>尝试</th><th>HTTP</th><th>时间</th><th>错误</th></tr></thead>
                <tbody>{props.detail.notifications.map((notification) => (
                  <tr key={notification.id}>
                    <td>{notification.providerName || '-'}</td>
                    <td><strong>{notification.templateName || '-'}</strong><small className="upload-notification-record-url" title={notification.url}>{notification.url || '-'}</small></td>
                    <td><span className={uploadNotificationStatusPillClass(notification.status)}>{uploadNotificationStatusLabel(notification.status)}</span></td>
                    <td>{notification.attempts}</td>
                    <td>{notification.responseStatus || '-'}</td>
                    <td>{formatStoredTime(notification.deliveredAt || notification.updatedAt || notification.createdAt, props.timezone)}</td>
                    <td className="path-cell upload-error-cell">{notification.errorSummary || '-'}</td>
                  </tr>
                ))}</tbody>
              </table>
            </div> : <p className="settings-note">{notificationSummary.emptyMessage}</p>}
          </section>
          <section className="upload-detail-section">
            <div className="upload-detail-section-header">
              <h3>文件</h3>
              <small>显示 {filteredFileRows.length} / {fileRows.length}</small>
            </div>
            <div className="task-status-tabs upload-file-status-tabs" role="group" aria-label="文件传输状态过滤">
              {uploadFileStatusFilters.map((status) => (
                <button
                  className={fileStatusFilter === status.value ? 'status-tab active' : 'status-tab'}
                  type="button"
                  key={status.value}
                  aria-pressed={fileStatusFilter === status.value}
                  onClick={() => {
                    setFileStatusFilter(status.value);
                    setFilePage(1);
                  }}
                >
                  <span>{status.label}</span>
                  <span className="status-tab-count" aria-label={`${fileStatusCounts[status.value]} 个文件`}>
                    {fileStatusCounts[status.value]}
                  </span>
                </button>
              ))}
            </div>
            <div className="task-table-wrap">
              <table className="task-table upload-file-table">
                <thead><tr><th>相对路径</th><th>类型</th><th>大小</th><th>传输状态</th><th>最近错误</th></tr></thead>
                <tbody>{pagedFileRows.length ? pagedFileRows.map(({ file, transfers }) => {
                  const transferErrors = transfers.flatMap((transfer) => {
                    const target = targetsByID.get(transfer.batchTargetId);
                    const status = effectiveUploadTransferStatus(transfer, target);
                    const summary = transfer.errorSummary || (['failed', 'canceled'].includes(status) ? target?.errorSummary : '');
                    return summary ? [{ transfer, target, summary }] : [];
                  });
                  return <tr key={file.id}>
                    <td className="path-cell">{file.relativePath}</td>
                    <td>{file.fileType}</td>
                    <td>{formatUploadBytes(file.size)}</td>
                    <td>{transfers.length ? <div className="upload-transfer-list">{transfers.map((transfer) => {
                      const target = targetsByID.get(transfer.batchTargetId);
                      const display = uploadTransferDisplay(transfer, target);
                      const activity = uploadTransferActivity(transfer, countdownNow);
                      const progress = uploadTransferProgress(transfer);
                      return <div className="upload-transfer-item" key={transfer.id}>
                        <div className="upload-transfer-state">
                          <span className={display.className}>{display.label}</span>
                          {activity && <span className={uploadTransferIsWaiting(transfer) ? 'upload-transfer-activity waiting' : 'upload-transfer-activity'}>{activity}</span>}
                        </div>
                        {progress && <div className="upload-transfer-progress">
                          <progress max={progress.bytesTotal} value={progress.bytesTransferred} aria-label={`已上传 ${progress.percent}%`} />
                          <small>{formatUploadBytes(progress.bytesTransferred)} / {formatUploadBytes(progress.bytesTotal)} · {progress.percent}%</small>
                        </div>}
                      </div>;
                    })}</div> : '-'}</td>
                    <td className="upload-error-cell">{transferErrors.length ? <div className="upload-transfer-errors">{transferErrors.map(({ transfer, target, summary }) => <div className="upload-transfer-error" key={transfer.id}><strong>{target?.providerName || `目标 #${transfer.batchTargetId}`}</strong><span>{summary}</span></div>)}</div> : '-'}</td>
                  </tr>;
                }) : <tr><td colSpan={5} className="empty-cell">{fileStatusFilter === 'all' ? '暂无上传文件。' : '当前状态下没有文件。'}</td></tr>}</tbody>
              </table>
            </div>
            <div className="pagination-bar upload-file-pagination">
              <span aria-live="polite">共 {filteredFileRows.length} 条，第 {safeFilePage} / {filePageCount} 页</span>
              <div className="inline-actions">
                <ActionIconButton label="上一页" icon={ChevronLeft} disabled={safeFilePage <= 1} onClick={() => setFilePage(safeFilePage - 1)} />
                <ActionIconButton label="下一页" icon={ChevronRight} disabled={safeFilePage >= filePageCount} onClick={() => setFilePage(safeFilePage + 1)} />
              </div>
            </div>
          </section>
        </div>
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

function AddEmbyKeyModal(props: { title: string; apiKey: string; saving: boolean; dirty: boolean; onTitleChange: (value: string) => void; onAPIKeyChange: (value: string) => void; onClose: () => void; onSubmit: () => void }) {
  return (
    <div className="modal-backdrop" role="presentation">
      <section className="modal-card add-emby-key-modal" data-protect-draft={props.dirty ? 'true' : undefined} role="dialog" aria-modal="true" aria-labelledby="add-emby-key-title" onClick={(event) => event.stopPropagation()}>
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

function ActionIconButton(props: {
  label: string;
  icon: LucideIcon;
  tone?: 'default' | 'danger';
  type?: 'button' | 'submit';
  disabled?: boolean;
  loading?: boolean;
  onClick?: (event: ReactMouseEvent<HTMLButtonElement>) => void;
}) {
  const Icon = props.icon;
  return (
    <button
      className={`icon-button ${props.tone === 'danger' ? 'danger' : 'secondary'}`}
      type={props.type ?? 'button'}
      title={props.label}
      aria-label={props.label}
      aria-busy={props.loading || undefined}
      disabled={props.disabled}
      onClick={props.onClick}
    >
      {props.loading ? <span className="loading-spinner compact" aria-hidden="true" /> : <Icon size={16} aria-hidden="true" />}
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
            <label className={`theme-option ${props.value === option.value ? 'selected' : ''}`} key={option.value} title={option.label}>
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

function Card(props: { title?: string; header?: React.ReactNode; action?: React.ReactNode; children: React.ReactNode }) {
  return (
    <section className="card">
      <div className="card-header">
        {props.header ?? (props.title ? <h2>{props.title}</h2> : null)}
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
      <section className="modal-card task-detail-modal" role="dialog" aria-modal="true" aria-labelledby="task-detail-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <h2 id="task-detail-title">任务详情</h2>
          <IconCloseButton onClick={props.onClose} />
        </div>
        <Row label="任务" value={`${taskTypeLabel(props.detail.task.type)} #${props.detail.task.id}`} />
        {props.detail.task.mediaPath && <Row label="文件" value={props.detail.task.mediaPath} />}
        <Row label="处理策略" value={props.detail.task.overwriteExisting ? '强制重建' : '只补缺失'} />
        <Row label="状态" value={taskStatusLabel(props.detail.task.status)} />
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
          <IconCloseButton onClick={props.onClose} disabled={props.rescanning} />
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
            <p className="rescan-inherit-note">上传按所属媒体目录的上传配置执行，不受一次性生成设置影响；不属于任何媒体目录的路径不会上传。</p>
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
          <button className="secondary" onClick={props.onClose} disabled={props.rescanning}>取消</button>
          <button onClick={props.onSubmit} disabled={props.rescanning}>{props.rescanning ? '扫描中' : '开始扫描生成'}</button>
        </div>
      </section>
    </div>
  );
}

function WatchDirModal(props: {
  title: string;
  submitLabel: string;
  saving: boolean;
  dirty: boolean;
  path: string;
  watchEnabled: boolean;
  useGlobalProcessing: boolean;
  processing: OutputProcessingConfig;
  uploadConfigs: UploadProviderRoute[];
  providers: UploadProvider[];
  notificationTemplates: UploadNotificationTemplate[];
  onPathChange: (value: string) => void;
  onWatchEnabledChange: (value: boolean) => void;
  onUseGlobalProcessingChange: (value: boolean) => void;
  onProcessingChange: (patch: Partial<OutputProcessingConfig>) => void;
  onUploadConfigsChange: (configs: UploadProviderRoute[]) => void;
  onAddProvider: () => void;
  onAuthorizeProvider: (provider: UploadProvider) => void;
  onBrowseRemoteDirectory: (request: RemoteDirectoryPickerRequest) => void;
  onClose: () => void;
  onBrowsePath: () => void;
  onSubmit: () => void;
}) {
  const uploadConfigsValid = props.uploadConfigs.every((config) =>
    config.providerId != null
    && config.providerId > 0
    && config.remoteRoot.trim()
    && config.includeTypes.length > 0
    && !uploadNotificationConfigError(config, props.notificationTemplates)
  );
  return (
    <div className="modal-backdrop" role="presentation">
      <section className="modal-card watch-dir-modal" data-protect-draft={props.dirty ? 'true' : undefined} role="dialog" aria-modal="true" aria-busy={props.saving} aria-labelledby="watch-dir-modal-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <h2 id="watch-dir-modal-title">{props.title}</h2>
          <IconCloseButton onClick={props.onClose} disabled={props.saving} />
        </div>
        <fieldset className="config-form watch-dir-modal-form" disabled={props.saving}>
          <label>
            媒体目录路径
            <div className="path-input"><input value={props.path} onChange={(event) => props.onPathChange(event.target.value)} placeholder="D:\\Media\\Anime" autoFocus /><button type="button" onClick={props.onBrowsePath}>选择</button></div>
          </label>
          <Toggle label="自动监听" checked={props.watchEnabled} onChange={props.onWatchEnabledChange} />
          <Toggle label="跟随全局生成设置" checked={props.useGlobalProcessing} onChange={props.onUseGlobalProcessingChange} />
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
          <DirectoryUploadConfigsEditor configs={props.uploadConfigs} providers={props.providers} notificationTemplates={props.notificationTemplates} onChange={props.onUploadConfigsChange} onAddProvider={props.onAddProvider} onAuthorizeProvider={props.onAuthorizeProvider} onBrowseRemoteDirectory={props.onBrowseRemoteDirectory} />
          {!uploadConfigsValid && <small className="upload-selection-warning">请补全上传配置，并检查通知模板所需变量。</small>}
        </fieldset>
        <p className="muted">保存后默认递归处理该目录。自动监听会在保存后立即热更新，无需重启服务。</p>
        <div className="inline-actions modal-actions">
          <button className="secondary" onClick={props.onClose} disabled={props.saving}>取消</button>
          <button onClick={props.onSubmit} disabled={props.saving || !props.path.trim() || !uploadConfigsValid}>{props.saving ? '保存中' : props.submitLabel}</button>
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
          <IconCloseButton onClick={props.onClose} disabled={props.applying} />
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
          <button className="secondary" onClick={props.onClose} disabled={props.applying}>取消</button>
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
            <ActionIconButton label="刷新 TMDB 详情" icon={RefreshCw} loading={props.refreshing} disabled={props.refreshing} onClick={props.onRefresh} />
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
            <ActionIconButton label="刷新重命名历史" icon={RefreshCw} loading={props.loading} disabled={props.loading} onClick={props.onRefresh} />
            <IconCloseButton onClick={props.onClose} />
          </div>
        </div>
        <div className="rename-history-list">
          {props.history.length ? props.history.map((batch) => (
            <div className="history-item" key={batch.id}>
              <div className="history-summary">
                <ActionIconButton label={`查看重命名历史 ${batch.id} 详情`} icon={Eye} onClick={() => props.onOpenDetails(batch)} />
                <div>
                  <strong>{formatStoredTime(batch.createdAt, props.timezone)}</strong>
                  <small>{batch.items.length} 项 · {batch.id}{batch.undone ? ` · 已撤销 ${batch.undoneAt ? formatStoredTime(batch.undoneAt, props.timezone) : ''}` : ''}</small>
                </div>
                <div className="inline-actions">
                  <ActionIconButton label={batch.undone ? `历史 ${batch.id} 已撤销` : `撤销重命名历史 ${batch.id}`} icon={Undo2} loading={props.undoingId === batch.id} onClick={() => props.onUndo(batch.id)} disabled={batch.undone || props.undoingId === batch.id} />
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
          <strong>{renameStatusLabel(item.status)}</strong>
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
    <div className="modal-backdrop" role="presentation">
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

function TargetPathEditorModal(props: { value: string; dirty: boolean; saving: boolean; onChange: (value: string) => void; onClose: () => void; onSubmit: () => void }) {
  return (
    <div className="modal-backdrop" role="presentation">
      <section className="modal-card target-path-modal" data-protect-draft={props.dirty ? 'true' : undefined} role="dialog" aria-modal="true" aria-busy={props.saving} aria-labelledby="target-path-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <h2 id="target-path-title">编辑目标路径</h2>
          <IconCloseButton onClick={props.onClose} disabled={props.saving} />
        </div>
        <textarea value={props.value} onChange={(event) => props.onChange(event.target.value)} autoFocus disabled={props.saving} />
        <p className="muted">可以填写文件名、相对路径或完整路径。执行前仍会检查目标冲突。</p>
        <div className="inline-actions modal-actions">
          <button className="secondary" onClick={props.onClose} disabled={props.saving}>取消</button>
          <button onClick={props.onSubmit} disabled={props.saving || !props.value.trim()}>{props.saving ? '校验中' : '校验并应用'}</button>
        </div>
      </section>
    </div>
  );
}

function RemoteDirectoryPicker(props: { provider: UploadProvider; initialPath: string; cache: Map<string, RemoteDirectoryList>; requests: Map<string, Promise<RemoteDirectoryList>>; onSelect: (path: string) => void; onClose: () => void }) {
  const initialPath = normalizeRemoteDirectoryPath(props.initialPath);
  const [currentPath, setCurrentPath] = useState(initialPath);
  const [data, setData] = useState<RemoteDirectoryList | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const requestIDRef = useRef(0);

  useEffect(() => {
    void load(initialPath);
    return () => { requestIDRef.current++; };
  }, []);

  async function load(value: string, refresh = false) {
    const path = normalizeRemoteDirectoryPath(value);
    const requestID = ++requestIDRef.current;
    const cacheKey = remoteDirectoryCacheKey(props.provider.id, path);
    setCurrentPath(path);
    setData(null);
    setError('');

    const cached = refresh ? null : props.cache.get(cacheKey);
    if (cached) {
      setCurrentPath(cached.path);
      setData(cached);
      setLoading(false);
      return;
    }

    setLoading(true);
    try {
      const result = await requestRemoteDirectory(path, refresh);
      if (requestID !== requestIDRef.current) return;
      setCurrentPath(result.path);
      setData(result);
    } catch (err) {
      if (requestID === requestIDRef.current) {
        const detail = err instanceof Error ? err.message : '读取失败';
        setError(`远端目录“${path}”不存在或无法访问：${detail}`);
      }
    } finally {
      if (requestID === requestIDRef.current) setLoading(false);
    }
  }

  async function requestRemoteDirectory(path: string, refresh: boolean): Promise<RemoteDirectoryList> {
    const cacheKey = remoteDirectoryCacheKey(props.provider.id, path);
    const pending = refresh ? null : props.requests.get(cacheKey);
    if (pending) return pending;

    const request = (async () => {
      const params = new URLSearchParams({ path });
      const response = await fetch(`/api/upload/providers/${props.provider.id}/directories?${params.toString()}`);
      if (!response.ok) throw new Error(await readErrorMessage(response));
      const result = await response.json() as Partial<RemoteDirectoryList>;
      const canonicalPath = normalizeRemoteDirectoryPath(result.path || path);
      const entries = asArray<RemoteDirectoryEntry>(result.entries)
        .filter((entry) => entry.isDir && Boolean(entry.path))
        .map((entry) => ({ ...entry, path: normalizeRemoteDirectoryPath(entry.path) }))
        .sort((left, right) => left.name.localeCompare(right.name, 'zh-CN', { numeric: true, sensitivity: 'base' }));
      return { path: canonicalPath, entries };
    })();
    props.requests.set(cacheKey, request);
    try {
      const result = await request;
      if (props.requests.get(cacheKey) === request) {
        props.cache.set(cacheKey, result);
        props.cache.set(remoteDirectoryCacheKey(props.provider.id, result.path), result);
      }
      return result;
    } catch (err) {
      if (refresh && props.requests.get(cacheKey) === request) props.cache.delete(cacheKey);
      throw err;
    } finally {
      if (props.requests.get(cacheKey) === request) props.requests.delete(cacheKey);
    }
  }

  const parentPath = remoteDirectoryParent(currentPath);
  return (
    <div className="modal-backdrop" role="presentation" onClick={props.onClose}>
      <section className="modal-card remote-directory-picker-modal" role="dialog" aria-modal="true" aria-busy={loading} aria-labelledby="remote-directory-picker-title" onClick={(event) => event.stopPropagation()}>
        <div className="card-header">
          <div><h2 id="remote-directory-picker-title">选择远端根目录</h2><small>{props.provider.name}</small></div>
          <IconCloseButton onClick={props.onClose} />
        </div>
        <div className="remote-directory-picker-content">
          <div className="remote-directory-toolbar">
            <div className="remote-directory-location"><span>当前目录</span><code title={currentPath}>{currentPath}</code></div>
            <div className="inline-actions">
              <button className="secondary" type="button" disabled={loading || currentPath === '/'} onClick={() => void load('/')}>根目录</button>
              <button className="secondary" type="button" disabled={loading || !parentPath} onClick={() => void load(parentPath)}>上一级</button>
              <button className="secondary icon-text-button" type="button" disabled={loading} onClick={() => void load(currentPath, true)}><RefreshCw size={15} />刷新</button>
            </div>
          </div>
          {error && <section className="error-card directory-error">{error}</section>}
          <div className="directory-list remote-directory-list" aria-live="polite">
            {loading && <div className="remote-directory-loading"><span className="loading-spinner" aria-hidden="true" />正在读取远端目录…</div>}
            {!loading && data?.entries.map((entry) => <button className="directory-item remote-directory-item" type="button" key={entry.id || entry.path} onClick={() => void load(entry.path)}><FolderOpen size={17} aria-hidden="true" /><span>{entry.name}</span></button>)}
            {!loading && data && !data.entries.length && <p className="muted">当前目录中没有子目录。</p>}
            {!loading && !data && !error && <p className="muted">没有可显示的目录。</p>}
          </div>
        </div>
        <div className="inline-actions modal-actions">
          <button className="secondary" type="button" onClick={props.onClose}>取消</button>
          <button type="button" onClick={() => data && props.onSelect(data.path)} disabled={loading || !data}>选择当前目录</button>
        </div>
      </section>
    </div>
  );
}

function normalizeRemoteDirectoryPath(value: string): string {
  const segments: string[] = [];
  for (const segment of value.trim().replace(/\\/g, '/').split('/')) {
    if (!segment || segment === '.') continue;
    if (segment === '..') segments.pop();
    else segments.push(segment);
  }
  return `/${segments.join('/')}`;
}

function remoteDirectoryParent(value: string): string {
  const path = normalizeRemoteDirectoryPath(value);
  if (path === '/') return '';
  const segments = path.split('/').filter(Boolean);
  segments.pop();
  return segments.length ? `/${segments.join('/')}` : '/';
}

function remoteDirectoryCacheKey(providerID: number, path: string): string {
  return `${providerID}:${normalizeRemoteDirectoryPath(path)}`;
}

function DirectoryPicker(props: { title: string; initialPath: string; rootPath?: string; onSelect: (path: string) => void; onClose: () => void }) {
  const [currentPath, setCurrentPath] = useState(props.initialPath);
  const [data, setData] = useState<DirectoryList>({ path: '', parent: '', entries: [] });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const requestIDRef = useRef(0);

  useEffect(() => {
    void load(currentPath);
  }, []);

  async function load(path: string) {
    const requestID = ++requestIDRef.current;
    setLoading(true);
    setError('');
    try {
      const params = new URLSearchParams();
      if (path.trim()) params.set('path', path.trim());
      if (props.rootPath?.trim()) params.set('root', props.rootPath.trim());
      const response = await fetch(`/api/fs/directories?${params.toString()}`);
      if (!response.ok) {
        if (requestID === requestIDRef.current) setError(await readErrorMessage(response));
        return;
      }
      const result = await response.json();
      if (requestID !== requestIDRef.current) return;
      setData({ ...result, entries: asArray<DirectoryEntry>(result.entries) });
      setCurrentPath(result.path || path);
    } catch (err) {
      if (requestID === requestIDRef.current) setError(err instanceof Error ? err.message : '读取目录失败');
    } finally {
      if (requestID === requestIDRef.current) setLoading(false);
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
          {data.parent && <button className="directory-item" disabled={loading} onClick={() => load(data.parent)}>..</button>}
          {data.entries.map((entry) => <button className="directory-item" disabled={loading} key={entry.path} onClick={() => load(entry.path)}>{entry.name}</button>)}
          {!data.entries.length && !data.parent && <p className="muted">没有可显示的目录。</p>}
        </div>
        <div className="inline-actions modal-actions">
          <button className="secondary" onClick={props.onClose}>取消</button>
          <button onClick={() => props.onSelect(currentPath)} disabled={loading || !currentPath.trim()}>选择当前目录</button>
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

function Toggle(props: { label: ReactNode; checked: boolean; disabled?: boolean; onChange: (value: boolean) => void }) {
  return (
    <label className={`toggle-row ${props.disabled ? 'disabled' : ''}`}>
      <span>{props.label}</span>
      <input type="checkbox" checked={props.checked} disabled={props.disabled} onChange={(event) => props.onChange(event.target.checked)} />
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
  const dropdownID = `language-fallback-${useId()}`;
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
      <button className="language-dropdown-trigger" type="button" aria-expanded={open} aria-controls={dropdownID} onClick={() => setOpen((value) => !value)}>
        {open ? '收起语言列表' : '选择备用语言'}
      </button>
      {open && (
        <div id={dropdownID} className="language-dropdown">
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

async function requestJSON<T>(url: string, label: string): Promise<T> {
  const response = await fetch(url);
  return readJSONResponse<T>(response, label);
}

async function readJSONResponse<T>(response: Response, label: string): Promise<T> {
  const text = await response.text();
  if (!response.ok) {
    let detail = response.statusText || '请求失败';
    if (text) {
      try {
        detail = (JSON.parse(text) as { error?: string }).error || text;
      } catch {
        detail = text;
      }
    }
    throw new Error(`${label}失败：${detail}`);
  }
  if (!text.trim()) throw new Error(`${label}返回了空响应`);
  try {
    return JSON.parse(text) as T;
  } catch {
    throw new Error(`${label}返回的数据格式无效`);
  }
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

function taskRunFilterLabel(run: TaskRun, timezone: string): string {
  const source = run.source === 'manual' ? '手动扫描' : run.source === 'watcher' ? '目录监控' : run.source === 'scan' ? '启动扫描' : '历史任务';
  const status = run.status === 'collecting' ? '扫描中'
    : run.status === 'running' ? '处理中'
      : run.status === 'completed' ? '已完成'
        : run.status === 'failed' ? '失败'
          : run.status === 'canceled' ? '已取消'
            : run.status === 'empty' ? '无任务'
              : run.status;
  const parts = run.scopePath.split(/[\\/]/).filter(Boolean);
  const count = run.total > 0 ? `${run.total} 个任务` : '无任务';
  const details = [formatStoredTime(run.createdAt, timezone), source];
  if (parts.length || run.scopePath) details.push(parts.slice(-2).join(' / ') || run.scopePath);
  details.push(`${count}（${status}）`);
  return details.join(' · ');
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
