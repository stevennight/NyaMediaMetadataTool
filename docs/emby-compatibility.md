# Emby 兼容命名清单

本文档定义 V1 产物的 Emby 兼容命名规则，目标是让工具生成的文件尽可能被 Emby 直接识别。

注意：

- 当前 V1 主线并不默认接管图片下载
- 因此本文档中的 `poster/fanart/clearlogo/clearart/thumb` 命名规则，主要用于“后续启用图片接管或图片兜底”时参考
- V1 默认主线仍是 `nfo / mediainfo / bif / 字幕`

已参考当前样本目录 `docs/example/`，其中可见现有产物包括：

- `tvshow.nfo`
- `season.nfo`
- `poster.jpg`
- `fanart.jpg`
- `clearlogo.png`
- `clearart.png`
- `视频文件名.nfo`
- `视频文件名-mediainfo.json`
- `视频文件名-thumb.jpg`
- `视频文件名-320-10.bif`

## 1. 规则原则

- 优先遵循 Emby 常见伴生文件命名
- 能复用视频同名规则的，优先使用视频同名
- 单集图片统一收敛到 `-thumb.jpg`
- 剧集/季度级别图片与单集级别图片分开处理
- 如果远程刮削结果缺失，允许回退到本地生成结果

## 2. 电影场景

示例目录：

```text
Movies/
  Spirited Away (2001)/
    Spirited Away (2001).mkv
```

建议产物：

```text
Spirited Away (2001).nfo
Spirited Away (2001)-mediainfo.json
Spirited Away (2001)-thumb.jpg
poster.jpg
backdrop.jpg
Spirited Away (2001).zh.ass
Spirited Away (2001).jpn.forced.sup
```

说明：

- `视频文件名.nfo`：电影 NFO
- `视频文件名-mediainfo.json`：媒体信息
- `视频文件名-thumb.jpg`：电影级缩略图，可由抽帧或远程图兜底生成
- `poster.jpg`：电影海报
- `backdrop.jpg`：电影背景图
- 字幕与视频同名，附加语言和备注

## 3. 剧集根目录场景

示例目录：

```text
TV/
  Bleach (2004)/
```

建议产物：

```text
tvshow.nfo
poster.jpg
fanart.jpg
clearlogo.png
clearart.png
```

说明：

- `tvshow.nfo`：剧集级 NFO
- `poster.jpg`：剧集主海报
- `fanart.jpg`：剧集背景图
- `clearlogo.png`：剧集 logo 图
- `clearart.png`：剧集透明插图

V1 建议：

- `tvshow.nfo` 作为剧集根目录必做
- `poster.jpg`、`fanart.jpg` 默认继续交由 Emby 负责
- `clearlogo.png`、`clearart.png` 作为后续增强项，优先兼容你当前样本
- 如果后续需要兼容更多 Emby 命名别名，可再补 `backdrop.jpg`

## 4. 季度目录场景

示例目录：

```text
TV/
  Bleach (2004)/
    Season 01/
```

说明：

- 你当前样本里已经出现 `season.nfo`
- 说明现有链路对季度级元数据采用了“季度目录内 `season.nfo`”的方式
- 因此 V1 需要优先兼容 `season.nfo`
- 至于季度图片，样本里暂未看到明确文件，先不在 V1 强制要求

V1 建议产物：

```text
season.nfo
```

## 5. 单集场景

示例目录：

```text
TV/
  Bleach (2004)/
    Season 01/
      Bleach S01E01.mkv
```

建议产物：

```text
Bleach S01E01.nfo
Bleach S01E01-mediainfo.json
Bleach S01E01-thumb.jpg
Bleach S01E01.zh.ass
Bleach S01E01.jpn.Commentary.sup
```

说明：

- `视频文件名.nfo`：单集 NFO
- `视频文件名-mediainfo.json`：单集媒体信息
- `视频文件名-thumb.jpg`：单集图片
- 字幕与视频同名，附加语言和备注

单集图片来源优先级：

1. `TMDB` 当集剧照
2. 本地抽帧图

也就是说：

- 单集一般不用 `poster.jpg`
- 单集图片统一落地到 `视频文件名-thumb.jpg`

## 6. 字幕规则

命名模板：

`视频文件名.语言.备注或字幕名称.字幕格式`

示例：

```text
Bleach S01E01.zh.ass
Bleach S01E01.jpn.forced.srt
Bleach S01E01.eng.Commentary.sup
```

规则：

- `语言` 优先使用规范短码
- `备注` 来自字幕轨标题或标签
- 图片字幕保留原格式
- 无法稳定转换时不伪造为文本字幕

## 7. NFO 规则

V1 推荐文件名：

- 电影：`视频文件名.nfo`
- 剧集根目录：`tvshow.nfo`
- 季度目录：`season.nfo`
- 单集：`视频文件名.nfo`

说明：

- 剧集场景通常至少包含剧集级 NFO
- 结合现有样本，季度级也要支持 `season.nfo`
- 对于单集，V1 建议也允许生成同名 `nfo`
- 如果后续实测发现 Emby 在某类目录结构下对单集 `nfo` 有额外要求，再微调

## 8. MediaInfo 规则

V1 推荐文件名：

- `视频文件名-mediainfo.json`

适用范围：

- 电影
- 单集
- 可选支持其他视频文件

说明：

- 这是本工具自定义产物，不是 Emby 标准文件
- 主要用于可观测性、调试、二次处理

## 9. BIF 规则

V1 暂定命名：

- `视频文件名-320-10.bif`

字段含义：

- `320`：缩略图宽度
- `10`：每 10 秒一张

说明：

- 仅预留规则，不作为 V1 强制交付项
- 是否被 Emby 直接消费，需要后续实测

## 10. 推荐落地矩阵

| 场景 | 文件 | V1 计划 |
| --- | --- | --- |
| 电影 | `视频文件名.nfo` | 做 |
| 电影 | `视频文件名-mediainfo.json` | 做 |
| 电影 | `视频文件名-thumb.jpg` | 可选兜底 |
| 电影 | `poster.jpg` | 默认由 Emby 负责 |
| 电影 | `backdrop.jpg` | 默认由 Emby 负责 |
| 电影 | 外挂字幕 | 做 |
| 剧集根目录 | `tvshow.nfo` | 做 |
| 剧集根目录 | `poster.jpg` | 默认由 Emby 负责 |
| 剧集根目录 | `fanart.jpg` | 默认由 Emby 负责 |
| 剧集根目录 | `clearlogo.png` | 后续增强 |
| 剧集根目录 | `clearart.png` | 后续增强 |
| 季度目录 | `season.nfo` | 做 |
| 单集 | `视频文件名.nfo` | 做 |
| 单集 | `视频文件名-mediainfo.json` | 做 |
| 单集 | `视频文件名-thumb.jpg` | 可选兜底 |
| 单集 | 外挂字幕 | 做 |
| 单集 | BIF | 做 |

## 11. 待样本确认项

真正开发前，建议用你现有目录中的实际样本再确认以下问题：

- 神医助手当前生成的单集图是否就是 `-thumb.jpg`
- 它是否同时生成 `fanart.jpg` 与 `backdrop.jpg`，还是只生成其一
- 单集 `nfo` 是否存在，以及命名是否为视频同名
- 季度图是否存在，以及是否使用 `seasonXX-*` 命名
- 是否还存在其他你依赖但目前没写进 V1 的伴生文件
