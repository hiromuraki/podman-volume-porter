package core

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/klauspost/compress/zstd"
)

func filterObjectKeys(objKeys []string, volumeName string) []string {
	var filteredKeys []string

	pattern := fmt.Sprintf(`^%s/\d{8}T\d{6}Z.*\.tar\.zstd$`, regexp.QuoteMeta(volumeName))
	filterRe := regexp.MustCompile(pattern)
	for _, key := range objKeys {
		if filterRe.MatchString(key) {
			filteredKeys = append(filteredKeys, key)
		}
	}

	return filteredKeys
}

func sortObjectKeys(objKeys []string) {
	re := regexp.MustCompile(`(\d{8}T\d{6}Z)`)
	sort.Slice(objKeys, func(i, j int) bool {
		return re.FindString(objKeys[i]) > re.FindString(objKeys[j])
	})
}

func (e Engine) findBestMatchedKey(ctx context.Context, volumeName string, keyPrefix string) (string, error) {
	// 如果 keyPrefix 是完整的 .tar.zstd 路径，直接原样返回
	if strings.HasSuffix(keyPrefix, ".tar.zstd") {
		return keyPrefix, nil
	}

	// 否则，获取所有存储桶中所有符合前缀的键，并选出最新的一版作为目标对象
	searchKey := volumeName + "/" + keyPrefix
	objKeys, err := e.Storage.ListObjectKeysWithPrefix(ctx, searchKey)
	if err != nil {
		return "", err
	}

	filteredKeys := filterObjectKeys(objKeys, volumeName)
	if len(filteredKeys) == 0 {
		return "", fmt.Errorf("在远程仓库中未找到卷 %s 的任何备份 (searchKey=%s)", volumeName, searchKey)
	}

	// 列表接口已按 searchKey 过滤，按时间逆序排列后首个即最新一版
	sortObjectKeys(filteredKeys)
	if keyPrefix == "" {
		e.Logger.Warning("未指定备份点，自动选择最新备份: " + filteredKeys[0])
	}

	return filteredKeys[0], nil
}

// getBackupStream 打开备份对象并校验 Zstd 帧头，调用方负责关闭返回的流。
func (e Engine) getBackupStream(ctx context.Context, key string) (io.ReadCloser, *zstd.Decoder, error) {
	body, err := e.Storage.GetObjectStream(ctx, key)
	if err != nil {
		return nil, nil, err
	}

	decoder, err := zstd.NewReader(body)
	if err != nil {
		body.Close()
		return nil, nil, fmt.Errorf("备份 %s 不是有效的 Zstd 数据: %w", key, err)
	}

	return body, decoder, nil
}

func (e Engine) RestoreAction(ctx context.Context, volumeName string, restoreFrom string, dryRun bool) error {
	targetKey, err := e.findBestMatchedKey(ctx, volumeName, restoreFrom)
	if err != nil {
		return err
	}

	if dryRun {
		e.Logger.Info(fmt.Sprintf("[DryRun] 将恢复卷：%s (源文件=%s)", volumeName, targetKey))
		return nil
	}

	exists, err := podman.volumeExists(ctx, volumeName)
	if err != nil {
		return err
	}

	if exists {
		confirm, err := e.UI.Confirm(fmt.Sprintf("卷 %s 已存在，是否覆盖并重新导入？", volumeName))
		if err != nil {
			return err
		}
		if !confirm {
			e.Logger.Info("操作已取消")
			return nil
		}
	}

	// 先确认备份可读取且是有效的 Zstd 数据，再移除旧卷，避免无效备份导致数据丢失
	e.Logger.Info(fmt.Sprintf("正在从 s3://%s/%s 获取数据...", e.Storage.BucketName, targetKey))
	body, decoder, err := e.getBackupStream(ctx, targetKey)
	if err != nil {
		return err
	}
	defer body.Close()
	defer decoder.Close()

	if exists {
		e.Logger.Info(fmt.Sprintf("正在移除旧卷 %s...", volumeName))
		if err := podman.deleteVolume(ctx, volumeName); err != nil {
			return err
		}
	}

	e.Logger.Info(fmt.Sprintf("正在恢复卷 [%s]...", volumeName))
	if err := podman.importVolume(ctx, volumeName, decoder); err != nil {
		if exists {
			return fmt.Errorf("卷 %s 恢复失败（旧卷已移除，备份仍保留在 s3://%s/%s）: %w", volumeName, e.Storage.BucketName, targetKey, err)
		}
		return fmt.Errorf("卷 %s 恢复失败（备份仍保留在 s3://%s/%s）: %w", volumeName, e.Storage.BucketName, targetKey, err)
	}

	e.Logger.Success(fmt.Sprintf("卷 %s 恢复成功", volumeName))
	return nil
}
