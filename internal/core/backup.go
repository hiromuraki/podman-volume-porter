package core

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"
)

func getBackupKey(volumeName string) string {
	now := time.Now().UTC()

	var backupType string

	// 每月 1 号的备份视为月备份
	// 每周一的备份视为周备份
	// 默认是日常备份
	if now.Day() == 1 {
		backupType = "monthly"
	} else if now.Weekday() == time.Monday {
		backupType = "weekly"
	} else {
		backupType = "daily"
	}

	timestamp := time.Now().UTC().Format("20060102T150405Z")

	return fmt.Sprintf("%s/%s_%s.tar.gz", volumeName, backupType, timestamp)
}

func (e Engine) BackupVolume(ctx context.Context, volumeName string, forceOverride bool) error {
	key := getBackupKey(volumeName)

	keyExists, err := e.Storage.ObjectExists(ctx, e.Config.BackupBucketName, key)
	if err != nil {
		return fmt.Errorf("无法检测文件 [%s]:%s 存在性", e.Config.BackupBucketName, key)
	}
	if keyExists && !forceOverride {
		return fmt.Errorf("文件 [%s]:%s 已存在", e.Config.BackupBucketName, key)
	}

	// 内存管道逻辑
	pr, pw := io.Pipe()
	go func() {
		gw := gzip.NewWriter(pw)

		cmd := exec.CommandContext(ctx, "podman", "volume", "export", volumeName)
		cmd.Stdout = gw

		err := cmd.Run()
		gw.Close()

		if err != nil {
			pw.CloseWithError(err)
		} else {
			pw.Close()
		}
	}()

	e.Logger.Info(fmt.Sprintf("正在上传至 [%s]:%s", e.Config.BackupBucketName, key))
	if err := e.Storage.UploadStream(ctx, e.Config.BackupBucketName, key, pr); err != nil {
		return fmt.Errorf("传输失败: %w", err)
	}

	e.Logger.Success(fmt.Sprintf("卷 %s 备份成功", volumeName))
	return nil
}
