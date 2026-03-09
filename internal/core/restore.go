package core

import (
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

func (e Engine) MapBackupKey(ctx context.Context, volumeName string, keyPrefix string) (string, error) {
	// 1. 如果 pos 是完整的 .tar.gz 路径，直接原样返回
	if strings.HasSuffix(keyPrefix, ".tar.gz") {
		return keyPrefix, nil
	}

	// 2. 准备从 S3 拉取列表
	searchKey := volumeName + "/" + keyPrefix
	objKeys, err := e.Storage.ListObjectKeysWithPrefix(ctx, Config.BackupBucketName, searchKey)
	if err != nil {
		return "", err
	}

	if len(objKeys) == 0 {
		return "", fmt.Errorf("在远程仓库中未找到卷 %s 的任何备份 (searchKey=%s)", volumeName, searchKey)
	}

	// 3. 对结果进行逆序排序
	extractDate := func(s string) string {
		lastUnderscore := strings.LastIndex(s, "_")
		lastDot := strings.LastIndex(s, ".tar.gz")
		if lastUnderscore == -1 || lastDot == -1 || lastUnderscore >= lastDot {
			return ""
		}
		return s[lastUnderscore+1 : lastDot]
	}

	sort.Slice(objKeys, func(i, j int) bool {
		return extractDate(objKeys[i]) > extractDate(objKeys[j])
	})

	// 4.未指定备份前缀，默认选择最新
	if keyPrefix == "" {
		e.Logger.Warning("未指定备份点，自动选择最新备份: " + objKeys[0])
		return objKeys[0], nil
	}

	// 5. 如果 prefix 是 daily/weekly/monthly，过滤出该类型中最新的一份
	for _, objKey := range objKeys {
		if strings.HasPrefix(objKey, searchKey) {
			return objKey, nil
		}
	}

	return "", fmt.Errorf("未找到符合条件 %s 的备份文件(searchKey=%s)", keyPrefix, searchKey)
}

func (e Engine) RestoreVolume(ctx context.Context, volumeName, key string) error {
	e.Logger.Info(fmt.Sprintf("正在从 s3://%s/%s 获取数据...", Config.BackupBucketName, key))
	objReader, err := e.Storage.GetObjectStream(ctx, Config.BackupBucketName, key)
	if err != nil {
		return err
	}

	gzReader, err := gzip.NewReader(objReader)
	if err != nil {
		return fmt.Errorf("初始化 Gzip 解压器失败: %w", err)
	}
	defer gzReader.Close()

	if VolumeExists(ctx, volumeName) {
		confirm, err := e.UI.Confirm(fmt.Sprintf("卷 %s 已存在，是否覆盖并重新导入？", volumeName))
		if err != nil {
			return err
		}

		if !confirm {
			e.Logger.Info("操作已取消")
			return nil
		}

		// 如果用户确认覆盖，先删除旧卷
		e.Logger.Info(fmt.Sprintf("正在清理旧卷 [%s]...", volumeName))
		_ = exec.CommandContext(ctx, "podman", "volume", "rm", "-f", volumeName).Run()
	}

	e.Logger.Info(fmt.Sprintf("正在创建卷 [%s]...", volumeName))
	if err := exec.CommandContext(ctx, "podman", "volume", "create", volumeName).Run(); err != nil {
		return fmt.Errorf("无法创建卷 %s", volumeName)
	}

	e.Logger.Info(fmt.Sprintf("正在注入数据到卷 [%s]...", volumeName))
	cmd := exec.CommandContext(ctx, "podman", "volume", "import", volumeName, "-")
	cmd.Stdin = gzReader
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("卷恢复失败: %w", err)
	}

	e.Logger.Success("卷恢复成功")
	return nil
}
