package core

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path"
	"time"

	"github.com/klauspost/compress/zstd"
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

	return fmt.Sprintf("%s/%s_%s.tar.zstd", volumeName, timestamp, backupType)
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

func (e Engine) backupVolume(ctx context.Context, volumeName string, allowOverride bool) error {
	if !VolumeExists(ctx, volumeName) {
		return fmt.Errorf("卷 %s 不存在", volumeName)
	}

	key := getBackupKey(volumeName, time.Now().UTC())

	keyExists, err := e.Storage.ObjectExists(ctx, Config.BackupBucketName, key)
	if err != nil {
		return fmt.Errorf("无法检测文件 [%s]:%s 存在性", Config.BackupBucketName, key)
	}
	if keyExists && !allowOverride {
		return fmt.Errorf("文件 [%s]:%s 已存在", Config.BackupBucketName, key)
	}

	// 内存管道逻辑
	pr, pw := io.Pipe()
	go func() {
		zw, _ := zstd.NewWriter(pw)

		cmd := exec.CommandContext(ctx, "podman", "volume", "export", volumeName)
		cmd.Stdout = zw

		err := cmd.Run()
		zw.Close()

		if err != nil {
			pw.CloseWithError(err)
		} else {
			pw.Close()
		}
	}()

	if err := e.Storage.UploadStream(ctx, Config.BackupBucketName, key, pr); err != nil {
		return err
	}

	return nil
}

func (e Engine) BackupAction(ctx context.Context, volumeNamePattern string, allowOverride bool, dryRun bool) {
	allVolumeNames := GetAllVolumeNames()
	matchedVolumeNames := filterVolumeNames(allVolumeNames, volumeNamePattern)

	if len(matchedVolumeNames) == 0 {
		e.Logger.Error(fmt.Sprintf("未找到匹配的卷: %s", volumeNamePattern))
		return
	}

	for _, v := range matchedVolumeNames {
		if dryRun {
			e.Logger.Info(fmt.Sprintf("[DryRun] 备份卷：%s", v))
			continue
		}

		e.Logger.Info(fmt.Sprintf("备份卷：%s", v))
		if err := e.backupVolume(ctx, v, allowOverride); err != nil {
			e.Logger.Error(fmt.Sprintf("卷 %s 备份时发生错误: %v", v, err))
		}

		e.Logger.Success(fmt.Sprintf("卷 %s 备份成功", v))
	}
}
