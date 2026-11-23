package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type Podman struct{}

var podman Podman

func (p Podman) volumeExists(ctx context.Context, volumeName string) (bool, error) {
	cmd := exec.CommandContext(ctx, "podman", "volume", "exists", volumeName)
	if err := cmd.Run(); err != nil {
		// podman volume exists 以退出码 1 表示卷不存在，其余错误需要上报
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("无法检测卷 %s 是否存在: %w", volumeName, err)
	}

	return true, nil
}

func (p Podman) importVolume(ctx context.Context, volumeName string, source io.Reader) error {
	if err := exec.CommandContext(ctx, "podman", "volume", "create", volumeName).Run(); err != nil {
		return fmt.Errorf("无法创建卷 %s: %w", volumeName, err)
	}

	cmd := exec.CommandContext(ctx, "podman", "volume", "import", volumeName, "-")
	cmd.Stdin = source
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// copyVolume 通过本地流把 src 卷内容复制到新建的 dst 卷。
func (p Podman) copyVolume(ctx context.Context, src string, dst string) error {
	if err := exec.CommandContext(ctx, "podman", "volume", "create", dst).Run(); err != nil {
		return fmt.Errorf("无法创建卷 %s: %w", dst, err)
	}

	exportCmd := exec.CommandContext(ctx, "podman", "volume", "export", src)
	stdout, err := exportCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("导出卷 %s 失败: %w", src, err)
	}
	exportCmd.Stderr = os.Stderr
	if err := exportCmd.Start(); err != nil {
		return fmt.Errorf("导出卷 %s 失败: %w", src, err)
	}

	importCmd := exec.CommandContext(ctx, "podman", "volume", "import", dst, "-")
	importCmd.Stdin = stdout
	importCmd.Stdout = os.Stdout
	importCmd.Stderr = os.Stderr
	importErr := importCmd.Run()
	exportErr := exportCmd.Wait()

	if importErr != nil {
		return fmt.Errorf("写入卷 %s 失败: %w", dst, importErr)
	}
	if exportErr != nil {
		return fmt.Errorf("导出卷 %s 失败: %w", src, exportErr)
	}

	return nil
}

func (p Podman) deleteVolume(ctx context.Context, volumeName string) error {
	if err := exec.CommandContext(ctx, "podman", "volume", "rm", "-f", volumeName).Run(); err != nil {
		return fmt.Errorf("删除卷 %s 失败: %w", volumeName, err)
	}

	return nil
}

func (p Podman) getAllVolumeNames(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "podman", "volume", "ls", "--format", "{{.Name}}")
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("获取 Podman 卷列表失败: %w: %s", err, strings.TrimSpace(errOut.String()))
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")

	var volumeNames []string
	for _, line := range lines {
		if name := strings.TrimSpace(line); name != "" {
			volumeNames = append(volumeNames, name)
		}
	}
	return volumeNames, nil
}
