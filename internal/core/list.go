package core

import (
	"context"
	"fmt"
	"strings"
)

func formatSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}

	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGTPE"[exp])
}

func (e Engine) ListAction(ctx context.Context, volumeName string) error {
	objects, err := e.Storage.ListObjects(ctx, volumeName+"/")
	if err != nil {
		return err
	}

	filtered := filterBackupObjects(objects, volumeName)
	if len(filtered) == 0 {
		e.Logger.Warning(fmt.Sprintf("卷 %s 在 s3://%s 上没有备份", volumeName, e.Storage.BucketName))
		return nil
	}

	sortBackupObjects(filtered)

	e.Logger.Info(fmt.Sprintf("卷 %s 共有 %d 份备份：", volumeName, len(filtered)))
	for _, obj := range filtered {
		name := strings.TrimPrefix(obj.Key, volumeName+"/")
		e.Logger.Info(fmt.Sprintf("  %s  %10s  %s", obj.LastModified.Local().Format("2006-01-02 15:04:05"), formatSize(obj.Size), name))
	}

	return nil
}
