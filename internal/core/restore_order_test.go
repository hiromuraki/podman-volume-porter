package core

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

type alwaysConfirm struct{}

func (alwaysConfirm) Confirm(string) (bool, error) { return true, nil }

// newFakeS3 返回一个只实现 ListObjectsV2 与 GetObject 的假 S3 服务。
func newFakeS3(t *testing.T, key string, payload []byte) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("list-type") == "2" {
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
<Name>container-volume</Name><Prefix>vol/</Prefix><KeyCount>1</KeyCount>
<MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated>
<Contents><Key>%s</Key><LastModified>2026-02-28T20:00:00.000Z</LastModified>
<ETag>"abc"</ETag><Size>%d</Size><StorageClass>STANDARD</StorageClass></Contents>
</ListBucketResult>`, key, len(payload))
			return
		}

		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(payload)
	}))
}

// backupPayload 构造一份合法的 Zstd(tar) 备份内容。
func backupPayload(t *testing.T) []byte {
	t.Helper()

	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	content := []byte("hello")
	if err := tw.WriteHeader(&tar.Header{Name: "hello.txt", Mode: 0o644, Size: int64(len(content))}); err != nil {
		t.Fatalf("写入 tar 头失败: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("写入 tar 内容失败: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("关闭 tar 失败: %v", err)
	}

	var out bytes.Buffer
	zw, err := zstd.NewWriter(&out)
	if err != nil {
		t.Fatalf("初始化 zstd 失败: %v", err)
	}
	if _, err := zw.Write(tarBuf.Bytes()); err != nil {
		t.Fatalf("压缩失败: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 zstd 失败: %v", err)
	}

	return out.Bytes()
}

func fakePodmanScript() string {
	return `#!/bin/sh
printf '%s\n' "$*" >> "$PODMAN_LOG"
case "$1 $2" in
  "volume exists") exit 0 ;;
  "volume import")
    cat > /dev/null
    case "$3" in
      *-restore-*) [ "${PODMAN_FAIL_RESTORE:-0}" = "1" ] && exit 1 ;;
    esac
    exit 0 ;;
  "volume create") exit 0 ;;
  "volume export") printf 'tar-bytes'; exit 0 ;;
  "volume rm") exit 0 ;;
esac
exit 0
`
}

func newRestoreEngine(t *testing.T, serverURL string, logPath string) Engine {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "podman"), []byte(fakePodmanScript()), 0o755); err != nil {
		t.Fatalf("写入假 podman 失败: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PODMAN_LOG", logPath)

	return Engine{
		Logger: &recordingLogger{},
		UI:     alwaysConfirm{},
		Storage: S3Storage{
			EndpointUrl:  serverURL,
			AccessKey:    "test",
			SecretKey:    "test",
			BucketName:   "container-volume",
			Region:       "us-east-1",
			UsePathStyle: true,
		},
	}
}

func readCalls(t *testing.T, logPath string) []string {
	t.Helper()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("读取假 podman 调用记录失败: %v", err)
	}

	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func findCall(calls []string, prefix string) int {
	for i, call := range calls {
		if strings.HasPrefix(call, prefix) {
			return i
		}
	}
	return -1
}

func findExactCall(calls []string, want string) int {
	for i, call := range calls {
		if call == want {
			return i
		}
	}
	return -1
}

func TestRestoreReplacesOldVolumeAfterSurvivingImport(t *testing.T) {
	key := "vol/20260228T200000Z_monthly.tar.zstd"
	payload := backupPayload(t)
	server := newFakeS3(t, key, payload)
	defer server.Close()

	logPath := filepath.Join(t.TempDir(), "podman.log")
	engine := newRestoreEngine(t, server.URL, logPath)

	if err := engine.RestoreAction(context.Background(), "vol", "", false); err != nil {
		t.Fatalf("RestoreAction() error = %v", err)
	}

	calls := readCalls(t, logPath)
	importTemp := findCall(calls, "volume import vol-restore-")
	removeOld := findExactCall(calls, "volume rm -f vol")
	if importTemp < 0 || removeOld < 0 {
		t.Fatalf("缺少关键调用: %v", calls)
	}
	if removeOld < importTemp {
		t.Errorf("旧卷在临时卷导入完成前被删除: %v", calls)
	}
	if findCall(calls, "volume import vol -") < removeOld {
		t.Errorf("旧卷替换发生在导入之前: %v", calls)
	}
}

func TestRestoreKeepsOldVolumeWhenImportFails(t *testing.T) {
	key := "vol/20260228T200000Z_monthly.tar.zstd"
	payload := backupPayload(t)
	server := newFakeS3(t, key, payload)
	defer server.Close()

	logPath := filepath.Join(t.TempDir(), "podman.log")
	engine := newRestoreEngine(t, server.URL, logPath)
	t.Setenv("PODMAN_FAIL_RESTORE", "1")

	if err := engine.RestoreAction(context.Background(), "vol", "", false); err == nil {
		t.Fatal("RestoreAction() 期望失败，实际成功")
	}

	calls := readCalls(t, logPath)
	if findExactCall(calls, "volume rm -f vol") >= 0 {
		t.Errorf("导入失败后旧卷被删除: %v", calls)
	}
	if findCall(calls, "volume rm -f vol-restore-") < 0 {
		t.Errorf("导入失败后未清理临时卷: %v", calls)
	}
}
