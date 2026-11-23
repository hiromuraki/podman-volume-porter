package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// getBackupKey 按 now 所在时区判定备份类型（月/周/日），时间戳统一以 UTC 记录。
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

	timestamp := now.UTC().Format("20060102T150405Z")

	return fmt.Sprintf("%s/%s_%s.tar.zstd", volumeName, timestamp, backupType)
}

// filterVolumeNames 先匹配任一包含模式，再剔除命中任一排除模式的卷。
// 同时命中包含与排除的卷以排除为准。
func filterVolumeNames(allVolumeNames []string, includePatterns []string, excludePatterns []string) (matched []string, excluded []string, err error) {
	if len(includePatterns) == 0 {
		return nil, nil, fmt.Errorf("至少需要一个包含模式")
	}

	for _, pattern := range includePatterns {
		if _, err := path.Match(pattern, ""); err != nil {
			return nil, nil, fmt.Errorf("包含模式 %q 无效: %w", pattern, err)
		}
	}
	for _, pattern := range excludePatterns {
		if _, err := path.Match(pattern, ""); err != nil {
			return nil, nil, fmt.Errorf("排除模式 %q 无效: %w", pattern, err)
		}
	}

	matched = make([]string, 0)
	for _, vol := range allVolumeNames {
		isIncluded := false
		for _, pattern := range includePatterns {
			if hit, _ := path.Match(pattern, vol); hit {
				isIncluded = true
				break
			}
		}
		if !isIncluded {
			continue
		}

		isExcluded := false
		for _, pattern := range excludePatterns {
			if hit, _ := path.Match(pattern, vol); hit {
				isExcluded = true
				break
			}
		}

		if isExcluded {
			excluded = append(excluded, vol)
			continue
		}

		matched = append(matched, vol)
	}

	return matched, excluded, nil
}

func (e Engine) backupVolume(ctx context.Context, volumeName string, allowOverride bool) error {
	exists, err := podman.volumeExists(ctx, volumeName)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("卷 %s 不存在", volumeName)
	}

	key := getBackupKey(volumeName, time.Now())

	keyExists, err := e.Storage.ObjectExists(ctx, key)
	if err != nil {
		return fmt.Errorf("无法检测文件 [%s]:%s 存在性: %w", e.Storage.BucketName, key, err)
	}
	if keyExists && !allowOverride {
		return fmt.Errorf("文件 [%s]:%s 已存在", e.Storage.BucketName, key)
	}

	// 内存管道逻辑
	pr, pw := io.Pipe()
	defer pr.Close()

	go func() {
		zw, err := zstd.NewWriter(pw)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("初始化 Zstd 压缩器失败: %w", err))
			return
		}

		cmd := exec.CommandContext(ctx, "podman", "volume", "export", volumeName)
		cmd.Stdout = zw

		if err := cmd.Run(); err != nil {
			zw.Close()
			pw.CloseWithError(fmt.Errorf("导出卷 %s 失败: %w", volumeName, err))
			return
		}

		if err := zw.Close(); err != nil {
			pw.CloseWithError(fmt.Errorf("结束 Zstd 压缩失败: %w", err))
			return
		}

		pw.Close()
	}()

	return e.Storage.UploadStream(ctx, key, pr)
}

func (e Engine) BackupAction(ctx context.Context, includePatterns []string, excludePatterns []string, allowOverride bool, dryRun bool) error {
	allVolumeNames, err := podman.getAllVolumeNames(ctx)
	if err != nil {
		return err
	}

	matchedVolumeNames, excludedVolumeNames, err := filterVolumeNames(allVolumeNames, includePatterns, excludePatterns)
	if err != nil {
		return err
	}

	if len(matchedVolumeNames) == 0 {
		if len(excludedVolumeNames) > 0 {
			return fmt.Errorf("匹配到的 %d 个卷均被 --exclude 排除: %s", len(excludedVolumeNames), strings.Join(excludedVolumeNames, ", "))
		}
		return fmt.Errorf("未找到匹配的卷: %s", strings.Join(includePatterns, ", "))
	}

	if dryRun {
		e.Logger.Info(fmt.Sprintf("[DryRun] 匹配到 %d 个卷：", len(matchedVolumeNames)))
		for _, v := range matchedVolumeNames {
			e.Logger.Info("  - " + v)
		}
		return nil
	}

	e.Logger.Info(fmt.Sprintf("开始备份 %d 个卷", len(matchedVolumeNames)))

	var errs []error
	for _, v := range matchedVolumeNames {
		e.Logger.Info(fmt.Sprintf("备份卷：%s", v))
		if err := e.backupVolume(ctx, v, allowOverride); err != nil {
			errs = append(errs, fmt.Errorf("卷 %s: %w", v, err))
			continue
		}

		e.Logger.Success(fmt.Sprintf("卷 %s 备份成功", v))
	}

	return errors.Join(errs...)
}
