import { useEffect, useState } from 'react';

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

type RescanScope = 'all' | 'dir' | 'path';
type RescanStrategy = 'normal' | 'missing' | 'force';

type LanguageOption = { code: string; name: string };
type RegionOption = { code: string; name: string };
type PageKey = 'dashboard' | 'settings' | 'watchDirs' | 'tasks';

const pagePaths: Record<PageKey, string> = {
  dashboard: '/',
  settings: '/settings',
  watchDirs: '/watch-dirs',
  tasks: '/tasks'
};

function pageFromPath(pathname: string): PageKey {
  switch (pathname) {
    case '/settings':
      return 'settings';
    case '/watch-dirs':
      return 'watchDirs';
    case '/tasks':
      return 'tasks';
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
  const [newWatchDir, setNewWatchDir] = useState('');
  const [rescanOpen, setRescanOpen] = useState(false);
  const [rescanScope, setRescanScope] = useState<RescanScope>('all');
  const [rescanTarget, setRescanTarget] = useState('');
  const [rescanStrategy, setRescanStrategy] = useState<RescanStrategy>('normal');
  const [selectedTask, setSelectedTask] = useState<TaskDetail | null>(null);
  const [checkingTools, setCheckingTools] = useState(false);
  const [savingConfig, setSavingConfig] = useState(false);
  const [notice, setNotice] = useState('');
  const [rescanning, setRescanning] = useState(false);
  const [error, setError] = useState<string>('');
  const [activePage, setActivePage] = useState<PageKey>(() => pageFromPath(window.location.pathname));
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
      } catch (err) {
        setError(err instanceof Error ? err.message : '加载失败');
      }
    }

    void load();
  }, [taskPageSize]);

  useEffect(() => {
    function handlePopState() {
      setActivePage(pageFromPath(window.location.pathname));
    }

    window.addEventListener('popstate', handlePopState);
    return () => window.removeEventListener('popstate', handlePopState);
  }, []);

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

  async function addWatchDir() {
    if (!newWatchDir.trim()) return;
    setError('');
    const response = await fetch('/api/watch-dirs', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: newWatchDir.trim(), recursive: true, enabled: true })
    });
    if (!response.ok) {
      setError(await response.text());
      return;
    }
    const created = await response.json();
    setWatchDirs((items) => [...items, created]);
    setNewWatchDir('');
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
          setError('请选择监听目录');
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
    setRescanStrategy('normal');
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
          <TabButton active={activePage === 'watchDirs'} label="监听目录" onClick={() => navigate('watchDirs')} />
          <TabButton active={activePage === 'tasks'} label="任务" onClick={() => navigate('tasks')} />
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
          <Card title="监听目录" action={<button onClick={() => openRescanDialog('all')} disabled={rescanning}>{rescanning ? '补扫中' : '补扫'}</button>}>
            <div className="form-row">
              <input value={newWatchDir} onChange={(event) => setNewWatchDir(event.target.value)} placeholder="D:\\Media\\Anime" />
              <button onClick={addWatchDir}>添加</button>
            </div>
            {watchDirs.length ? watchDirs.map((dir) => (
              <div className="dir-item" key={dir.id}>
                <div>
                  <strong>{dir.path}</strong>
                  <small>{dir.enabled ? '启用' : '停用'} · {dir.recursive ? '递归' : '当前层'}</small>
                </div>
                <div className="inline-actions">
                  <button onClick={() => openRescanDialog('dir', dir.path)} disabled={rescanning}>补扫</button>
                  <button className="danger" onClick={() => deleteWatchDir(dir.id)}>删除</button>
                </div>
              </div>
            )) : <p className="muted">尚未配置监听目录。</p>}
          </Card>
        </section>
      )}

        {activePage === 'tasks' && (
        <section className="page-grid task-page-grid">
          <Card title="任务列表">
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
                      <td><span className={`pill ${task.status === 'completed' ? 'ok' : task.status === 'failed' ? 'bad' : ''}`}>{task.status}</span></td>
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
      {rescanOpen && <RescanModal scope={rescanScope} target={rescanTarget} strategy={rescanStrategy} directories={watchDirs} rescanning={rescanning} onClose={() => setRescanOpen(false)} onScopeChange={setRescanScope} onTargetChange={setRescanTarget} onStrategyChange={setRescanStrategy} onSubmit={() => void rescan()} />}
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
              <option value="all">全部监听目录</option>
              <option value="dir">指定监听目录</option>
              <option value="path">指定路径</option>
            </select>
          </label>
          {props.scope === 'dir' && (
            <label>
              监听目录
              <select value={props.target} onChange={(event) => props.onTargetChange(event.target.value)}>
                <option value="">请选择</option>
                {props.directories.map((dir) => <option key={dir.id} value={dir.path}>{dir.path}</option>)}
              </select>
            </label>
          )}
          {props.scope === 'path' && (
            <label>
              路径
              <input value={props.target} onChange={(event) => props.onTargetChange(event.target.value)} placeholder="D:\\Media\\Anime\\S01" />
            </label>
          )}
          <label>
            策略
            <select value={props.strategy} onChange={(event) => props.onStrategyChange(event.target.value as RescanStrategy)}>
              <option value="normal">正常补扫</option>
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
