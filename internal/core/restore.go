package core

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

var backupTimestampRe = regexp.MustCompile(`(\d{8}T\d{6})Z`)

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
	sort.Slice(objKeys, func(i, j int) bool {
		return backupTimestampRe.FindString(objKeys[i]) > backupTimestampRe.FindString(objKeys[j])
	})
}

// localTimestamp 把键中的 UTC 时间戳转换到 loc 时区的紧凑格式，用于按本地日期匹配。
func localTimestamp(key string, loc *time.Location) (string, bool) {
	match := backupTimestampRe.FindStringSubmatch(key)
	if match == nil {
		return "", false
	}

	ts, err := time.ParseInLocation("20060102T150405", match[1], time.UTC)
	if err != nil {
		return "", false
	}

	return ts.In(loc).Format("20060102T150405"), true
}

// selectKeyByLocalPrefix 从已按时间倒序的键中选出本地时间前缀匹配的最新一项。
func selectKeyByLocalPrefix(sortedKeys []string, prefix string, loc *time.Location) (string, bool) {
	for _, key := range sortedKeys {
		local, ok := localTimestamp(key, loc)
		if ok && strings.HasPrefix(local, prefix) {
			return key, true
		}
	}

	return "", false
}

func (e Engine) findBestMatchedKey(ctx context.Context, volumeName string, keyPrefix string) (string, error) {
	// 完整对象键直接使用，但必须属于该卷
	if strings.HasSuffix(keyPrefix, ".tar.zstd") {
		if len(filterObjectKeys([]string{keyPrefix}, volumeName)) != 1 {
			return "", fmt.Errorf("备份文件 %s 不属于卷 %s", keyPrefix, volumeName)
		}
		return keyPrefix, nil
	}

	// 列出该卷的全部备份，再按本地日期/时间前缀筛选
	searchKey := volumeName + "/"
	objKeys, err := e.Storage.ListObjectKeysWithPrefix(ctx, searchKey)
	if err != nil {
		return "", err
	}

	filteredKeys := filterObjectKeys(objKeys, volumeName)
	if len(filteredKeys) == 0 {
		return "", fmt.Errorf("在远程仓库中未找到卷 %s 的任何备份 (searchKey=%s)", volumeName, searchKey)
	}

	sortObjectKeys(filteredKeys)

	if keyPrefix == "" {
		e.Logger.Warning("未指定备份点，自动选择最新备份: " + filteredKeys[0])
		return filteredKeys[0], nil
	}

	key, ok := selectKeyByLocalPrefix(filteredKeys, keyPrefix, time.Local)
	if !ok {
		return "", fmt.Errorf("未找到符合本地时间前缀 %s 的备份（--from 按本地时间匹配）", keyPrefix)
	}

	return key, nil
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

func (e Engine) cleanupVolume(ctx context.Context, volumeName string) {
	// 任务 ctx 可能已取消，清理使用独立的短超时 context
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	if err := podman.deleteVolume(cleanupCtx, volumeName); err != nil {
		e.Logger.Warning(fmt.Sprintf("临时卷 %s 清理失败，请手动删除: %v", volumeName, err))
	}
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

	e.Logger.Info(fmt.Sprintf("正在从 s3://%s/%s 获取数据...", e.Storage.BucketName, targetKey))
	body, decoder, err := e.getBackupStream(ctx, targetKey)
	if err != nil {
		return err
	}
	defer body.Close()
	defer decoder.Close()

	if !exists {
		e.Logger.Info(fmt.Sprintf("正在恢复卷 [%s]...", volumeName))
		if err := podman.importVolume(ctx, volumeName, decoder); err != nil {
			return fmt.Errorf("卷 %s 恢复失败（备份仍保留在 s3://%s/%s）: %w", volumeName, e.Storage.BucketName, targetKey, err)
		}

		e.Logger.Success(fmt.Sprintf("卷 %s 恢复成功", volumeName))
		return nil
	}

	// 旧卷存在时先导入临时卷，只有整条流成功落盘后才替换旧卷
	tmpVolume := fmt.Sprintf("%s-restore-%d", volumeName, time.Now().UnixNano())
	e.Logger.Info(fmt.Sprintf("正在恢复到临时卷 [%s]...", tmpVolume))
	if err := podman.importVolume(ctx, tmpVolume, decoder); err != nil {
		e.cleanupVolume(ctx, tmpVolume)
		return fmt.Errorf("卷 %s 恢复失败（旧卷未改动，备份仍保留在 s3://%s/%s）: %w", volumeName, e.Storage.BucketName, targetKey, err)
	}

	e.Logger.Info(fmt.Sprintf("正在用临时卷替换旧卷 %s...", volumeName))
	if err := podman.deleteVolume(ctx, volumeName); err != nil {
		return fmt.Errorf("临时卷 %s 已创建，但删除旧卷失败: %w", tmpVolume, err)
	}
	if err := podman.copyVolume(ctx, tmpVolume, volumeName); err != nil {
		return fmt.Errorf("旧卷 %s 已删除，完整数据保留在临时卷 %s（可执行 podman volume export %s | podman volume import %s - 手工恢复）: %w", volumeName, tmpVolume, tmpVolume, volumeName, err)
	}

	e.cleanupVolume(ctx, tmpVolume)

	e.Logger.Success(fmt.Sprintf("卷 %s 恢复成功", volumeName))
	return nil
}
