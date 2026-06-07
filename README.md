# NyaMediaMetadataTool

面向本地媒体库的元数据与伴生文件生成工具。项目当前以 Go 常驻服务为核心，提供内嵌 Web 管理端，用于监控媒体目录、生成 Emby 友好的本地伴生文件、查看任务日志，并提供剧集重命名与核对工具。

当前主线不是下载器，也不是 Emby 插件；它更像一个落盘后的媒体整理与产物生成服务，可以和 AniRss、qBittorrent、Emby 等现有链路并行工作。

## 当前能力

- 媒体目录管理：支持多个目录、递归扫描、实时监控、手动重扫、目录级处理策略覆盖。
- 任务队列：SQLite 记录任务、日志、产物和工具状态；支持并发处理、失败重试、取消运行中任务、重新排队和忽略失败任务。
- 伴生文件生成：支持字幕抽取、`mediainfo.json`、BIF 预览索引、单集 NFO、剧集/季度 NFO、单集缩略图。
- 元数据增强：支持 TMDB 查询、缓存、语言/地区配置、备用语言、代理，以及可选 fanart.tv 图片来源。
- 图片接管：默认关闭；开启后可生成 `poster.jpg`、`fanart.jpg`、`clearlogo.png`、`clearart.png` 和季度海报。
- Web 管理端：提供仪表盘、设置、媒体目录、任务、重命名、剧集核对等页面。
- 批量重命名：支持预览、手动修正、模板占位符、TMDB 匹配、附属文件随动重命名、历史回滚。
- 剧集核对：Web 端支持本地缺集/伴生文件检查、Emby API 对比、本地与远端 SFTP 文件对齐检查。
- 辅助工具：保留 `bifunpack` BIF 解包命令，用于调试 BIF 生成结果。

## 项目结构

```text
cmd/
  nyammd/        主服务入口
  bifunpack/     BIF 图片解包 CLI
internal/
  api/           HTTP API 与静态前端入口
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
  watcher/       fsnotify 目录监控
web/
  src/           React + TypeScript 前端
docs/
  example/       伴生文件样例
```

## 运行要求

- Go 1.22+
- Node.js 与 npm，仅在修改或重新构建前端时需要
- 外部媒体工具：
  - `ffmpeg`
  - `ffprobe`
  - `mkvextract`
  - `mediainfo`

外部工具路径可在 `config.yaml` 或 Web 设置页中配置。服务启动后可以在 Web 端执行工具可用性检查。

## 快速开始

复制示例配置：

```powershell
Copy-Item config.example.yaml config.yaml
```

启动服务：

```powershell
go run ./cmd/nyammd -config config.yaml
```

访问 Web 管理端：

```text
http://127.0.0.1:18880
```

如果修改了前端，需要先构建前端，再启动或构建 Go 服务：

```powershell
Set-Location web
npm install
npm run build
Set-Location ..
go run ./cmd/nyammd -config config.yaml
```

验证后端与前端构建：

```powershell
go test ./...
Set-Location web
npm run build
```

## 配置说明

示例配置见 `config.example.yaml`。主要配置块如下：

- `server`：服务监听地址和时区，默认 `127.0.0.1:18880`、`Asia/Shanghai`。
- `database`：SQLite 数据库路径，默认 `data/nyamedia.db`。
- `tools`：`ffmpeg`、`ffprobe`、`mkvextract`、`mediainfo` 路径。
- `processing`：视频扩展名、并发数、文件稳定检测、BIF 参数、处理策略和产物开关。
- `renaming`：重命名预览并发数。
- `scraping`：TMDB、fanart.tv、语言、地区、备用语言、代理等刮削配置。

`processing.strategy` 当前支持：

- `missing`：只补缺失产物。
- `force`：强制重建产物。

监控目录保存在 SQLite 中，启动后以数据库里的媒体目录为准，而不是直接读取 YAML 中的 `watchDirs`。

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

## Web 功能

Web 管理端当前包含这些页面：

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

重命名会同步处理同名前缀的常见附属文件，例如 `.nfo`、字幕、`.json`、`.bif`、图片等。执行记录会写入历史，可在 Web 端检查并回滚。

## 剧集核对

剧集核对的主要入口在 Web 管理端的 `剧集核对` 页面，包含三个能力：

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
- TMDB 或 fanart.tv 相关能力依赖网络和 API 配置，未配置时会降级或跳过。
- BIF 硬件加速支持 `cpu`、`auto`、`nvidia`、`intel`、`amd`、`d3d11va`、`dxva2`、`vaapi`、`videotoolbox`，失败会按策略回退。
- 本地与远端文件对齐检查当前远端实现为 SFTP，默认建议配置 `known_hosts`，临时调试时才跳过主机指纹校验。
- 当前仓库里 `docs/v1-implementation.md` 仍偏历史设计稿；以 README 和代码实现为准。

## 参考文档

- `docs/emby-compatibility.md`：Emby 兼容命名清单。
- `docs/example/`：样例 NFO、图片、BIF、mediainfo 产物。
- `docs/v1-implementation.md`：早期 V1 方案记录。
