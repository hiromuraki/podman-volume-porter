# 代码评审修复记录

## 1. 元信息

| 项 | 值 |
| --- | --- |
| 评审记录 | [REVIEW.md](./REVIEW.md)（`2026-11-23T17-55-51+0800_9a51e2f`） |
| 被评审 commit | `9a51e2f23a1beea782f60194cfac2eecbcab8855` |
| 响应时间 | `2026-11-23T18:07:46+08:00`（Asia/Shanghai） |
| 修复范围 | F1-F5、F7-F9 已修复；F6、F11 补充文档；F10、F12 记录为后续项 |

## 2. 逐项响应

### F1 恢复在备份流消费完前删除旧卷（已修复）

- 位置：[internal/core/restore.go:126](../../internal/core/restore.go#L126)
- 方案：旧卷存在时先导入临时卷 `<卷名>-restore-<纳秒>`；只有整条备份流成功写入临时卷后才删除旧卷，随后用 `podman volume export | podman volume import`（新增 [copyVolume](../../internal/core/podman.go#L44)）把临时卷内容复制到目标卷。导入失败时旧卷不受影响并清理临时卷；替换阶段失败时保留临时卷，并在错误信息中给出手工恢复命令。临时卷清理使用与任务 ctx 解耦的 30s context（[restore.go:116](../../internal/core/restore.go#L116)）。
- 验证：新增 `TestRestoreReplacesOldVolumeAfterSurvivingImport`（断言 `volume rm -f vol` 出现在 `volume import vol-restore-…` 之后）与 `TestRestoreKeepsOldVolumeWhenImportFails`（导入失败时旧卷未被删除、临时卷被清理），使用 httptest 假 S3 与假 podman 脚本驱动真实调用顺序。

### F2 压缩后超过约 78 GiB 的卷无法备份（已修复）

- 位置：[internal/core/storage.go:19](../../internal/core/storage.go#L19)、[storage.go:142](../../internal/core/storage.go#L142)
- 方案：transfermanager 显式设置 `PartSizeBytes=64 MiB`、`Concurrency=3`，单对象上限由约 78 GiB 提升到约 625 GiB，内存峰值约 256 MiB。README 同步容量与内存说明。
- 验证：代码级确认；未做真实大对象上传（无 S3 服务）。

### F3 启动预检无超时（已修复）

- 位置：[main.go:85](../../main.go#L85)、[main.go:116](../../main.go#L116)
- 方案：连通性预检使用 30s 超时的 context，端点无响应时返回错误而不是无限阻塞。
- 验证：代码级确认；未构造黑洞端点。

### F4 备份类型与时间戳时区不一致（已修复，保留键格式）

- 位置：[internal/core/restore.go:38](../../internal/core/restore.go#L38)、[restore.go:53](../../internal/core/restore.go#L53)
- 方案：键内时间戳保持 UTC（兼容已有备份与对象布局），`--from` 改为按本地时间前缀匹配：解析键内 UTC 时间戳后转换到 `time.Local` 再与用户前缀比较。README 明确"时间戳为 UTC，类型按本地时区判定，`--from` 按本地时间匹配"。
- 保留项：键中日期（UTC）与类型标签（本地日历）在跨日时刻看起来不一致，这是保持存储兼容的取舍；若要求键内也写本地时间需要新键格式与迁移，另行讨论。
- 验证：`TestLocalTimestamp` 覆盖 UTC 前一日到本地次日的换算；`TestSelectKeyByLocalPrefix` 覆盖按月、按日、按时刻匹配与无匹配。

### F5 `--from` 完整对象键绕过卷名校验（已修复）

- 位置：[internal/core/restore.go:64](../../internal/core/restore.go#L64)
- 方案：完整键必须通过 `filterObjectKeys`（属于该卷且符合时间戳与后缀格式），否则报错。
- 验证：`TestFindBestMatchedKeyRejectsForeignKey`。

### F6 恢复丢失卷的 driver/options/labels（部分修复：文档）

- 位置：[README.md](../../README.md#L100)
- 方案：明确"只恢复卷数据，不恢复 driver/options/labels"。
- 未实现配置透传：需要新增 CLI 选项并在删除前读取旧卷配置，属于功能变更，留待确认。

### F7 `backup --dry-run` 依赖 S3（已修复）

- 位置：[main.go:88](../../main.go#L88)
- 方案：`backup` 在 `--dry-run` 下只加载配置，跳过 S3 环境变量检查与连通性预检。
- 验证：手动执行 `backup "app-*" --exclude "*-cache" --dry-run`（无 `S3_*` 变量）退出 0 并输出清单。

### F8 dry-run 不显示被排除的卷（已修复）

- 位置：[internal/core/backup.go:154](../../internal/core/backup.go#L154)
- 方案：dry-run 输出"已排除 N 个卷"及卷名。
- 验证：`TestBackupActionDryRun` 断言排除信息；手动输出见 F7。

### F9 日志无条件输出 ANSI 颜色（已修复）

- 位置：[internal/core/common.go:32](../../internal/core/common.go#L32)
- 方案：仅在输出为终端且未设置 `NO_COLOR` 时着色；`NO_COLOR` 存在即禁用。
- 验证：`TestShouldColorize`；手动重定向到文件后输出无 `\033`。

### F10 每次 S3 操作重建 client（未修复）

- 原因：需要把 client/连接池生命周期提升到 Engine 初始化，并引入可注入的存储接口，与 F12 的结构性重构同批处理更合适；本轮不为单点优化引入半套抽象。

### F11 上传中断后残留分片（部分修复：文档）

- 位置：[README.md](../../README.md#L51)
- 方案：README 建议为存储桶配置 `AbortIncompleteMultipartUpload` 生命周期规则。代码层 abort 由 transfermanager 内部使用同一 ctx 执行，不改用自研上传实现无法解耦。
- 未实现：上传层替换。

### F12 Storage 为具体类型、核心路径无测试（部分修复）

- 已补：用 httptest 假 S3 与假 podman 覆盖恢复成功/失败时序（F1 的两个测试）。
- 未做：把 S3 收敛为接口并在 `Engine` 注入，属于结构性重构，建议单独一轮。

## 3. 构建与测试

环境 `go1.27.4 linux/amd64`，`GOCACHE=/home/user/repos/hm-tools/.tmp/gocache`、`GOMODCACHE=/home/user/repos/hm-tools/.tmp/gomodcache`。

| 命令 | 结果 |
| --- | --- |
| `go build ./...` | 通过 |
| `go vet ./...` | 通过 |
| `go test ./... -count=1` | 通过 |
| `go test -race ./... -count=1` | 通过 |
| `go test ./... -v` | 55 个 `RUN`（含子测试），无失败 |
| `pvp backup "app-*" --exclude "*-cache" --dry-run`（无 S3 变量） | 退出 0，匹配 1 个、排除 1 个，输出无 ANSI |

新增测试文件：[internal/core/common_test.go](../../internal/core/common_test.go)、[internal/core/restore_order_test.go](../../internal/core/restore_order_test.go)；其余为已有测试扩展。

## 4. 未自动验证

- 真实 podman 端到端（沙箱内无法建立 user namespace）。
- 真实 S3/MinIO/SeaweedFS 的上传、分片、abort 行为。
- F2 的大对象上传与 F3 的黑洞端点超时。
- 评审"待确认"项：checksum 兼容性、HeadObject 403、SELinux/xattr、备份一致性，均保持未验证。

## 5. 未修复项汇总

F10（client 生命周期）、F12（存储接口化）为结构性改动；F6（卷配置透传）、F11（上传层 abort）为功能/上传层改动。其余 F1-F5、F7-F9 已修复。下一轮评审可对未修项给出接受或拒绝结论。
