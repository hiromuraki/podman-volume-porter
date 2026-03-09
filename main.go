package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"podman-volumes-porter/internal/core"
	"time"
)

func printUsage() {
	fmt.Println("用法: pvp <command> [options]")
	fmt.Println("\n命令:")
	fmt.Println("  backup <volume_name> [--allow-override] 备份指定的 Podman 卷")
	fmt.Println("  restore <volume_name> [--from key] 恢复指定的 Podman 卷")
	fmt.Println("\n环境变量:")
	fmt.Println("  S3_ENDPOINT_URL     S3 兼容存储地址 (必填)")
	fmt.Println("  S3_ACCESS_KEY       S3 Access Key (必填)")
	fmt.Println("  S3_SECRET_KEY       S3 Secret Key (必填)")
	fmt.Println("  BACKUP_BUCKET_NAME  存储桶名称 (默认: container-volume)")
	fmt.Println("  TASK_TIMEOUT        人物最大执行时间（秒） (默认: 7200)")
}

func main() {
	core.LoadConfig()
	engine := core.Engine{
		Logger: core.ConsoleLogger{},
		UI:     core.ConsoleUI{},
		Storage: core.S3Storage{
			EndpointUrl: core.GetEnv("S3_ENDPOINT_URL", ""),
			AccessKey:   core.GetEnv("S3_ACCESS_KEY", ""),
			SecretKey:   core.GetEnv("S3_SECRET_KEY", ""),
		},
	}

	// 环境变量检查
	if engine.Storage.EndpointUrl == "" || engine.Storage.AccessKey == "" || engine.Storage.SecretKey == "" {
		engine.Logger.Error("缺少必要环境变量：S3_ENDPOINT，S3_ACCESS_KEY，S3_SECRET_KEY")
		os.Exit(1)
	}

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// 配置任务上下文，单次任务至多运行两小时
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	// 创建两个子命令的 FlagSet
	backupCmd := flag.NewFlagSet("backup", flag.ExitOnError)
	backupAllowOverride := backupCmd.Bool("allow-override", false, "备份数据存在时是否强制覆盖")
	restoreCmd := flag.NewFlagSet("restore", flag.ExitOnError)
	restoreFrom := restoreCmd.String("from", "", "指定恢复的备份前缀 (例如: daily_20260309)")

	if !engine.Storage.IsAvailable(ctx) {
		engine.Logger.Error("无法连接到 S3 存储，请检查网络或环境变量")
		os.Exit(1)
	}

	switch os.Args[1] {
	// 备份操作
	case "backup":
		backupCmd.Parse(os.Args[2:])
		volumeName := backupCmd.Arg(0)

		if volumeName == "" {
			engine.Logger.Error("缺少 volume_name 参数。\n用法: pvp backup <volume_name>")
			os.Exit(1)
		}
		print(*backupAllowOverride)
		err := engine.BackupVolume(ctx, volumeName, *backupAllowOverride)
		if err != nil {
			engine.Logger.Error(err.Error())
			os.Exit(1)
		}

	// 恢复操作
	case "restore":
		restoreCmd.Parse(os.Args[2:])
		volumeName := restoreCmd.Arg(0)

		if volumeName == "" {
			engine.Logger.Error("缺少 volume_name 参数。\n用法: pvp restore <volume_name> [--from key_prefix]")
			os.Exit(1)
		}

		key, err := engine.MapBackupKey(ctx, volumeName, *restoreFrom)
		if err != nil {
			engine.Logger.Error(err.Error())
			os.Exit(1)
		}
		err = engine.RestoreVolume(ctx, volumeName, key)
		if err != nil {
			engine.Logger.Error(err.Error())
			os.Exit(1)
		}

	default:
		engine.Logger.Error(fmt.Sprintf("未知命令: %s", os.Args[1]))
		printUsage()
		os.Exit(1)
	}
}
