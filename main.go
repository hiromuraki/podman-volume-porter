package main

import (
	"context"
	"fmt"
	"os"
	"podman-volumes-porter/internal/core"
	"time"

	"github.com/spf13/cobra"
)

var (
	dryRun        bool
	allowOverride bool
	restoreFrom   string
	engine        core.Engine
)

var rootCmd = &cobra.Command{
	Use:   "pvp",
	Short: "Podman Volumes Porter - 像搬运工一样管理你的 Podman 卷",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		setupEngine()
	},
}

var backupCmd = &cobra.Command{
	Use:   "backup <volumeNamePattern>",
	Short: "备份指定的 Podman 卷至 S3，支持通配符",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(core.Config.TaskTimeout)*time.Hour)
		defer cancel()

		pattern := args[0]
		matchedVolumes := core.GetMatchedVolumeNames(pattern)

		if len(matchedVolumes) == 0 {
			engine.Logger.Error(fmt.Sprintf("未找到匹配的卷: %s", pattern))
			return
		}

		for _, v := range matchedVolumes {
			if dryRun {
				engine.Logger.Info(fmt.Sprintf("[DryRun] 备份卷：%s", v))
				continue
			}

			engine.Logger.Info(fmt.Sprintf("[DryRun] 备份卷：%s", v))
			if err := engine.BackupVolume(ctx, v, allowOverride); err != nil {
				engine.Logger.Error(fmt.Sprintf("卷 %s 备份失败: %v", v, err))
			}
		}
	},
}

// restoreCmd 对应 restore 命令
var restoreCmd = &cobra.Command{
	Use:   "restore <volume_name>",
	Short: "从 S3 恢复指定的 Podman 卷",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(core.Config.TaskTimeout)*time.Hour)
		defer cancel()

		volumeName := args[0]
		key, err := engine.GetMatchedBackupKey(ctx, volumeName, restoreFrom)
		if err != nil {
			engine.Logger.Error(err.Error())
			return
		}

		if dryRun {
			engine.Logger.Info(fmt.Sprintf("[DryRun] 将恢复卷：%s (源文件=%s)", volumeName, key))
			return
		}

		if err := engine.RestoreVolume(ctx, volumeName, key); err != nil {
			engine.Logger.Error(fmt.Sprintf("恢复失败: %v", err))
		}
	},
}

func init() {
	// 全局 Flag
	rootCmd.PersistentFlags().BoolVarP(&dryRun, "dry-run", "d", false, "仅预览执行，不改变远程数据")

	// 子命令特有 Flag
	backupCmd.Flags().BoolVar(&allowOverride, "allow-override", false, "备份数据存在时是否强制覆盖")
	restoreCmd.Flags().StringVar(&restoreFrom, "from", "", "指定恢复的备份前缀 (例如: daily_20260309)")

	// 将子命令添加到根命令
	rootCmd.AddCommand(backupCmd)
	rootCmd.AddCommand(restoreCmd)
}

func setupEngine() {
	core.LoadConfig()
	engine = core.Engine{
		Logger: core.ConsoleLogger{},
		UI:     core.ConsoleUI{},
		Storage: core.S3Storage{
			EndpointUrl: core.GetEnv("S3_ENDPOINT_URL", ""),
			AccessKey:   core.GetEnv("S3_ACCESS_KEY", ""),
			SecretKey:   core.GetEnv("S3_SECRET_KEY", ""),
		},
	}

	// 环境检查
	if engine.Storage.EndpointUrl == "" || engine.Storage.AccessKey == "" || engine.Storage.SecretKey == "" {
		fmt.Println("❌ 错误: 缺少必要环境变量 (S3_ENDPOINT_URL, S3_ACCESS_KEY, S3_SECRET_KEY)")
		os.Exit(1)
	}

	// 连通性预检
	if !engine.Storage.IsAvailable(context.Background()) {
		fmt.Println("❌ 错误: 无法连接至 S3 存储，请检查网络设置")
		os.Exit(1)
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
