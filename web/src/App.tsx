import { useEffect, useState } from 'react';

type Health = {
  status: string;
  time: string;
};

type AppConfig = {
  server: { addr: string };
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
    enableThumbFallback: boolean;
  };
  scraping: {
    enableTmdb: boolean;
    enablePeople: boolean;
    tmdbApiKey: string;
    tmdbToken: string;
    tmdbBaseUrl: string;
    language: string;
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
  type: string;
  status: string;
  attempts: number;
  errorSummary: string;
  createdAt: string;
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

export function App() {
  const [health, setHealth] = useState<Health | null>(null);
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [tools, setTools] = useState<ToolStatus[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [watchDirs, setWatchDirs] = useState<WatchDir[]>([]);
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [newWatchDir, setNewWatchDir] = useState('');
  const [selectedTask, setSelectedTask] = useState<TaskDetail | null>(null);
  const [checkingTools, setCheckingTools] = useState(false);
  const [savingConfig, setSavingConfig] = useState(false);
  const [notice, setNotice] = useState('');
  const [rescanning, setRescanning] = useState(false);
  const [error, setError] = useState<string>('');

  useEffect(() => {
    async function load() {
      try {
        const [healthResponse, configResponse, toolsResponse, tasksResponse, dirsResponse, artifactsResponse] = await Promise.all([
          fetch('/api/health'),
          fetch('/api/config'),
          fetch('/api/tools/status'),
          fetch('/api/tasks?limit=10'),
          fetch('/api/watch-dirs'),
          fetch('/api/artifacts?limit=10')
        ]);
        setHealth(await healthResponse.json());
        setConfig(await configResponse.json());
        setTools(asArray<ToolStatus>(await toolsResponse.json()));
        setTasks(asArray<Task>(await tasksResponse.json()));
        setWatchDirs(asArray<WatchDir>(await dirsResponse.json()));
        setArtifacts(asArray<Artifact>(await artifactsResponse.json()));
      } catch (err) {
        setError(err instanceof Error ? err.message : '加载失败');
      }
    }

    void load();
  }, []);

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
    try {
      const response = await fetch('/api/watch-dirs', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: newWatchDir.trim(), recursive: true, enabled: true })
      });
      if (!response.ok) throw new Error(await response.text());
      const created = await response.json();
      setWatchDirs((items) => [...items, created]);
      setNewWatchDir('');
    } catch (err) {
      setError(err instanceof Error ? err.message : '添加目录失败');
    }
  }

  async function deleteWatchDir(id: number) {
    setError('');
    try {
      const response = await fetch(`/api/watch-dirs/${id}`, { method: 'DELETE' });
      if (!response.ok) throw new Error(await response.text());
      setWatchDirs((items) => items.filter((item) => item.id !== id));
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除目录失败');
    }
  }

  async function rescan(watchDirId?: number) {
    setRescanning(true);
    setError('');
    try {
      const response = await fetch('/api/tasks/rescan', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ watchDirId: watchDirId ?? 0 })
      });
      if (!response.ok) throw new Error(await response.text());
      const tasksResponse = await fetch('/api/tasks?limit=10');
      setTasks(asArray<Task>(await tasksResponse.json()));
    } catch (err) {
      setError(err instanceof Error ? err.message : '补扫失败');
    } finally {
      setRescanning(false);
    }
  }

  async function loadTaskDetail(id: number) {
    setError('');
    try {
      const response = await fetch(`/api/tasks/${id}`);
      if (!response.ok) throw new Error(await response.text());
      const detail = await response.json();
      setSelectedTask({
        ...detail,
        logs: asArray<TaskLog>(detail.logs),
        artifacts: asArray<Artifact>(detail.artifacts)
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载任务详情失败');
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
      if (!response.ok) throw new Error(await response.text());
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
      <section className="hero">
        <div>
          <p className="eyebrow">NyaMediaMetadataTool</p>
          <h1>本地媒体伴生文件生成器</h1>
          <p className="summary">替代神医助手的本地生成职责：NFO、MediaInfo、BIF、字幕提取与 TMDB 增强。</p>
        </div>
        <div className="status-card">
          <span className="label">服务状态</span>
          <strong>{health?.status ?? 'loading'}</strong>
          <small>{health?.time ?? ''}</small>
        </div>
      </section>

      {error && <section className="error-card">{error}</section>}
      {notice && <section className="notice-card">{notice}</section>}

      <section className="grid">
        <Card title="当前配置">
          <Row label="监听地址" value={config?.server.addr ?? '-'} />
          <Row label="数据库" value={config?.database.path ?? '-'} />
          <Row label="并发数" value={String(config?.processing.concurrency ?? '-')} />
          <Row label="扩展名" value={config?.processing.extensions?.join(', ') ?? '-'} />
          <Row label="TMDB 地址" value={config?.scraping.tmdbBaseUrl ?? '-'} />
        </Card>

        <Card title="任务开关">
          <Flag label="字幕提取" enabled={config?.processing.enableSubtitles} />
          <Flag label="MediaInfo" enabled={config?.processing.enableMediaInfo} />
          <Flag label="NFO" enabled={config?.processing.enableNfo} />
          <Flag label="BIF" enabled={config?.processing.enableBif} />
          <Flag label="接管图片" enabled={config?.processing.enableImageTakeover} />
          <Flag label="单集图兜底" enabled={config?.processing.enableThumbFallback} />
        </Card>

        <Card title="配置编辑" action={<button onClick={saveConfig} disabled={savingConfig || !config}>{savingConfig ? '保存中' : '保存配置'}</button>}>
          {config ? (
            <div className="config-form">
              <label>ffmpeg<input value={config.tools.ffmpeg} onChange={(event) => updateConfig((draft) => { draft.tools.ffmpeg = event.target.value; })} /></label>
              <label>ffprobe<input value={config.tools.ffprobe} onChange={(event) => updateConfig((draft) => { draft.tools.ffprobe = event.target.value; })} /></label>
              <label>mkvextract<input value={config.tools.mkvextract} onChange={(event) => updateConfig((draft) => { draft.tools.mkvextract = event.target.value; })} /></label>
              <label>mediainfo<input value={config.tools.mediainfo} onChange={(event) => updateConfig((draft) => { draft.tools.mediainfo = event.target.value; })} /></label>
              <label>并发数<input type="number" value={config.processing.concurrency} onChange={(event) => updateConfig((draft) => { draft.processing.concurrency = Number(event.target.value); })} /></label>
              <label>BIF 宽度<input type="number" value={config.processing.bifWidth} onChange={(event) => updateConfig((draft) => { draft.processing.bifWidth = Number(event.target.value); })} /></label>
              <label>BIF 间隔秒<input type="number" value={config.processing.bifInterval} onChange={(event) => updateConfig((draft) => { draft.processing.bifInterval = Number(event.target.value); })} /></label>
              <Toggle label="TMDB 刮削" checked={config.scraping.enableTmdb} onChange={(value) => updateConfig((draft) => { draft.scraping.enableTmdb = value; })} />
              <Toggle label="刮削演员/职员" checked={config.scraping.enablePeople} onChange={(value) => updateConfig((draft) => { draft.scraping.enablePeople = value; })} />
              <label>TMDB Token<input type="password" value={config.scraping.tmdbToken} onChange={(event) => updateConfig((draft) => { draft.scraping.tmdbToken = event.target.value; })} placeholder="Bearer token" /></label>
              <label>TMDB API Key<input value={config.scraping.tmdbApiKey} onChange={(event) => updateConfig((draft) => { draft.scraping.tmdbApiKey = event.target.value; })} placeholder="可选，优先使用 Token" /></label>
              <label>TMDB 地址<input value={config.scraping.tmdbBaseUrl} onChange={(event) => updateConfig((draft) => { draft.scraping.tmdbBaseUrl = event.target.value; })} placeholder="https://api.themoviedb.org/3" /></label>
              <label>TMDB 代理<input value={config.scraping.proxy} onChange={(event) => updateConfig((draft) => { draft.scraping.proxy = event.target.value; })} placeholder="http://127.0.0.1:7890" /></label>
              <label>刮削语言<input value={config.scraping.language} onChange={(event) => updateConfig((draft) => { draft.scraping.language = event.target.value; })} /></label>
              <label>刮削地区<input value={config.scraping.region} onChange={(event) => updateConfig((draft) => { draft.scraping.region = event.target.value; })} /></label>
              <Toggle label="覆盖已有文件" checked={config.processing.overwriteExisting} onChange={(value) => updateConfig((draft) => { draft.processing.overwriteExisting = value; })} />
              <Toggle label="字幕提取" checked={config.processing.enableSubtitles} onChange={(value) => updateConfig((draft) => { draft.processing.enableSubtitles = value; })} />
              <Toggle label="MediaInfo" checked={config.processing.enableMediaInfo} onChange={(value) => updateConfig((draft) => { draft.processing.enableMediaInfo = value; })} />
              <Toggle label="NFO" checked={config.processing.enableNfo} onChange={(value) => updateConfig((draft) => { draft.processing.enableNfo = value; })} />
              <Toggle label="BIF" checked={config.processing.enableBif} onChange={(value) => updateConfig((draft) => { draft.processing.enableBif = value; })} />
            </div>
          ) : (
            <p className="muted">配置加载中。</p>
          )}
        </Card>

        <Card title="监听目录" action={<button onClick={() => rescan()} disabled={rescanning}>{rescanning ? '补扫中' : '全部补扫'}</button>}>
          <div className="form-row">
            <input value={newWatchDir} onChange={(event) => setNewWatchDir(event.target.value)} placeholder="D:\\Media\\Anime" />
            <button onClick={addWatchDir}>添加</button>
          </div>
          {watchDirs.length ? (
            watchDirs.map((dir) => (
              <div className="dir-item" key={dir.id}>
                <div>
                  <strong>{dir.path}</strong>
                  <small>{dir.enabled ? '启用' : '停用'} · {dir.recursive ? '递归' : '当前层'}</small>
                </div>
                <div className="inline-actions">
                  <button onClick={() => rescan(dir.id)} disabled={rescanning}>补扫</button>
                  <button className="danger" onClick={() => deleteWatchDir(dir.id)}>删除</button>
                </div>
              </div>
            ))
          ) : (
            <p className="muted">尚未配置监听目录。</p>
          )}
        </Card>

        <Card title="工具状态" action={<button onClick={checkTools} disabled={checkingTools}>{checkingTools ? '检测中' : '一键检测'}</button>}>
          {tools.length ? (
            tools.map((tool) => (
              <div className="tool" key={tool.name}>
                <div>
                  <strong>{tool.name}</strong>
                  <small>{tool.version || tool.error || '未检测'}</small>
                </div>
                <span className={tool.available ? 'pill ok' : 'pill bad'}>{tool.available ? '可用' : '不可用'}</span>
              </div>
            ))
          ) : (
            <p className="muted">尚未检测工具状态。</p>
          )}
        </Card>

        <Card title="最近任务">
          {tasks.length ? (
            tasks.map((task) => (
              <button className="task-row" key={task.id} onClick={() => loadTaskDetail(task.id)}>
                <span className={`pill ${task.status === 'completed' ? 'ok' : task.status === 'failed' ? 'bad' : ''}`}>{task.status}</span>
                <strong>{task.type} #{task.id}</strong>
                <small>{task.errorSummary || task.createdAt}</small>
              </button>
            ))
          ) : (
            <p className="muted">暂无任务。</p>
          )}
        </Card>

        <Card title="最近产物">
          {artifacts.length ? (
            artifacts.map((artifact) => <Row key={artifact.id} label={artifact.type} value={artifact.path} />)
          ) : (
            <p className="muted">暂无产物。</p>
          )}
        </Card>

        <Card title="任务详情">
          {selectedTask ? (
            <div>
              <Row label="任务" value={`${selectedTask.task.type} #${selectedTask.task.id}`} />
              <Row label="状态" value={selectedTask.task.status} />
              <Row label="尝试次数" value={String(selectedTask.task.attempts)} />
              {selectedTask.task.errorSummary && <Row label="错误" value={selectedTask.task.errorSummary} />}
              <h3>日志</h3>
              {asArray<TaskLog>(selectedTask.logs).length ? asArray<TaskLog>(selectedTask.logs).map((log) => (
                <div className="log-line" key={log.id}>
                  <span className={`pill ${log.level === 'error' ? 'bad' : 'ok'}`}>{log.level}</span>
                  <div>
                    <strong>{log.message}</strong>
                    <small>{log.detail || log.createdAt}</small>
                  </div>
                </div>
              )) : <p className="muted">暂无日志。</p>}
              <h3>产物</h3>
              {asArray<Artifact>(selectedTask.artifacts).length ? asArray<Artifact>(selectedTask.artifacts).map((artifact) => (
                <Row key={artifact.id} label={artifact.type} value={artifact.path} />
              )) : <p className="muted">暂无产物。</p>}
            </div>
          ) : (
            <p className="muted">点击最近任务查看详情。</p>
          )}
        </Card>
      </section>
    </main>
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

function asArray<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : [];
}
