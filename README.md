# NyaMediaMetadataTool

面向本地媒体库的元数据与伴生文件生成工具。当前主入口是基于 Wails 的跨平台桌面客户端，用于监控媒体目录、生成 Emby 友好的本地伴生文件、查看任务日志，并提供剧集重命名、上传与核对工具。桌面端将 React 工作台和 Go 服务运行在同一进程内，生产模式不会额外监听本机 HTTP 端口。

项目仍保留命令行服务与浏览器管理方式，便于无桌面环境的主机、远程部署和自动化场景复用同一套核心能力。它不是下载器，也不是 Emby 插件；更适合作为下载落盘后的媒体整理与产物生成工作站，与 AniRss、qBittorrent、Emby 等现有链路并行工作。

## 当前能力

- 媒体目录管理：支持多个目录、递归扫描、实时监控、手动重扫、目录级处理策略覆盖。
- 任务队列：SQLite 记录任务、日志、产物和工具状态；支持并发处理、失败重试、取消运行中任务、重新排队和忽略失败任务。
- 伴生文件生成：支持字幕抽取、`mediainfo.json`、BIF 预览索引、单集 NFO、剧集/季度 NFO、单集缩略图。
- 网盘发布：元数据完成后按番剧变更窗口合并上传批次；一个目标可设置默认路由并为指定媒体目录覆盖远端目录、碰撞策略和文件类型，批次按目标独立重试、校验、记录文件清单，并在目标完成后写入可租约消费的 outbox 事件。首个 Provider 为 `115cookie`，`115open`、123 云盘和百度网盘已保留运行时注册与凭据契约。
- 元数据增强：支持 TMDB 查询、缓存、语言/地区配置、备用语言、代理，以及可选 fanart.tv 图片来源。
- 图片接管：默认关闭；开启后可生成 `poster.jpg`、`fanart.jpg`、`clearlogo.png`、`clearart.png` 和季度海报。
- 桌面工作台：提供仪表盘、首次运行检查、设置、媒体目录、任务、上传、重命名、剧集核对等页面，并集成原生路径选择、文件定位、系统通知和退出保护。
- 批量重命名：支持预览、手动修正、模板占位符、TMDB 匹配、附属文件随动重命名、历史回滚。
- 剧集核对：支持本地缺集/伴生文件检查、Emby API 对比、本地与远端 SFTP 文件对齐检查。
- 辅助工具：保留 `bifunpack` BIF 解包命令，用于调试 BIF 生成结果。

## 项目结构

```text
main.go          Wails 桌面入口
desktop_app.go   原生桌面能力桥接
build/           三平台应用图标与打包元数据
.github/workflows/desktop-build.yml
                 Windows、macOS、Linux 原生构建 CI
cmd/
  nyammd/        兼容的 CLI/Web 服务入口
  bifunpack/     BIF 图片解包 CLI
internal/
  api/           HTTP API 与静态前端入口
  appcore/       桌面与 CLI 共用的后台生命周期
  appdata/       桌面数据目录与初始化
  bootstrap/     目录扫描与任务入队
  config/        YAML 配置
  episodeparse/  文件名季集解析
  fileaudit/     本地/远端文件对齐检查
  metadataaudit/ 剧集缺漏与 Emby 对比
  pipeline/      字幕、mediainfo、BIF、NFO、图片生成
  renamer/       重命名预览、执行与历史
  runner/        任务执行器
  store/         SQLite 存储
  tmdb/          TMDB 客户端与缓存
  upload/        多 Provider 上传批次、115 Cookie 授权与上传 Worker
  watcher/       fsnotify 目录监控
web/
  src/           React + TypeScript 前端
docs/
  example/       伴生文件样例
```

## 运行要求

- Go 1.25+
- Node.js 与 npm，用于桌面开发和正式打包
- Wails CLI 2.13.x；建议与 `go.mod` 中的 Wails 版本一致
- 对应平台的 Wails 原生依赖；使用 `wails doctor` 检查
- 外部媒体工具：`ffmpeg`、`ffprobe`、`mkvextract`、`mediainfo`

外部工具路径可以在桌面端“设置”中通过原生文件选择器配置，也可以直接编辑 `config.yaml`。应用启动后可在设置页执行工具可用性检查。

## 桌面开发

安装与项目版本一致的 Wails CLI，并检查本机依赖：

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
wails doctor
```

安装前端依赖后启动桌面开发模式：

```powershell
Set-Location web
npm ci
Set-Location ..
wails dev
```

`wails dev` 会启动 Vite 热更新和 Wails 窗口。桌面端首次启动会在系统应用数据目录创建默认配置、SQLite 数据库和日志目录。

## 正式打包

Wails 2 不支持在一个主机上完整交叉打包三种桌面平台。发布构建应分别在 Windows、macOS 和 Linux 原生环境执行；产物写入 `build/bin/`。

Windows x64 可执行文件：

```powershell
wails build -clean -platform windows/amd64 -ldflags "-X main.version=0.1.0"
```

安装 NSIS 后可同时生成当前用户范围的安装包：

```powershell
wails build -clean -platform windows/amd64 -nsis -installscope user -ldflags "-X main.version=0.1.0"
```

macOS 通用应用包，需要在 macOS 主机执行：

```bash
wails build -clean -platform darwin/universal -ldflags "-X main.version=0.1.0"
```

Linux x64 二进制，需要在安装了 GTK/WebKitGTK 开发依赖的 Linux 主机执行：

```bash
wails build -clean -platform linux/amd64 -tags webkit2_41 -ldflags "-X main.version=0.1.0"
```

上述 Linux 示例面向提供 WebKitGTK 4.1 的新发行版；仍提供 WebKitGTK 4.0 的系统可以去掉 `-tags webkit2_41` 并安装对应开发包。`build/linux/nya-media.desktop` 是发行包使用的桌面入口元数据，打包时将 `build/appicon.png` 安装为主题图标 `nya-media`。

`.github/workflows/desktop-build.yml` 会在三种原生 GitHub Runner 上执行 Go 测试、前端构建和 Wails 打包，并上传 14 天保留的未签名产物。正式对外分发前还需要在受保护的发布流水线中配置代码签名；macOS 发行版还需要公证。版本发布时同步修改 workflow、`wails.json` 中的 `info.productVersion` 和上述 `main.version` 注入值。

## 桌面数据目录

桌面端将状态放在当前用户的系统数据目录，不依赖启动时的工作目录：

- Windows：`%LOCALAPPDATA%\NyaMediaMetadataTool`
- macOS：`~/Library/Application Support/NyaMediaMetadataTool`
- Linux：`${XDG_DATA_HOME:-~/.local/share}/nya-media-metadata-tool`

目录内包含 `config.yaml`、`nyamedia.db` 和 `logs/`。Windows 的 WebView2 用户数据也位于该目录下。`NYAMMD_DATA_DIR` 可以覆盖根目录，适合便携运行、隔离测试或受控部署；请勿让多个运行中的实例共享同一个覆盖目录。

首次启动且目标 `config.yaml` 与 `nyamedia.db` 都不存在时，桌面端会在启动工作目录和可执行文件目录中查找旧 CLI 数据。识别到旧 `config.yaml` 后会保留未知字段与注释、改写数据库路径，并通过 SQLite 一致性快照导入数据库；旧文件不会被修改。目标目录中任一文件已经存在时会跳过整次迁移。使用任意自定义 `-config` 路径的旧实例无法被自动发现，需要先把配置放到上述候选目录或手动迁移。

SQLite 数据库包含任务、目录、上传目标和本地凭据。应用会在支持 POSIX 权限的平台把配置、数据库及其 WAL 文件收紧为仅当前用户可读写（`0600`）；仍应保护备份和同步目录的访问权限。应用运行中备份请使用 SQLite 一致性快照，不要只复制一个正在写入的 `.db` 文件。当前版本不会把凭据同步到系统钥匙串。

## 外部工具策略

桌面安装包不捆绑 `ffmpeg`、`ffprobe`、MKVToolNix 的 `mkvextract` 或 MediaInfo。这样可以避免显著增大安装包，也允许用户选择符合自身许可证、编解码器和硬件加速需求的构建。建议通过系统包管理器安装，或在设置页为每个工具选择明确的可执行文件路径。

应用首次创建配置时会尝试从 `PATH` 自动识别这些工具。缺失的工具不会影响工作台启动，但依赖它的字幕、媒体探测、BIF 或 NFO 处理会不可用或失败；开始扫描前应在设置页完成工具检查。

## CLI 与 Web 兼容入口

无桌面环境时仍可复制示例配置并启动原有服务：

```powershell
Copy-Item config.example.yaml config.yaml
go run ./cmd/nyammd -config config.yaml
```

默认浏览器入口：

```text
http://127.0.0.1:18880
```

CLI 使用显式传入的配置文件和其中的数据库路径，不会改用桌面端系统数据目录。不要让 CLI 与桌面客户端同时指向同一 SQLite 数据库或重命名历史文件；桌面单实例锁不覆盖另一个 CLI 进程。需要更新内嵌前端时先执行：

```powershell
Set-Location web
npm ci
npm run build
Set-Location ..
go run ./cmd/nyammd -config config.yaml
```

验证后端与前端构建：

```powershell
go test ./...
Set-Location web
npm run build
Set-Location ..
wails build -clean -platform windows/amd64
```

## 配置说明

示例配置见 `config.example.yaml`。主要配置块如下：

- `server`：CLI/Web 服务监听地址和时区，默认 `127.0.0.1:18880`、`Asia/Shanghai`；桌面生产模式不开放该监听端口。
- `database`：SQLite 数据库路径。CLI 示例默认为 `data/nyamedia.db`；桌面端生成指向系统应用数据目录的绝对路径。
- `tools`：`ffmpeg`、`ffprobe`、`mkvextract`、`mediainfo` 路径。
- `processing`：视频扩展名、并发数、文件稳定检测、BIF 参数、处理策略和产物开关。
- `upload`：是否自动发布、上传目标并发、番剧变更合并窗口、自动重试次数和默认发布文件类型。至少选择一种默认文件类型；网盘目标、目录路由与凭据保存在 SQLite，由工作台的“上传”页面管理；Cookie 不会写入 `config.yaml` 或 `GET /api/config` 响应。
- `renaming`：重命名预览并发数。
- `scraping`：TMDB、fanart.tv、语言、地区、备用语言、代理等刮削配置。

`processing.strategy` 当前支持：

- `missing`：只补缺失产物。
- `force`：强制重建产物。

监控目录保存在 SQLite 中，启动后以数据库里的媒体目录为准，而不是直接读取 YAML 中的 `watchDirs`。

### 网盘发布流程

上传批次不是按一次整库扫描划分，而是按“监控目录 + 番剧根目录”聚合。每个媒体任务在元数据成功后把视频和已生成的伴生文件加入该番剧的 collecting 批次；新的文件会延长安静窗口。安静窗口结束且该番剧没有待处理任务时，批次才会封存并发往目标。

- 一个完整扫描可产生多个番剧批次；同一番剧连续下载的多集会合并成一个批次。
- 每个目标都有一个默认路由，可应用于所有监控目录；也可关闭默认路由，仅选择目录。选中的目录可以分别覆盖远端根目录、冲突策略和文件类型。关闭默认路由时至少要选择一个目录，删除最后一个已选目录也不会意外回退为全目录上传。
- 新批次会快照最终生效的远端根目录、冲突策略和文件类型，之后修改目标不会改变历史批次。
- 每个目标完成后产生一个 `upload_target_verified` outbox 事件。事件包含 Provider、远端根目录、番剧 key、revision 和文件清单，供 NyaMedia 或其他通知消费者按目标独立消费。
- 第一版不会自动删除本地或远端文件。默认碰撞策略为 `fail`，避免同名不同大小文件被静默覆盖；只有明确选择 `replace` 才会替换远端同名文件。

### Provider 扩展

`upload.Manager.RegisterProviderDescriptor` 是新增网盘实现的注册入口：它同时注册上传 Builder、显示名称和所需凭据键。`GET /api/upload/provider-types` 始终反映运行时已安装的 Provider，因此前端会自动启用新类型，而不是维护一份独立的硬编码列表。

未安装的预留 Provider（当前为 `115open`、`123pan`、`baidupan`）可以被识别但不能启用，不会进入上传重试队列。通用凭据接口为 `PUT/DELETE /api/upload/providers/{id}/secrets/{key}`；只允许 Provider descriptor 声明的键，且不提供读取接口。`115cookie` 仍保留专用 Cookie 与二维码授权流程。

## 生成产物

工具会按视频文件同名或 Emby 常见规则生成伴生文件：

```text
视频文件名.nfo
视频文件名-mediainfo.json
视频文件名-thumb.jpg
视频文件名-320-10.bif
视频文件名.语言.备注.字幕格式
tvshow.nfo
season.nfo
poster.jpg
fanart.jpg
clearlogo.png
clearart.png
seasonXX-poster.jpg 或季度目录内 poster.jpg
```

说明：

- `mediainfo.json` 优先使用 `mediainfo --Output=JSON`，失败时回退到 `ffprobe`。
- 字幕使用 `ffprobe` 枚举字幕轨，并通过 `ffmpeg` 导出当前支持的文本字幕；不支持的字幕编码会跳过。
- BIF 使用 `ffmpeg` 抽帧后由项目写入 BIF 文件，命名包含宽度和间隔秒数。
- NFO 使用 `ffprobe` 写入流信息，并在 TMDB 可用时补充标题、简介、日期、评分、演员、导演、编剧和 provider id。
- 单集缩略图优先使用 TMDB still，缺失时回退到视频 50% 位置抽帧。
- 剧集/季度 NFO 与图片会按扫描批次做作用域去重，避免同一轮扫描中重复生成。

## 工作台功能

桌面客户端与 CLI 的浏览器管理端共用以下页面；桌面环境会额外启用原生文件与通知能力：

- 仪表盘：查看服务健康、工具状态、最近任务与最近产物。
- 设置：编辑服务、工具、处理策略、TMDB/fanart.tv 等配置。
- 媒体目录：新增、编辑、删除目录，配置递归、实时监控、启动扫描和目录级处理策略。
- 任务：筛选任务、查看详情日志和产物、取消运行中任务、重试或忽略任务、手动扫描生成。
- 重命名：批量预览、TMDB 匹配、模板编辑、季集批量修正、执行重命名、查看历史并回滚。
- 剧集核对：检查缺集与伴生文件、对比 Emby 元数据、通过 SFTP 对齐本地与远端文件。

## 重命名模板

默认模板：

```text
{show} - S{season:00}E{episode:00} - {title}
```

常用占位符：

- `{show}`、`{showOriginal}`、`{title}`、`{releaseGroup}`
- `{season}`、`{episode}`、`{year}`
- `{tmid}` 或 `{tmdbShowId}`
- `{show:zh-CN}`、`{title:ja-JP}` 这类语言限定字段
- `{season:00}`、`{episode:000}` 这类补零格式
- `{if:releaseGroup| - {releaseGroup}|}` 条件片段

重命名会同步处理同名前缀的常见附属文件，例如 `.nfo`、字幕、`.json`、`.bif`、图片等。执行记录会写入历史，可在工作台检查并回滚。

## 剧集核对

剧集核对的主要入口在工作台的 `剧集核对` 页面，包含三个能力：

- `剧集缺漏`：扫描本地剧集目录，结合 `tvshow.nfo` 或手动选择的 TMDB 剧集 ID 判断缺集，并检查单集/季度/剧集伴生文件。
- `Emby 与本地核对`：通过 Emby API 对比本地 NFO、图片状态、provider id、文件名等信息。
- `文件对齐检查`：通过 SFTP 对比本地目录与远端目录，支持大小与 MD5 检查，也支持本地视频匹配远端同名 `.strm`。

## BIF 解包 CLI

命令入口：`cmd/bifunpack`。

```powershell
go run ./cmd/bifunpack -- "D:\Media\TV\Example\Example-320-10.bif"
go run ./cmd/bifunpack -o "D:\Temp\bif-frames" -- "D:\Media\TV\Example\Example-320-10.bif"
```

该工具会校验 BIF 头并将其中的 JPEG 帧导出为 `*-frame-0001-0ms.jpg` 这类文件，适合调试 BIF 生成结果。

## HTTP API 摘要

主服务提供 JSON API，Web 端也是通过这些接口工作：

- `GET /api/health`
- `GET /api/config`、`PUT /api/config`
- `GET /api/tools/status`、`POST /api/tools/check`
- `GET /api/watch-dirs`、`POST /api/watch-dirs`、`PUT /api/watch-dirs/{id}`、`DELETE /api/watch-dirs/{id}`
- `GET /api/tasks`、`GET /api/tasks/{id}`
- `POST /api/tasks/rescan`
- `POST /api/tasks/cancel-active`
- `POST /api/tasks/retry`
- `POST /api/tasks/ignore`
- `GET /api/artifacts`
- `GET /api/uploads/summary`、`GET /api/uploads`、`GET /api/uploads/{id}`
- `POST /api/uploads/targets/{id}/retry`、`POST /api/uploads/targets/{id}/cancel`
- `GET/POST /api/upload/providers`、`GET/PUT/DELETE /api/upload/providers/{id}`
- `PUT/DELETE /api/upload/providers/{id}/cookie`、`POST /api/upload/providers/{id}/check`
- `PUT/DELETE /api/upload/providers/{id}/secrets/{key}`
- `POST/GET /api/upload/providers/{id}/auth/115cookie`
- `GET /api/upload/provider-types`
- `GET /api/upload/events`、`POST /api/upload/events/claim`
- `POST /api/upload/events/{id}/ack`、`POST /api/upload/events/{id}/fail`
- `POST /api/rename/preview`
- `POST /api/rename/preview/stream`
- `POST /api/rename/preview/item`
- `POST /api/rename/apply`
- `GET /api/rename/history`
- `GET /api/rename/history/{id}/undo-check`
- `POST /api/rename/history/{id}/undo`
- `POST /api/audit/missing`
- `POST /api/audit/emby`
- `POST /api/audit/files`
- `GET /api/tmdb/search-tv`
- `GET /api/tmdb/episode`
- `GET /api/fs/directories`

## 忽略目录

在目录中放置 `.ignore` 文件后，监控、扫描和重命名预览会跳过该目录及其子目录。这个规则也会向上检查祖先目录。

## 注意事项

- `PUT /api/config` 会写入配置文件，但返回 `restartRequired: true`；服务级配置建议重启后生效。
- 115 Cookie 和未来 Provider 的通用凭据当前保存在本地 SQLite 数据库中，不会经 API 回显。请把数据库目录视作凭据存储，限制本机账户和备份文件的访问权限。
- TMDB 或 fanart.tv 相关能力依赖网络和 API 配置，未配置时会降级或跳过。
- BIF 硬件加速支持 `cpu`、`auto`、`nvidia`、`intel`、`amd`、`d3d11va`、`dxva2`、`vaapi`、`videotoolbox`，失败会按策略回退。
- 本地与远端文件对齐检查当前远端实现为 SFTP，默认建议配置 `known_hosts`，临时调试时才跳过主机指纹校验。
- 当前仓库里 `docs/v1-implementation.md` 仍偏历史设计稿；以 README 和代码实现为准。

## 参考文档

- `docs/emby-compatibility.md`：Emby 兼容命名清单。
- `docs/example/`：样例 NFO、图片、BIF、mediainfo 产物。
- `docs/v1-implementation.md`：早期 V1 方案记录。
