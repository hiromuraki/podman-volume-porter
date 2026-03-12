# 📦 Podman Volumes Porter

像搬运工一样优雅地管理你的 Podman 卷。这是一个用 Go 编写的轻量级、无依赖、全流式的备份与恢复工具，专门为本地容器环境与 S3 兼容对象存储（如 SeaweedFS, MinIO, AWS S3）之间的数据流转而设计。

## ✨ 核心特性

* 🚀 **全流式处理 (Zero-Disk-Footprint)**：备份和恢复过程完全在内存管道中进行（Podman -> Tar Stream -> Zstd -> S3），无论你的卷有多大，都不需要占用宿主机额外的磁盘空间。
* ☁️ **原生 S3 兼容**：完美兼容标准的 S3 API，通过简单的环境变量即可接入任何对象存储。
* 🎯 **智能通配符匹配**：支持 Shell 风格的通配符（如 `podman-volumes-porter backup *-data`），一键批量备份多个业务卷。
* 📅 **自动分级保留策略**：无需数据库记录，程序会自动根据当前时间戳判定备份性质，自动打上 `daily`、`weekly`（每周一）或 `monthly`（每月1号）的标签。
* 🛡️ **安全演练模式**：内置 `--dry-run` 选项，让你在执行真实的破坏性写入或上传前，随时预览即将发生的操作。
* 🪶 **极简部署**：编译后为单一静态二进制文件，无任何系统动态库依赖，开箱即用。

## 📥 安装指南

**方式一：从 Release 下载 (推荐)**
直接从 GitHub Releases 页面下载针对你系统架构的编译产物：

```bash
curl -L -o /usr/local/bin/podman-volumes-porter https://github.com/hiromuraki/podman-volumes-porter/releases/latest/download/podman-volumes-porter
chmod +x /usr/local/bin/podman-volumes-porter
```

**方式二：源码编译**
确保你已安装 Go 1.26+，然后执行：

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o podman-volumes-porter main.go
```

## ⚙️ 环境配置

Podman Volumes Porter 遵循云原生十二要素应用原则，所有敏感配置均通过环境变量注入。运行前请确保环境中存在以下变量（推荐写入 `.bashrc` 或 `.env` 文件）：

```bash
export S3_ENDPOINT_URL="http://localhost:8333"  # S3 API 地址
export S3_ACCESS_KEY="your_access_key"          # S3 Access Key
export S3_SECRET_KEY="your_secret_key"          # S3 Secret Key
export S3_REGION="cn-beijing"                   # （可选）S3 区域（默认 cn-beijing）
export S3_USE_PATH_STYLE="false"                # （可选）S3 是否使用路径模式（默认 false）
export S3_BACKUP_BUCKET_NAME="container-volume" # （可选）存储桶名称 (默认 container-volume)
```

## 🚀 使用说明

使用 `podman-volumes-porter --help` 可以随时查看动态帮助信息。

### 1. 备份卷 (Backup)

备份单个或多个匹配的卷到 S3 存储。

```bash
# 备份单个指定的卷
podman-volumes-porter backup seaweed-config

# 使用通配符批量备份所有以 "-data" 结尾的卷
podman-volumes-porter backup "*-data"

# 演练模式：看看会备份哪些卷，但不实际上传
podman-volumes-porter backup "*-data" --dry-run

# 强制覆盖模式：如果远程已存在同名同时间的备份，强行覆盖
podman-volumes-porter backup db-data --allow-override
```

*💡 提示：备份后的文件在 S3 中会自动命名为 `<卷名>/<时间戳>_<类型>.tar.gz`。如 `mysql-data/20260309T152027Z_weekly.tar.gz`*

### 2. 恢复卷 (Restore)

从 S3 存储中拉取备份流并覆盖本地指定的 Podman 卷。

```bash
# 自动恢复该卷时间最新的备份
podman-volumes-porter restore mysql-data

# 精准恢复到某月的最新备份（2026年3月）
podman-volumes-porter restore mysql-data --from 202603

# 精准恢复到某天的最新备份（2026年3月1日）
podman-volumes-porter restore mysql-data --from 20260301

# 演练模式：查找匹配的文件，但不执行实际的覆盖
podman-volumes-porter restore mysql-data --dry-run
```

## 🤖 自动化建议 (Systemd Timer)

Podman Volumes Porter 非常适合配合定时任务系统实现自动化无人值守备份。推荐使用 `systemd`，可参考以下文件实现：

* [podman-volumes-porter.service](./deployments/systemd/podman-volumes-porter.service)
* [podman-volumes-porter.timer](./deployments/systemd/podman-volumes-porter.timer)

部署方法：

1. 将 `*.service` 与 `*.timer` 文件复制到 `~/.config/systemd/user`
2. 执行 `systemd --user daemon-reload`
3. 执行 `systemd --user enable --now podman-volumes-porter.timer`

也可以使用 `systemd --user start podman-volumes-porter` 立即启动一次备份。

或者，也可以通过 `crontab` 来实现。添加如下配置：

```cron
# 每天凌晨 4:00 自动备份所有带有 '-data' 后缀的卷
0 4 * * * source /etc/podman-volumes-porter.env && /usr/local/bin/podman-volumes-porter backup "*-data" >> /var/log/podman-volumes-porter_backup.log 2>&1
```
