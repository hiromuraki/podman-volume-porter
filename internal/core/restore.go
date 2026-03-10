package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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

func (e Engine) restoreVolume(ctx context.Context, volumeName string, key string) error {
	e.Logger.Info(fmt.Sprintf("正在从 s3://%s/%s 获取数据...", Config.BackupBucketName, key))
	objReader, err := e.Storage.GetObjectStream(ctx, Config.BackupBucketName, key)
	if err != nil {
		return err
	}

	zstdReader, err := zstd.NewReader(objReader)
	if err != nil {
		return fmt.Errorf("初始化 Zstd 解压器失败: %w", err)
	}
	defer zstdReader.Close()

	if VolumeExists(ctx, volumeName) {
		confirm, err := e.UI.Confirm(fmt.Sprintf("卷 %s 已存在，是否覆盖并重新导入？", volumeName))
		if err != nil {
			return err
		}

		if !confirm {
			e.Logger.Info("操作已取消")
			return nil
		}

		e.Logger.Info(fmt.Sprintf("正在清理旧卷 [%s]...", volumeName))
		_ = exec.CommandContext(ctx, "podman", "volume", "rm", "-f", volumeName).Run()
	}

	e.Logger.Info(fmt.Sprintf("正在创建卷 [%s]...", volumeName))
	if err := exec.CommandContext(ctx, "podman", "volume", "create", volumeName).Run(); err != nil {
		return fmt.Errorf("无法创建卷 %s", volumeName)
	}

	e.Logger.Info(fmt.Sprintf("正在注入数据到卷 [%s]...", volumeName))
	cmd := exec.CommandContext(ctx, "podman", "volume", "import", volumeName, "-")
	cmd.Stdin = zstdReader
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("卷恢复失败: %w", err)
	}

	e.Logger.Success("卷恢复成功")
	return nil
}

func (e Engine) RestoreAction(ctx context.Context, volumeName string, restoreFrom string, dryRun bool) error {
	findBestMatchedKey := func() (string, error) {
		keyPrefix := restoreFrom

		// 如果 keyPrefix 是完整的 .tar.zstd 路径，直接原样返回
		if strings.HasSuffix(keyPrefix, ".tar.zstd") {
			return keyPrefix, nil
		}

		// 否则，获取所有存储桶中所有符合前缀的键，并选出最新的一版作为目标对象
		searchKey := volumeName + "/" + keyPrefix
		objKeys, err := e.Storage.ListObjectKeysWithPrefix(ctx, Config.BackupBucketName, searchKey)
		if err != nil {
			return "", err
		}

		if len(objKeys) == 0 {
			return "", fmt.Errorf("在远程仓库中未找到卷 %s 的任何备份 (searchKey=%s)", volumeName, searchKey)
		}

		// 进行过滤与逆序排序，以保证按时间逆序排列
		filteredKeys := filterObjectKeys(objKeys, volumeName)
		sortObjectKeys(filteredKeys)

		// 未指定备份前缀，默认选择符合条件的最新一版
		if keyPrefix == "" {
			e.Logger.Warning("未指定备份点，自动选择最新备份: " + filteredKeys[0])
			return filteredKeys[0], nil
		}

		// 如果 prefix 非空，过滤出匹配前缀中最新的一份
		for _, objKey := range filteredKeys {
			if strings.HasPrefix(objKey, searchKey) {
				return objKey, nil
			}
		}

		return "", fmt.Errorf("未找到符合条件 %s 的备份文件(searchKey=%s)", keyPrefix, searchKey)
	}

	targetKey, err := findBestMatchedKey()
	if err != nil {
		return err
	}

	if dryRun {
		e.Logger.Info(fmt.Sprintf("[DryRun] 将恢复卷：%s (源文件=%s)", volumeName, targetKey))
		return nil
	}

	if err := e.restoreVolume(ctx, volumeName, targetKey); err != nil {
		e.Logger.Error(fmt.Sprintf("恢复失败: %v", err))
	}

	return nil
}
