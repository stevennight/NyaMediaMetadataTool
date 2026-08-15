# 百度网盘网页上传方案确认文档

更新时间：2026-08-15

本文记录百度网盘网页端上传流程的已验证结论、拟采用的默认处理方式和仍需确认的事项。本文只记录方案，不代表主应用已经接入该流程。

本文中的 MAO 文件和远端路径是固定的验证向量，不是主应用的全局硬编码上传目标。

当前主应用已经接入本方案中的 `preuploadBeforeRapid` Provider 配置。默认值仍为关闭；开启后才会启用 MD5 计算与普通分片上传的并行竞速。

## 1. 当前结论

百度网盘网页端可以在不上传普通文件分片的情况下，通过 `rapidupload` 直接创建完整文件。成功条件不是 `precreate.return_type`，而是：

- `rapidupload` 返回 `errno=0`；
- 返回 `info.fs_id`；
- 返回的路径和文件大小符合预期。

因此主应用的暂定默认策略是：

1. 检查本地文件状态和远端目标文件冲突。
2. 计算完整文件 MD5 和前 256 KiB 的 slice MD5。
3. 调用网页端 `precreate` 获取 `uploadid`。
4. 不预先上传普通分片，直接调用网页端 `rapidupload`。
5. `rapidupload` 成功后以返回的 `fs_id` 和精确路径作为上传结果。
6. `rapidupload` 未命中或失败时，才回退到普通分片上传，最后调用网页端 `create`。

预先上传分片保留为用户可配置选项，默认关闭。

## 2. 已验证的网页端接口流程

### 2.1 预创建

接口：

```text
POST https://pan.baidu.com/api/precreate?rtype=1&app_id=250528&channel=chunlei&web=1&clienttype=0
```

网页会通过 Cookie 和 `bdstoken` 鉴权。表单中的关键字段：

| 字段 | 值或来源 | 说明 |
| --- | --- | --- |
| `path` | 目标完整路径 | 例如 `/Video/NEW/example.mkv` |
| `target_path` | 目标目录 | 例如 `/Video/NEW/` |
| `autoinit` | `1` | 网页端流程使用 |
| `block_list` | 初始分片 MD5 列表 | 新任务使用网页端固定初始值 |
| `local_mtime` | 本地文件 Unix 秒时间戳 | 来源于本地文件修改时间 |
| `rtype` | `1` | 放在查询参数中 |

已观察到 `precreate` 返回 `return_type=1` 仍然可以随后 `rapidupload` 成功。因此 `return_type` 只记录日志，不作为是否尝试 rapidupload 的判断条件。

预创建返回的 `uploadid` 是上传会话 ID，必须用于后续网页端 `superfile2` 或 `rapidupload`。它不是最终文件的 `fs_id`。

`precreate` 返回的 `block_list` 只表示服务端认为可以复用或需要处理的分片序号，不能单独证明已经命中秒传。官方文档说明空列表按 `[0]` 处理；实际使用时仍需结合上传会话和后续接口响应判断。网页端对全新任务会把上传范围视为 `all`，不会因为本次返回 `[0, 1]` 就只上传这两个分片；只有恢复本地缓存上传会话时，才会使用返回列表跳过已处理分片。

### 2.2 普通分片上传

定位上传服务器：

```text
GET https://d.pcs.baidu.com/rest/2.0/pcs/file?method=locateupload
```

分片接口：

```text
POST https://c2.pcs.baidu.com/rest/2.0/pcs/superfile2?method=upload
```

关键查询参数：

```text
app_id=250528
channel=chunlei
web=1
clienttype=0
path=<目标完整路径>
uploadid=<precreate 返回值>
uploadsign=0
partseq=<从 0 开始的分片序号>
```

网页端捕获到的普通上传分片大小为 4 MiB，分片数量为：

```text
ceil(file_size / 4194304)
```

对于 MAO 测试文件 `741385398` 字节，分片数量为 `177`。

### 2.3 快速上传

接口：

```text
POST https://pan.baidu.com/api/rapidupload?rtype=1&app_id=250528&channel=chunlei&web=1&clienttype=0
```

关键表单字段：

| 字段 | 来源 | 说明 |
| --- | --- | --- |
| `uploadid` | `precreate` | 上传会话 ID |
| `path` | 目标完整路径 | 期望创建的文件路径 |
| `target_path` | 目标目录 | 目标文件夹 |
| `content-length` | 本地文件大小 | 字节数 |
| `content-md5` | 普通完整 MD5 经百度网页编码 | 不是直接发送普通 MD5 |
| `slice-md5` | 前 256 KiB 的 MD5 经百度网页编码 | 不是直接发送普通 MD5 |
| `data_time` | 请求时的 Unix 秒时间戳 | 用于计算采样偏移 |
| `data_offset` | 根据算法计算 | 采样数据在文件中的偏移 |
| `data_content` | `data_offset` 开始的 256 KiB 文件数据 Base64 | 保留标准 Base64 内容和 padding |
| `local_mtime` | 本地文件 Unix 秒时间戳 | 网页流程会发送 |

成功响应示例的有效标志是：

```json
{
  "errno": 0,
  "info": {
    "fs_id": 420340925995315,
    "path": "/Video/NEW/MAO (2026)/Season 1/MAO-web-probe.mkv",
    "size": 741385398
  }
}
```

网页端 `rapidupload` 成功后已经创建正式文件，不需要再调用网页端 `create`。普通分片回退流程完成所有分片后，才需要调用 `create`。

## 3. 已确认的 MD5 和采样算法

### 3.1 普通 MD5

- `content-md5` 的基础值是整个文件的普通 MD5。
- `slice-md5` 的基础值是文件前 256 KiB 的普通 MD5。
- 两者在发送网页端 `precreate`/`rapidupload` 相关请求时，转换为百度网页端使用的特殊 MD5 表示。
- Open API 返回的文件元数据可能也使用该特殊表示，读取时需要解码后再和本地普通 MD5 比较。

MAO 测试文件的已验证值：

```text
普通完整 MD5:       4e6ff05e39d763d062288e3c2798de36
网页 content-md5:  38f426b7cnc43db126bb9b51eb8343d3
网页 slice-md5:    02ae41002n4751a1aaa8555600491241
```

### 3.2 `data_offset`

网页端 JS 使用的算法：

```text
sampleSize = 262144
seed = uk + encodedContentMD5 + dataTime
digest = MD5(seed)
raw = digest 前 4 字节按大端序解释为 uint32
dataOffset = raw % (fileSize - sampleSize + 1)
```

其中：

- `uk` 通过网页会话的 `gettemplatevariable` 获取，是百度网页端用户变量；
- `uk` 不是 Open API 的 `access_token`；
- `dataTime` 必须和请求中的 `data_time` 完全一致；
- `data_content` 必须是从 `dataOffset` 开始读取的 256 KiB 原始文件数据的标准 Base64。

已验证的 MAO HAR 向量：

```text
uk          = 416237033
data_time   = 1786779706
data_offset = 426185739
```

## 4. 官方网页 JS 的分片和 rapidupload 关系

从 HAR 中提取到的网页端上传器配置：

```text
FILE_CHUNK_SIZE   = 4194304       // 4 MiB
chunkThread       = 3             // 同时最多 3 个分片请求
FILE_RAPId_MIN_SIZE = 262144      // 256 KiB
```

网页端的实际行为不是“固定先上传 N 个分片”：

1. 首先计算前 256 KiB 的 slice MD5并完成 `precreate`。
2. `precreate` 成功后启动普通分片上传和完整文件 MD5 计算。
3. 普通上传最多同时运行 3 个分片线程。
4. 完整 MD5 计算完成后，独立触发 `rapidupload`。
5. `rapidupload` 成功后设置 `fs_id`，并终止剩余普通分片请求。
6. `rapidupload` 失败时继续普通分片上传，最后走 `create`。

所以“rapidupload 发起前已经上传几个分片”由 MD5 计算速度和网络速度决定。HAR 中观察到的分片 `0` 到 `5` 不是协议规定的固定数量。

当前探针的 `warmup-parts` 是人为测试开关：它会单线程、顺序上传指定数量的 4 MiB 分片，再调用 `rapidupload`。它只用于验证接口，不等价于官方网页端的 3 路竞速实现。

## 5. 拟接入主应用的处理模式

### 5.1 默认模式：先计算，再快速上传

默认关闭预上传分片：

```text
读取并锁定本地文件状态
    -> 计算完整 MD5、slice MD5、分片 MD5
    -> 检查远端目标冲突
    -> precreate
    -> rapidupload
       -> 成功：记录 fs_id，完成
       -> 失败/未命中：普通分片上传
                      -> create
                      -> 验证 fs_id、路径、大小
```

这里的普通分片 MD5可以复用前面计算出的分片 MD5，避免重复读取和计算。

### 5.2 可选模式：MD5 计算期间预上传分片

该模式用于获得更早的上传进度，并尽量复现官方网页端行为：

```text
slice MD5 + precreate
    -> 3 路普通分片上传并行开始
    -> 同时计算完整 MD5
    -> 完整 MD5完成后调用 rapidupload
       -> 成功：取消剩余分片
       -> 失败：继续分片上传并 create
```

该模式不设置固定的“预上传分片数量”。预上传应由 3 路分片调度器运行到完整 MD5完成或 rapidupload成功为止。固定数量只保留给诊断探针使用。

## 6. 拟提供的配置

以下名称是实现建议，最终字段名仍需在开发前确认：

| 配置 | 默认值 | 作用 |
| --- | --- | --- |
| `preuploadBeforeRapid` | `false` | 是否在完整 MD5 完成前预上传普通分片；在 Provider 编辑页配置并随上传批次快照 |
| `preuploadConcurrency` | `3` | 预上传并发数，和官方网页端一致 |
| `chunkSize` | `4194304` | 分片大小，网页端固定为 4 MiB，通常不开放修改 |
| `rapidUploadEnabled` | `true` | 是否尝试网页端 rapidupload |

暂不把 `warmupParts` 作为主应用配置。它是探针诊断参数，不代表官方固定策略。

## 7. 冲突、重命名和成功判定

百度网页端在目标文件已存在时可能自动重命名。例如：

```text
期望：/Video/NEW/MAO (2026)/Season 1/MAO-web-probe.mkv
实际：/Video/NEW/MAO (2026)/Season 1/MAO-web-probe_20260815_162559.mkv
```

这次响应同时返回了有效 `fs_id`，说明百度确实创建了文件；探针因为实际路径不等于期望路径而报告校验失败。

主应用不应把这种结果直接报告为目标路径上传成功。处理原则暂定为：

- 上传前按当前 Provider 的冲突策略检查目标文件；
- 不接受百度静默改名作为目标路径成功；
- `rapidupload` 返回的 `fs_id`、路径、大小必须记录；
- 实际路径和期望路径不一致时，标记为冲突/路径校验失败，并保留实际路径和 `fs_id` 供排查；
- 不在 `rapidupload` 成功后追加调用 `create`。

当前已存在的 Provider 冲突策略为 `skip`、`fail`、`replace`。网页端 rapidupload 如何严格对应这三种策略，仍需单独确认，尤其是如何阻止百度自动重命名。

## 8. 日志和验证要求

每个上传阶段至少记录：

- 阶段名称：`precreate`、`locateupload`、`superfile2`、`rapidupload`、`create`、`verify`；
- HTTP 状态和百度 `errno`/`error_code`；
- `return_type`，但不据此单独判断成功或失败；
- `uploadid` 是否存在，不记录完整 Cookie、`bdstoken` 或 `data_content`；
- 分片数量、分片序号、已传字节数和进度；
- rapidupload 的 `data_time`、`data_offset`、`data_length`；
- 返回的 `fs_id`、实际路径和大小；
- 期望路径和实际路径是否一致。

成功必须同时满足：

1. 拿到非空 `fs_id`；
2. 实际路径符合期望路径；
3. 实际大小符合本地文件大小；
4. 主应用的远端验证接口能够读取该文件。

## 9. 当前已验证结果

### 9.1 固定验证向量

```text
local_file:
D:\Download\Anirss\TV\MAO (2026)\Season 1\MAO - S01E01 - 菜花と摩緒 - LoliHouse.mkv

file_size: 741385398
chunk_size: 4194304
chunk_count: 177

remote_path:
/Video/NEW/MAO (2026)/Season 1/MAO-web-probe.mkv
```

测试运行前仍应检查本地文件实际大小等于 `741385398`。主应用正式上传其他文件时，`file_size` 和远端路径必须从任务实际数据计算或读取，不能使用这组固定值。

使用 MAO 文件和专用测试路径验证成功：

```text
precreate:    errno=0, return_type=1, uploadid 有效
warmup:       6 个 4 MiB 分片上传成功
rapidupload:  errno=0
fs_id:        420340925995315
size:         741385398
```

第二次使用相同测试路径时，百度返回了新的带时间后缀路径和 `fs_id=892363487656649`，证明重复文件时存在自动重命名行为，也证明探针对实际路径做严格校验是必要的。

### 9.2 并发分片实现结论

可以按照官方网页端实现单个文件的并发分片上传，拟采用以下固定规则：

```text
chunk_size              = 4194304
per_file_concurrency    = 3
worker_start_stagger    = 1 second  // 对齐官方 JS 的启动方式，可作为实现细节
chunk_retry_limit       = 3
partseq                  = 0..chunk_count-1
```

调度方式不是一次性提交 3 个分片后等待，而是 3 个 worker 各自持续领取下一个未完成的 `partseq`。一个分片完成后，该 worker 继续上传下一个分片；失败时只重试当前 `partseq`，不重新上传其他分片。

预上传开关关闭时，并发分片只在 `rapidupload` 失败后作为回退流程启动。预上传开关开启时，3 个分片 worker 和完整 MD5 计算同时运行；`rapidupload` 成功后取消剩余 worker，不能再调用 `create`；`rapidupload` 失败后等待分片全部完成，再调用 `create`。

每个分片请求使用独立的 `partseq`、文件区间和 multipart body。进度按各 worker 已确认上传的字节数累加；rapidupload 成功时将文件阶段直接标记为完成，并保留此前的 MD5 计算阶段日志。

### 9.3 请求间隔和多文件并发

不对网页端普通分片请求设置全局固定请求间隔。官方 JS 的主要控制手段是：

- 单文件最多 3 个分片请求并发；
- 一个分片完成后立即补充下一个分片；
- 初始 3 个 worker 以约 1 秒错峰启动，但这不是每个请求之间的固定间隔；
- 多文件并发由文件级 `uploadingLimit` 控制。当前 HAR 中普通账号为 1，SVIP 为 3。

主应用应将以下两个限制分开：

```text
per_file_chunk_concurrency = 3
max_concurrent_files       = 独立配置，默认 1
```

### 9.4 按官方用户等级选择文件并发数

网页端通过登录会话获取用户信息：

```text
GET https://pan.baidu.com/api/loginStatus?clienttype=0&app_id=250528&web=1&channel=chunlei&version=<timestamp>
```

响应中的 `login_info.vip_identity` 是官方上传器使用的等级来源。网页 JS 的映射为：

```text
vipType = parseInt(vip_identity / 10, 10)

NORMAL = 0
VIP    = 1
SVIP   = 2
```

官方 H5 上传器随后使用：

```text
vipType == SVIP  -> uploadingLimit = 3
其他/未知等级    -> uploadingLimit = 1
```

`vip_level` 是会员等级显示值，不直接用于这个并发判断。当前 HAR 中：

```text
vip_identity = 21 -> vipType = 2 -> SVIP -> 最多 3 个文件并发
vip_level    = 9  -> 会员等级显示值
```

实现时应在网页 Provider 建立会话时读取并缓存 `vip_identity`，只保存规范化后的等级和并发上限，不把 Cookie 或完整响应写入日志。等级读取失败、字段缺失或映射结果未知时，保守使用 `max_concurrent_files=1`。

### 9.5 文件级并发需要 Manager 配合

当前架构中：

- `Provider.Upload` 一次只负责一个文件；
- `Manager.Options.Concurrency` 控制的是上传 target worker 数量，不是同一 target 内的文件数量；
- `Manager.processTarget` 当前按顺序遍历同一 target 的 transfers；
- 当前 runtime 状态对一个 target 只保留一个 active transfer。

因此，官方的多文件并发不能只在 Provider 内部实现。拟采用的改动边界是：

1. 网页 Provider 暴露一个可选的文件并发能力，例如 `MaxConcurrentFiles()`；
2. Manager 为同一个 Provider 账号/`ProviderID` 维护共享信号量，不能让不同 target 为同一账号各自突破上限；
3. `processTarget` 改为在该信号量约束下调度同一 target 的多个 transfers；
4. 全局 Manager 并发作为上限之一，最终有效并发取全局上限、Provider 等级上限和系统资源上限的最小值；
5. 每个 transfer 独立持久化状态、失败和完成结果，target 只有在全部 transfers 结束后才能完成；
6. runtime/UI 需要支持多个 active transfer，或提供聚合的总字节数、总文件数和当前文件信息。

这不会改变其他 Provider 的单文件 `Upload` 契约；单文件内部的 3 路分片并发仍由百度网页 Provider 自己负责。

如果对所有 `superfile2` 请求套用现有的全局 `requestInterval`，会把官方的 3 路分片和多文件并发人为串行化，降低吞吐；因此网页 Provider 不应默认继承 500 ms 的全局请求间隔。

请求间隔只适合用于控制面请求或错误退避：`precreate`、`locateupload`、`rapidupload`、`create`、目录查询，以及百度返回频控/临时错误后的重试。重试应使用可取消的指数退避和少量随机抖动，并优先遵守响应中的 `Retry-After`；正常分片请求不应因为固定间隔而等待。

## 10. 尚未确认、暂不实现

- 网页端 rapidupload 对 `skip`、`fail`、`replace` 三种冲突策略的完整参数映射；
- 是否能通过网页端参数完全禁止自动重命名；
- 主应用使用 Cookie 网页 Provider 的授权、刷新和安全保存方式；
- 普通分片回退时的断点续传和已存在分片恢复；
- `rapidupload` 失败后的临时上传会话清理；
- 主应用的进度模型如何同时表示 MD5 计算和网络上传进度；
- 是否需要对大文件的 MD5 计算和分片上传增加取消、暂停、重试调度。
- 是否要在并发 worker 启动时严格保留官方的 1 秒错峰，还是仅保留 3 路并发语义。

在上述事项确认前，不修改主应用的百度上传流程。
