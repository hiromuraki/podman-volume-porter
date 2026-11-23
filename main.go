package main

import (
	"context"
	"fmt"
	"os"
	"podman-volume-porter/internal/core"
	"time"

	"github.com/spf13/cobra"
)

// version 由构建流程通过 -ldflags "-X main.version=..." 注入。
var version = "dev"

var (
	dryRun          bool
	allowOverride   bool
	includePatterns []string
	excludePatterns []string
	restoreFrom     string
	engine          core.Engine
)

var rootCmd = &cobra.Command{
	Use:          "pvp",
	Short:        "Podman Volume Porter - 像搬运工一样管理你的 Podman 卷",
	Version:      version,
	SilenceUsage: true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		setupEngine(cmd)
	},
}

var backupCmd = &cobra.Command{
	Use:   "backup [volumeNamePattern]",
	Short: "备份指定的 Podman 卷至 S3，支持通配符",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			return fmt.Errorf("最多允许一个位置参数，更多包含模式请使用 --include")
		}
		if len(args) == 0 && len(includePatterns) == 0 {
			return fmt.Errorf("必须通过位置参数或 --include 指定至少一个卷名模式")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(core.Config.TaskTimeout)*time.Second)
		defer cancel()

		patterns := make([]string, 0, len(args)+len(includePatterns))
		patterns = append(patterns, args...)
		patterns = append(patterns, includePatterns...)

		return engine.BackupAction(ctx, patterns, excludePatterns, allowOverride, dryRun)
	},
}

var restoreCmd = &cobra.Command{
	Use:   "restore <volume_name>",
	Short: "从 S3 恢复指定的 Podman 卷",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(core.Config.TaskTimeout)*time.Second)
		defer cancel()

		return engine.RestoreAction(ctx, args[0], restoreFrom, dryRun)
	},
}

func init() {
	// 全局 Flag
	rootCmd.PersistentFlags().BoolVarP(&dryRun, "dry-run", "d", false, "仅预览执行，不改变远程数据")

	// 子命令特有 Flag
	backupCmd.Flags().BoolVar(&allowOverride, "allow-override", false, "备份数据存在时是否强制覆盖")
	backupCmd.Flags().StringArrayVar(&includePatterns, "include", nil, "包含匹配的卷名模式，可重复指定并与位置参数取并集")
	backupCmd.Flags().StringArrayVar(&excludePatterns, "exclude", nil, "排除匹配的卷名模式，可重复指定，优先于包含模式")
	restoreCmd.Flags().StringVar(&restoreFrom, "from", "", "指定恢复的备份前缀 (例如: 20260309)")

	rootCmd.AddCommand(backupCmd)
	rootCmd.AddCommand(restoreCmd)
}

// preflightTimeout 是启动时 S3 连通性预检的超时，避免端点无响应时无限阻塞。
const preflightTimeout = 30 * time.Second

func setupEngine(cmd *cobra.Command) {
	core.LoadConfig()
	engine = core.Engine{
		Logger: core.ConsoleLogger{},
		UI:     core.ConsoleUI{},
	}

	// backup --dry-run 只需要本地卷列表，不访问 S3
	if dryRun && cmd.Name() == "backup" {
		return
	}

	engine.Storage = core.S3Storage{
		EndpointUrl:  core.GetEnv("S3_ENDPOINT_URL", ""),
		AccessKey:    core.GetEnv("S3_ACCESS_KEY", ""),
		SecretKey:    core.GetEnv("S3_SECRET_KEY", ""),
		BucketName:   core.GetEnv("S3_BUCKET_NAME", "container-volume"),
		Region:       core.GetEnv("S3_REGION", "cn-beijing"),
		UsePathStyle: core.GetBoolEnv("S3_USE_PATH_STYLE", false),
	}

	// 环境检查
	if engine.Storage.EndpointUrl == "" || engine.Storage.AccessKey == "" || engine.Storage.SecretKey == "" {
		fmt.Fprintln(os.Stderr, "❌ 错误: 缺少必要环境变量 (S3_ENDPOINT_URL, S3_ACCESS_KEY, S3_SECRET_KEY)")
		os.Exit(1)
	}

	// 连通性预检
	ctx, cancel := context.WithTimeout(context.Background(), preflightTimeout)
	defer cancel()

	isS3Available, err := engine.Storage.IsAvailable(ctx)
	if !isS3Available {
		fmt.Fprintf(os.Stderr, "❌ 错误: 无法连接至 S3 存储. %s\n", err.Error())
		os.Exit(1)
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
