# 📦 Podman Volume Porter

像搬运工一样优雅地管理你的 Podman 卷。这是一个用 Go 编写的轻量级、无依赖、全流式的备份与恢复工具，专门为本地容器环境与 S3 兼容对象存储（如 SeaweedFS, MinIO, AWS S3）之间的数据流转而设计。

## ✨ 核心特性

* 🚀 **全流式处理 (Zero-Disk-Footprint)**：备份和恢复过程完全在内存管道中进行（Podman -> Tar Stream -> Zstd -> S3），不需要占用宿主机额外的磁盘空间；上传按 64 MiB 分片、并发 3，内存峰值约 256 MiB，单个备份对象上限约 625 GiB。
* ☁️ **原生 S3 兼容**：完美兼容标准的 S3 API，通过简单的环境变量即可接入任何对象存储。
* 🎯 **智能通配符匹配**：支持 Shell 风格的通配符（如 `podman-volume-porter backup *-data`），一键批量备份多个业务卷。
* 📅 **自动分级标记**：无需数据库记录，程序会自动根据当前时间戳判定备份性质，自动打上 `daily`、`weekly`（每周一）或 `monthly`（每月1号）的标签。
* 🛡️ **安全演练模式**：内置 `--dry-run` 选项，让你在执行真实的破坏性写入或上传前，随时预览即将发生的操作。
* 🪶 **极简部署**：编译后为单一静态二进制文件，无任何系统动态库依赖，开箱即用。

## 📥 安装指南

版本号采用“日期 + 当日构建序号”的形式（如 `2026.11.23-1`，日期取 UTC），每次推送 `main` 由 CI 自动构建并发布 Release。

**方式一：从 Release 下载 (推荐)**
直接从 GitHub Releases 页面下载针对你系统架构的编译产物：

```bash
curl -L -o /usr/local/bin/podman-volume-porter https://github.com/hiromuraki/podman-volume-porter/releases/latest/download/podman-volume-porter-linux-amd64
chmod +x /usr/local/bin/podman-volume-porter
```

**方式二：源码编译**
确保你已安装 Go 1.26 或更高版本，然后执行：

```bash
make build                        # 产物位于 ./dist/podman-volume-porter，版本号为 dev
make build VERSION=2026.11.23-1   # 指定版本号
```

运行需要 Podman 4.1 或更高版本（依赖 `podman volume exists`）。

## ⚙️ 环境配置

Podman Volume Porter 遵循云原生十二要素应用原则，所有配置均通过环境变量注入，程序本身不读取配置文件。运行前请确保环境中存在以下变量：

```bash
export S3_ENDPOINT_URL="http://localhost:8333"  # S3 API 地址
export S3_ACCESS_KEY="your_access_key"          # S3 Access Key
export S3_SECRET_KEY="your_secret_key"          # S3 Secret Key
export S3_REGION="cn-beijing"                   # （可选）S3 区域（默认 cn-beijing）
export S3_USE_PATH_STYLE="false"                # （可选）S3 是否使用路径模式（默认 false）
export S3_BUCKET_NAME="container-volume"        # （可选）存储桶名称 (默认 container-volume)
export TASK_TIMEOUT="7200"                      # （可选）单次任务超时秒数 (默认 7200)
```

systemd 单元默认从 `%h/.env.podman-volume-porter` 加载上述变量；在 shell 中手动运行时可先执行 `set -a; . "$HOME/.env.podman-volume-porter"; set +a` 导入。

> 建议为存储桶配置 `AbortIncompleteMultipartUpload` 生命周期规则，清理上传中断后残留的分片；`NO_COLOR=1` 可禁用日志颜色。

## 🚀 使用说明

使用 `podman-volume-porter --help` 可以随时查看动态帮助信息，使用 `podman-volume-porter --version` 查看当前版本。

### 1. 备份卷 (Backup)

备份单个或多个匹配的卷到 S3 存储。卷名支持 Shell 风格通配符：位置参数提供单个模式，`--include` 可重复提供多个模式（与位置参数取并集），`--exclude` 排除匹配的卷；同时命中包含与排除时以排除为准。

```bash
# 备份单个指定的卷
podman-volume-porter backup seaweed-config

# 使用通配符批量备份所有以 "-data" 结尾的卷
podman-volume-porter backup "*-data"

# 多个包含模式并排除缓存类卷
podman-volume-porter backup --include "app-*" --include "db-*" --exclude "*-cache"

# 演练模式：打印将备份的卷清单与数量，但不实际上传
podman-volume-porter backup "*-data" --dry-run

# 强制覆盖模式：如果远程已存在同名同时间的备份，强行覆盖
podman-volume-porter backup db-data --allow-override
```

*💡 提示：备份后的文件在 S3 中会自动命名为 `<卷名>/<时间戳>_<类型>.tar.zstd`，如 `mysql-data/20260309T152027Z_weekly.tar.zstd`。时间戳为 UTC，`daily`/`weekly`/`monthly` 类型按本地时区判定。*

### 2. 恢复卷 (Restore)

从 S3 存储中拉取备份流并覆盖本地指定的 Podman 卷。

```bash
# 自动恢复该卷时间最新的备份
podman-volume-porter restore mysql-data

# 精准恢复到某月的最新备份（2026年3月）
podman-volume-porter restore mysql-data --from 202603

# 精准恢复到某天的最新备份（2026年3月1日）
podman-volume-porter restore mysql-data --from 20260301

# 演练模式：查找匹配的文件，但不执行实际的覆盖
podman-volume-porter restore mysql-data --dry-run
```

*`--from` 按本地日期/时间前缀匹配（如 `202603`、`20260301`），也可以传完整对象键（须属于该卷）。*

*⚠️ 提示：恢复只恢复卷数据，不恢复卷的 driver/options/labels 配置；目标卷已存在时会先导入临时卷，成功后再替换旧卷，执行前请确认目标卷已停止写入。*

## 🤖 自动化建议 (Systemd Timer)

Podman Volume Porter 非常适合配合定时任务系统实现自动化无人值守备份。推荐使用 `systemd`，可参考以下文件实现：

* [podman-volume-porter.service](./deployments/systemd/podman-volume-porter.service)
* [podman-volume-porter.timer](./deployments/systemd/podman-volume-porter.timer)

部署方法：

```bash
# 1. 复制单元文件
mkdir -p ~/.config/systemd/user
cp deployments/systemd/podman-volume-porter.{service,timer} ~/.config/systemd/user/

# 2. 按实际安装路径与备份范围修改 service 中的 ExecStart
#    默认：/usr/local/bin/podman-volume-porter backup '*'

# 3. 加载并启动
systemctl --user daemon-reload
systemctl --user enable --now podman-volume-porter.timer
```

> 单元文件默认从 `~/.env.podman-volume-porter` 读取环境变量。
> 也可以使用 `systemctl --user start podman-volume-porter` 立即启动一次备份。

或者，也可以通过 `crontab` 来实现。添加如下配置：

```cron
# 每天凌晨 4:00 自动备份所有带有 '-data' 后缀的卷
0 4 * * * . "$HOME/.env.podman-volume-porter" && /usr/local/bin/podman-volume-porter backup "*-data" >> "$HOME/podman-volume-porter.log" 2>&1
```