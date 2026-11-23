# Podman Volume Porter 代码评审

## 1. 元信息

| 项 | 值 |
| --- | --- |
| 完整 commit | `9a51e2f23a1beea782f60194cfac2eecbcab8855` |
| 分支 | `main` |
| 评审开始时间 | `2026-11-23T17:55:51+08:00`（Asia/Shanghai） |
| 评审对象 | 全项目 |
| 评审范围 | `15ec99d`、`6b9fab1`、`1181515`、`9a51e2f` 四个提交的改动；全部 Go 源码与测试；`README.md`、`Makefile`、CI workflow、systemd 单元 |
| 评审开始时工作区 | 干净（`git status --porcelain` 无输出） |
| 评审期间新增 | 仅 `code-reviews/2026-11-23T17-55-51+0800_9a51e2f/`（本文件与 `scripts/probe.go`），未修改生产代码或测试代码 |

历史评审：`git log -- code-reviews` 无输出，仓库没有 `code-reviews/` 目录，因此本轮针对当前 HEAD 做整项目评审。仓库没有 `DESIGN.md`，行为基准取 `README.md` 与实际代码对照。

结论：共 12 条 finding，其中 P2 4 条、P3 8 条，无 P0/P1。单卷备份、按卷名恢复的正常路径在代码层面成立；主要风险集中在恢复的时序（F1）、超大卷上传上限（F2）、启动预检无超时（F3）以及时区语义（F4）。

## 2. 构建、测试与探针

环境：`go1.27.4 linux/amd64`。所有 go 命令执行前设置 `GOCACHE=/home/user/repos/hm-tools/.tmp/gocache`、`GOMODCACHE=/home/user/repos/hm-tools/.tmp/gomodcache`。

| 命令 | 结果 |
| --- | --- |
| `go build ./...` | 通过（exit 0） |
| `go vet ./...` | 通过（exit 0） |
| `go test ./... -count=1` | 通过：`ok podman-volume-porter/internal/core` |
| `go test -race ./...` | 通过（1.021s） |
| `go mod tidy -diff` | 无差异（exit 0） |
| `go build -trimpath -ldflags="-s -w -X main.version=probe" -o /home/user/repos/hm-tools/.tmp/pvp-probe/pvp .` | 通过 |
| `go run code-reviews/2026-11-23T17-55-51+0800_9a51e2f/scripts/probe.go -mode=header` | 见 2.1 |
| `go run code-reviews/2026-11-23T17-55-51+0800_9a51e2f/scripts/probe.go -mode=restore -bin=/home/user/repos/hm-tools/.tmp/pvp-probe/pvp` | 见 2.2 |
| `go run code-reviews/2026-11-23T17-55-51+0800_9a51e2f/scripts/probe.go -mode=dryrun -bin=/home/user/repos/hm-tools/.tmp/pvp-probe/pvp` | 见 2.3 |
| `gh api repos/<action>/releases/latest`（checkout、setup-go、upload-artifact、action-gh-release） | v7.0.1 / v7.1.0 / v7.0.2 / v3.0.4，CI 引用的主版本均存在 |

### 2.1 zstd 帧头校验探针（`-mode=header`）

```text
完整 zstd 流: 1287 字节
截断到     4 字节: NewReader 成功, 解码 0 字节后读取错误: unexpected EOF
截断到     8 字节: NewReader 成功, 解码 0 字节后读取错误: unexpected EOF
截断到    16 字节: NewReader 成功, 解码 0 字节后读取错误: unexpected EOF
截断到    64 字节: NewReader 成功, 解码 0 字节后读取错误: unexpected EOF
截断到  1024 字节: NewReader 成功, 解码 0 字节后读取错误: unexpected EOF
```

### 2.2 恢复过程中下载中断探针（`-mode=restore`）

假 S3 声明完整 `Content-Length` 后只发送一半响应体并中断连接（模拟下载中断）；假 podman 的 `volume import` 读完全部输入后以退出码 1 结束（模拟 podman 对截断 tar 的失败）。

```text
[s3] HEAD /container-volume
[s3] GET /container-volume?list-type=2&prefix=vol%2F
[s3] GET /container-volume/vol/20260309T152027Z_daily.tar.zstd?x-id=GetObject
[INFO] 正在从 s3://container-volume/vol/20260309T152027Z_daily.tar.zstd 获取数据...
[INFO] 正在移除旧卷 vol...
[INFO] 正在恢复卷 [vol]...
Error: 卷 vol 恢复失败（旧卷已移除，备份仍保留在 s3://container-volume/vol/20260309T152027Z_daily.tar.zstd）: exit status 1
--- CLI 退出: exit status 1
--- fake podman 调用顺序 ---
volume exists vol
volume rm -f vol
volume create vol
volume import vol -
旧卷删除发生在导入之前: true (rm 行=2, import 行=4)
```

### 2.3 命令行行为探针（`-mode=dryrun`）

```text
=== pvp backup --include app-* --exclude *-cache --dry-run
退出: <nil>
[INFO] [DryRun] 匹配到 1 个卷：
[INFO]   - app-data

=== pvp backup app-* --exclude tmp-* --dry-run
退出: <nil>
[INFO] [DryRun] 匹配到 2 个卷：
[INFO]   - app-data
[INFO]   - app-cache

=== pvp backup --dry-run
退出: exit status 1
Error: 必须通过位置参数或 --include 指定至少一个卷名模式

=== pvp backup a b
退出: exit status 1
Error: 最多允许一个位置参数，更多包含模式请使用 --include

=== pvp backup * --exclude * --dry-run
退出: exit status 1
Error: 匹配到的 3 个卷均被 --exclude 排除: app-data, app-cache, tmp-cache

=== pvp backup nginx* --dry-run
退出: exit status 1
Error: 未找到匹配的卷: nginx*

=== pvp backup * --dry-run （无 S3 环境变量）
退出: exit status 1
❌ 错误: 缺少必要环境变量 (S3_ENDPOINT_URL, S3_ACCESS_KEY, S3_SECRET_KEY)

=== pvp --version （无 S3 环境变量）
退出: <nil>
pvp version probe
```

探针源码：[scripts/probe.go](scripts/probe.go)。

## 3. Findings

### [P2][F1] 恢复在备份流被完整消费前就删除旧卷，中途失败会丢失旧数据

- 位置：[internal/core/restore.go:105-127](../../internal/core/restore.go#L105-L127)；前置校验在 [restore.go:63-76](../../internal/core/restore.go#L63-L76)。
- 触发条件：备份对象可读且 zstd 帧头有效，但后续下载中断（网络抖动、连接重置）、对象尾部损坏，或 `podman volume import` 因 tar 内容错误失败。
- 实际结果：代码在 import 之前执行 `podman volume rm -f`（[restore.go:114-119](../../internal/core/restore.go#L114-L119)），随后 import 失败，返回「卷 vol 恢复失败（旧卷已移除…）」。旧卷数据已不可恢复，新建的空卷/半成品卷仍留在本地；README:97 的提示只覆盖「主动覆盖会删除旧卷」，没有说明下载中断同样会丢卷。
- 证据：2.2 的探针显示调用顺序为 `volume exists` → `volume rm -f` → `volume create` → `volume import`，CLI 退出 1；2.1 的探针显示即使只剩 4 字节，`zstd.NewReader` 仍然成功，错误只到读取时才出现，说明 [restore.go:105](../../internal/core/restore.go#L105) 注释里的「确认备份可读取且是有效的 Zstd 数据」实际只校验了帧头。
- 修复建议：把「删除旧卷」后移到确认整条流可完整读取之后；可行方案包括先完整解码校验再导入（代价是两倍流量）、先导入到临时卷 `<name>.restore-<ts>` 成功后再切换，或先落临时文件校验。至少应在 import 失败时删除半成品卷并明确提示「旧卷已删除」。

### [P2][F2] 压缩后超过约 78 GiB 的卷无法备份

- 位置：[internal/core/storage.go:132-150](../../internal/core/storage.go#L132-L150)（`transfermanager.New(s3Client)` 与 `UploadObject`）。
- 触发条件：单个卷经 zstd 压缩后的字节数超过约 78.1 GiB。
- 实际结果：`transfermanager@v0.1.8` 默认 `PartSizeBytes=8 MiB`、`MaxUploadParts=10000`；上传体是 `io.PipeReader`（不是 `io.Seeker`，也没有设置 `ContentLength`），上传器无法得知对象大小（`objectSize = -1`），不会自动上调分片大小。流达到 8 MiB 后即走分片路径，第 10001 片时报 `exceeded total allowed S3 limit MaxUploadParts (10000). Adjust PartSize to fit in this limit`，此时已传输约 78 GiB 后失败，该卷没有产生备份。README:7 的「无论你的卷有多大」不成立。
- 证据：`transfermanager@v0.1.8/api_client.go` 中 `defaultPartSizeBytes = 8 MiB`、`defaultMaxUploadParts = 10000`；`api_op_UploadObject.go` 的 `initSize` 只对 `io.Seeker` 或显式 `ContentLength` 计算大小；`Config.PartSizeBytes` 只能通过 `transfermanager.New` 的可变选项设置，而本仓库未传任何选项。
- 修复建议：通过 `transfermanager.New(client, func(o *transfermanager.Options) { o.PartSizeBytes = 64 << 20; o.Concurrency = ... })` 显式设定分片大小（64 MiB 时上限约 625 GiB，内存约为 `(Concurrency+1) × PartSize`，需同时调低并发），或在 README 中明确写出 78 GiB 上限并提示拆分卷。

### [P2][F3] 启动时的 S3 连通性预检没有超时，S3 端无响应会无限阻塞

- 位置：[main.go:106-111](../../main.go#L106-L111)（`IsAvailable(context.Background())`），实现见 [storage.go:45-56](../../internal/core/storage.go#L45-L56)；`TASK_TIMEOUT` 只在 [main.go:48](../../main.go#L48) 与 [main.go:64](../../main.go#L64) 的子命令里生效。
- 触发条件：`S3_ENDPOINT_URL` 指向的端点 TCP 可连接但迟迟不返回响应（黑洞、LB 挂起、服务假死）。
- 实际结果：aws-sdk-go-v2 默认 HTTP 客户端的 `Timeout` 为 0，只有 30s 拨号超时和 10s TLS 握手超时，没有响应头超时；`context.Background()` 没有 deadline，SDK 重试也不受 `TASK_TIMEOUT` 约束。`pvp` 会在加载配置阶段永久阻塞，任何子命令都不会执行。配合仓库自带的 oneshot unit（oneshot 默认没有启动超时），定时任务会一直停在 activating，后续 timer 触发不会启动新实例，备份会静默停止。
- 证据：`aws-sdk-go-v2@v1.41.3/aws/transport/http/client.go` 中 `clientTimeout` 默认零值、`http.Client{Timeout: b.clientTimeout}`，未设置 `ResponseHeaderTimeout`。
- 修复建议：预检使用独立的短超时 context（如 15–30s，或从 `TASK_TIMEOUT` 派生一个上限），并给 HTTP 客户端设置整体 `Timeout`；把预检错误原样返回给用户。

### [P2][F4] 备份类型的时区（本地）与对象键时间戳（UTC）不一致，`--from` 的按天/按月语义系统性偏移

- 位置：[internal/core/backup.go:17-33](../../internal/core/backup.go#L17-L33)（第 23-29 行用本地时间判定类型，第 31 行用 UTC 生成时间戳）；筛选逻辑见 [restore.go:42-43](../../internal/core/restore.go#L42-L43)；文档示例见 [README.md:87-91](../../README.md#L87-L91)。
- 触发条件：主机时区非 UTC（例如 Asia/Shanghai），且备份时刻落在本地 00:00–07:59；仓库自带 timer 的 04:00 正好命中。
- 实际结果：本地 2026-03-01 04:00（+08）的备份键是 `20260228T200000Z_monthly.tar.zstd`，`restore --from 202603` 的前缀 `vol/202603` 匹配不到该月度备份；同理本地 3 月 2 日 04:00 的日常备份键是 `20260301T200000Z_daily.tar.zstd`，README 的 `restore --from 20260302`（「精准恢复到某天」）实际会选到 3 月 3 日 04:00 的备份，整体偏移一天。备份类型标签与键中的日期也会互相矛盾。
- 证据：现有单测 [backup_test.go:178-183](../../internal/core/backup_test.go#L178-L183) 明确断言 `time.Date(2026, 3, 1, 4, 0, 0, 0, +08)` 得到 `20260228T200000Z_monthly.tar.zstd`，可据此直接推导前缀不匹配。
- 修复建议：统一时区语义。推荐让类型判定与时间戳使用同一时区（都用 UTC 判定类型，或都使用本地时间并在键中写入带偏移的时间戳），`--from` 的过滤按同一语义解析（例如解析键中的时间戳后按本地日历比较），并在 README 明确 `--from` 的时间基准。

### [P3][F5] `--from` 以 `.tar.zstd` 结尾时被当作完整对象键，绕过卷名前缀校验

- 位置：[internal/core/restore.go:35-39](../../internal/core/restore.go#L35-L39)。
- 触发条件：`pvp restore vol-a --from vol-b/20260309T152027Z_daily.tar.zstd`（或脚本拼接、手误）。
- 实际结果：该字符串被原样当作 S3 key 下载并导入到 `vol-a`，既不校验 key 是否属于 `vol-a`，也不经过 `filterObjectKeys` 的时间戳/后缀过滤。可以把别的卷的备份写进目标卷；传入不存在的键时只得到 S3 层的错误。该模式未在 README 中说明。
- 修复建议：要求完整键以 `volumeName + "/"` 开头并匹配 `^\d{8}T\d{6}Z`，或直接移除该旁路，让 `--from` 只接受前缀。

### [P3][F6] 恢复只按卷名重建，卷的 driver/options/labels 全部丢失

- 位置：[internal/core/podman.go:32-42](../../internal/core/podman.go#L32-L42)（`podman volume create <name>` 不带任何参数），配合 [restore.go:114-119](../../internal/core/restore.go#L114-L119) 先删除旧卷。
- 触发条件：原卷使用非默认 driver、`--opt`（如 device/type/o）、labels 或特定的 uid/gid 映射。
- 实际结果：恢复后的卷是默认配置的空卷，容器可能因挂载参数变化而启动失败；旧卷的配置随删除一起丢失，README 与帮助文本没有提示只恢复内容。
- 修复建议：至少在 README/命令输出中说明「只恢复数据，不恢复卷配置」；如需完整恢复，可增加 `--volume-opt`/`--label` 透传，或在删除前读取旧卷配置并复用。

### [P3][F7] `backup --dry-run` 仍要求 S3 凭据与连通性，无法离线预览

- 位置：[main.go:30-32](../../main.go#L30-L32)（所有子命令的 `PersistentPreRun` 都会调用 `setupEngine`）与 [main.go:100-111](../../main.go#L100-L111)。
- 触发条件：未设置 `S3_*` 或 S3 不可达时执行 `pvp backup '*' --dry-run`。
- 实际结果：退出码 1，提示「缺少必要环境变量」，无法预览卷清单（2.3 已复现）。而 `backup --dry-run` 本身只调用 `podman volume ls`，不需要 S3；README:11、70-71 把它描述为执行上传前的安全预览。
- 修复建议：把配置校验与连通性预检延迟到真正需要 S3 的路径（restore、非 dry-run 的 backup），或为 backup dry-run 跳过预检。

### [P3][F8] dry-run 不显示被 `--exclude` 排除的卷

- 位置：[internal/core/backup.go:147-160](../../internal/core/backup.go#L147-L160)；`excludedVolumeNames` 只在「全部被排除」时报错（第 148-150 行），dry-run 分支只打印 matched。
- 触发条件：`pvp backup 'app-*' --exclude 'tmp-*' --dry-run`。
- 实际结果：输出只列 2 个待备份卷，`tmp-cache` 被静默排除（2.3 已复现）。`--exclude` 写宽或写错时用户无从发现，与 README:70-71「预览即将发生的操作」不符。
- 修复建议：dry-run 同时打印排除数量与卷名（`filterVolumeNames` 已经返回该列表），或至少输出「已排除 N 个卷」。

### [P3][F9] 日志始终输出 ANSI 颜色，重定向到文件/日志后混入转义序列

- 位置：[internal/core/common.go:33-47](../../internal/core/common.go#L33-L47)（`ConsoleLogger` 无条件拼接颜色码）。
- 触发条件：按 README:128 的 crontab 示例把输出重定向到 `$HOME/podman-volume-porter.log`，或由 journal 收集。
- 实际结果：日志文件里出现 `\x1b[33m…\x1b[0m`（2.2/2.3 的探针输出需要去掉颜色码才能阅读），grep、日志平台与 `less` 显示混乱。
- 修复建议：仅在 stdout 是终端（或未设置 `NO_COLOR`）时着色，或提供 `--no-color`。

### [P3][F10] 每次 S3 操作都重新加载配置并新建 client 与连接池

- 位置：[internal/core/storage.go:27-43](../../internal/core/storage.go#L27-L43) 的 `getS3Client` 在 `IsAvailable`(45)、`ListObjectKeysWithPrefix`(58)、`ObjectExists`(88)、`GetObjectStream`(113)、`UploadStream`(132) 中各自调用。
- 触发条件：任意 `backup`（N 个卷 → N 次 `ObjectExists` + N 次上传）或 `restore`。
- 实际结果：每次操作都执行 `config.LoadDefaultConfig` + `s3.NewFromConfig`，生成新的 `http.Transport` 与连接池，无法复用 TCP/TLS 连接；N 个卷会产生 2N 套配置加载与连接，属于重复实现且放大了运行时开销。
- 修复建议：在 `setupEngine`/`Engine` 初始化时创建一次 client 并注入 `S3Storage`；顺带解决 `Engine.Storage` 不可替换的问题（见 F12）。

### [P3][F11] 上传超时/取消后 S3 会残留未完成的分片上传

- 位置：[internal/core/storage.go:142-148](../../internal/core/storage.go#L142-L148)（上传使用带 `TASK_TIMEOUT` 的 ctx）。
- 触发条件：大卷上传过程中任务 ctx 到期，或进程被 systemd 停止/杀死（`SIGKILL` 一定无法清理）。
- 实际结果：`transfermanager` 在失败时用同一个 ctx 调用 `AbortMultipartUpload`；ctx 已取消时 abort 请求立即失败，S3 中留下不可见的 incomplete multipart upload，持续占用存储，直到生命周期规则清理。工具与 README 均未提示这一点。
- 修复建议：把任务 ctx 与清理 ctx 分离（abort 用独立的短超时 context），并在 README 建议为存储桶配置 `AbortIncompleteMultipartUpload` 生命周期规则；`SIGKILL` 场景只能依赖该规则。

### [P3][F12] 核心上传/恢复路径没有可注入的存储层，测试只覆盖 dry-run

- 位置：[internal/core/common.go:18-22](../../internal/core/common.go#L18-L22)（`Engine.Storage` 是具体的 `S3Storage`），而 `Logger`/`UI` 是接口（第 24-29、49-51 行）；现有测试 [backup_test.go:214-241](../../internal/core/backup_test.go#L214-L241) 只覆盖 dry-run，完全不触碰 `Storage`。
- 触发条件：`backupVolume`、`getBackupStream`、`UploadStream`、`RestoreAction` 的任何回归。
- 实际结果：没有 S3 时无法测试上传/下载/恢复顺序，F1 这类时序缺陷没有任何测试保护；`Logger`/`UI` 已经有接口抽象，但真正需要替换（用于覆盖失败/时序路径）的 `Storage` 仍是具体类型，抽象方向与实际风险不匹配。
- 修复建议：把 S3 侧收敛为一个小接口（`ObjectExists`/`ListObjectKeysWithPrefix`/`GetObjectStream`/`UploadStream`）并在 `Engine` 注入，用 fake storage 覆盖「先校验后删除」的顺序与上传失败路径。

## 4. 待确认

1. **checksum 与 S3 兼容存储的兼容性**：`UploadStream` 指定了 `ChecksumAlgorithmSha256`（[storage.go:146](../../internal/core/storage.go#L146)）。对不可 seek 的 body，SDK 会使用 aws-chunked + trailer 校验头；README 声称兼容 SeaweedFS/MinIO 等实现，但部分版本可能不支持这类请求而拒绝上传。本环境没有 S3 服务，未验证，建议用 MinIO 做一次冒烟或把 checksum 算法做成可配置。
2. **HeadObject 的 404 判定**：`ObjectExists` 依赖 404 被映射为 `types.NotFound`（[storage.go:102-110](../../internal/core/storage.go#L102-L110)）。某些 S3 兼容实现或仅有 ListBucket 权限的账号会对不存在的键返回 403，此时备份会以「无法检测文件存在性」失败，而不是继续上传。未验证。
3. **`podman volume import` 对导出产物的兼容性**：SELinux 上下文、uid/gid、xattr 是否完整恢复未验证；沙箱内 podman 无法创建 user namespace，只能用假 podman 验证参数与调用顺序。
4. **备份一致性**：`podman volume export` 不停止写入方，运行中容器可能产生不一致快照。README 未承诺一致性，是否需要提示或提供停机/快照选项需与作者确认。

## 5. 未验证项

- 真实 podman 端到端：备份（export → zstd → S3）与恢复（S3 → zstd 解码 → import）在真实卷上的行为；沙箱内 `newuidmap` 权限不足，podman 无法建立 user namespace。
- 真实 S3/MinIO/SeaweedFS 端到端：签名、checksum、分片上传、abort、分页列表均只按 SDK 源码与 httptest 假服务推导，未对真实服务执行。
- CI 实际运行：只读核对了 workflow 引用的 Action 主版本存在（v7/v7.1/v7.0.2/v3.0.4）以及仓库已有 `2026.11.23-1..3` Release 记录，未触发 workflow。

## 6. 补充观察（未列为 finding）

- `systemd` 单元 `Restart=on-failure` + 秒级时间戳键：失败后 30s 重试会给已成功的卷再生成一份新时间戳的备份对象，重复占用存储；同时 5 分钟内最多重试 10 次。属于部署取舍，建议在 README 说明。
- `TASK_TIMEOUT` 取极大值（≥ 约 9.22e9 秒）时，`time.Duration(TaskTimeout) * time.Second` 会溢出为负，命令立即超时（[main.go:48](../../main.go#L48)）；属于异常输入，可在配置解析时加上限。
- `make build` 固定 `GOARCH=amd64`（[Makefile:8](../../Makefile#L8)），在 arm64 主机上按 README 方式二构建出的产物无法运行。
- `rootCmd.Use` 为 `pvp`，与二进制名 `podman-volume-porter` 不同，`--help` 中显示 `pvp`，无功能影响。
- 无匹配卷时 `backup` 返回错误（退出码 1）。对交互使用是合理的；配合自带 unit 的 `Restart=on-failure` 会重试 10 次。是否为预期行为需与作者确认。
