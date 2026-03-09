package core

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path"
	"time"
)

func getBackupKey(volumeName string, now time.Time) string {
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

	timestamp := now.Format("20060102T150405Z")

	return fmt.Sprintf("%s/%s_%s.tar.gz", volumeName, timestamp, backupType)
}

func filterVolumeNames(allVolumeNames []string, namePattern string) []string {
	if len(allVolumeNames) == 0 {
		return []string{}
	}

	matched := make([]string, 0)
	for _, vol := range allVolumeNames {
		isMatch, err := path.Match(namePattern, vol)
		if err == nil && isMatch {
			matched = append(matched, vol)
		}
	}
	return matched
}

func GetMatchedVolumeNames(namePattern string) []string {
	allVolumeNames := GetAllVolumeNames()
	return filterVolumeNames(allVolumeNames, namePattern)
}

func (e Engine) BackupVolume(ctx context.Context, volumeName string, forceOverride bool) error {
	if !VolumeExists(ctx, volumeName) {
		return fmt.Errorf("卷 %s 不存在", volumeName)
	}

	key := getBackupKey(volumeName, time.Now().UTC())

	keyExists, err := e.Storage.ObjectExists(ctx, Config.BackupBucketName, key)
	if err != nil {
		return fmt.Errorf("无法检测文件 [%s]:%s 存在性", Config.BackupBucketName, key)
	}
	if keyExists && !forceOverride {
		return fmt.Errorf("文件 [%s]:%s 已存在", Config.BackupBucketName, key)
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

	e.Logger.Info(fmt.Sprintf("正在上传至 [%s]:%s", Config.BackupBucketName, key))
	if err := e.Storage.UploadStream(ctx, Config.BackupBucketName, key, pr); err != nil {
		return fmt.Errorf("传输失败: %w", err)
	}

	e.Logger.Success(fmt.Sprintf("卷 %s 备份成功", volumeName))
	return nil
}
