//go:build ignore

// 评审探针：验证恢复流程在下载流中途失败时旧卷已被删除，以及 dry-run/参数校验行为。
// 运行方式见 REVIEW.md 的“验证命令”一节。
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

const objectKey = "vol/20260309T152027Z_daily.tar.zstd"

func main() {
	mode := flag.String("mode", "restore", "restore | dryrun | header")
	bin := flag.String("bin", "", "pvp 二进制路径（restore/dryrun 模式需要）")
	flag.Parse()

	switch *mode {
	case "restore":
		runRestore(*bin)
	case "dryrun":
		runDryRun(*bin)
	case "header":
		runHeader()
	default:
		fmt.Printf("unknown mode %q\n", *mode)
		os.Exit(2)
	}
}

// runHeader 证明 zstd.NewReader 只校验帧头：截断的帧可以成功创建解码器，错误只在读取时暴露。
func runHeader() {
	full := zstdBytes(1 << 20)
	fmt.Printf("完整 zstd 流: %d 字节\n", len(full))
	for _, cut := range []int{4, 8, 16, 64, 1024} {
		if cut > len(full) {
			continue
		}
		dec, err := zstd.NewReader(bytes.NewReader(full[:cut]))
		if err != nil {
			fmt.Printf("截断到 %5d 字节: NewReader 失败: %v\n", cut, err)
			continue
		}
		n, rerr := io.Copy(io.Discard, dec)
		dec.Close()
		fmt.Printf("截断到 %5d 字节: NewReader 成功, 解码 %d 字节后读取错误: %v\n", cut, n, rerr)
	}
}

func runRestore(bin string) {
	if bin == "" {
		fmt.Println("缺少 -bin")
		os.Exit(2)
	}
	full := zstdBytes(1 << 20)

	dir, err := os.MkdirTemp("", "pvp-probe-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	logPath := filepath.Join(dir, "podman.log")
	script := "#!/bin/sh\n" +
		"echo \"$@\" >> \"$PVP_PODMAN_LOG\"\n" +
		"case \"$1 $2\" in\n" +
		"  \"volume exists\") exit 0 ;;\n" +
		"  \"volume rm\") exit 0 ;;\n" +
		"  \"volume create\") exit 0 ;;\n" +
		"  \"volume import\") cat >/dev/null 2>&1; exit 1 ;;\n" +
		"esac\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "podman"), []byte(script), 0o755); err != nil {
		panic(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("[s3] %s %s\n", r.Method, r.URL.RequestURI())
		if r.Method == http.MethodHead && r.URL.Path == "/container-volume" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Query().Get("list-type") == "2" {
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintf(w, listXML, objectKey, len(full))
			return
		}
		if r.URL.Path == "/container-volume/"+objectKey {
			// 声明完整长度，只发送一半后中断连接，模拟下载中途断流。
			w.Header().Set("Content-Length", strconv.Itoa(len(full)))
			w.WriteHeader(http.StatusOK)
			w.Write(full[:len(full)/2])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			panic(http.ErrAbortHandler)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "restore", "vol")
	cmd.Stdin = strings.NewReader("y\n")
	cmd.Env = probeEnv(dir, srv.URL, logPath, true)
	out, runErr := cmd.CombinedOutput()
	logText, _ := os.ReadFile(logPath)

	fmt.Printf("\n--- CLI 输出 ---\n%s\n", out)
	fmt.Printf("--- CLI 退出: %v\n", runErr)
	fmt.Printf("--- fake podman 调用顺序 ---\n%s\n", logText)
	rmAt, importAt := -1, -1
	for i, line := range strings.Split(strings.TrimSpace(string(logText)), "\n") {
		if strings.HasPrefix(line, "volume rm") {
			rmAt = i
		}
		if strings.HasPrefix(line, "volume import") {
			importAt = i
		}
	}
	fmt.Printf("旧卷删除发生在导入之前: %v (rm 行=%d, import 行=%d)\n", rmAt >= 0 && importAt >= 0 && rmAt < importAt, rmAt+1, importAt+1)
}

func runDryRun(bin string) {
	if bin == "" {
		fmt.Println("缺少 -bin")
		os.Exit(2)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead && r.URL.Path == "/container-volume" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dir, err := os.MkdirTemp("", "pvp-probe-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	logPath := filepath.Join(dir, "podman.log")
	script := "#!/bin/sh\n" +
		"echo \"$@\" >> \"$PVP_PODMAN_LOG\"\n" +
		"if [ \"$1 $2\" = \"volume ls\" ]; then\n" +
		"  printf 'app-data\\napp-cache\\ntmp-cache\\n'\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "podman"), []byte(script), 0o755); err != nil {
		panic(err)
	}

	cases := [][]string{
		{"backup", "--include", "app-*", "--exclude", "*-cache", "--dry-run"},
		{"backup", "app-*", "--exclude", "tmp-*", "--dry-run"},
		{"backup", "--dry-run"},
		{"backup", "a", "b"},
		{"backup", "*", "--exclude", "*", "--dry-run"},
		{"backup", "nginx*", "--dry-run"},
	}
	for _, args := range cases {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env = probeEnv(dir, srv.URL, logPath, true)
		out, runErr := cmd.CombinedOutput()
		cancel()
		fmt.Printf("\n=== pvp %s\n退出: %v\n%s", strings.Join(args, " "), runErr, out)
	}

	// 无 S3 环境变量时 backup --dry-run 的行为
	for _, args := range [][]string{{"backup", "*", "--dry-run"}, {"--version"}} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env = probeEnv(dir, "", logPath, false)
		out, runErr := cmd.CombinedOutput()
		cancel()
		fmt.Printf("\n=== pvp %s （无 S3 环境变量）\n退出: %v\n%s", strings.Join(args, " "), runErr, out)
	}
}

const listXML = `<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>container-volume</Name><Prefix>vol/</Prefix><KeyCount>1</KeyCount><MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated><Contents><Key>%s</Key><LastModified>2026-03-09T15:20:27.000Z</LastModified><ETag>"etag"</ETag><Size>%d</Size><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>`

func zstdBytes(n int) []byte {
	var buf bytes.Buffer
	enc, err := zstd.NewWriter(&buf)
	if err != nil {
		panic(err)
	}
	payload := make([]byte, n)
	for i := range payload {
		payload[i] = byte(i*31 + i/251)
	}
	if _, err := enc.Write(payload); err != nil {
		panic(err)
	}
	if err := enc.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func probeEnv(podmanDir, endpoint, logPath string, withS3 bool) []string {
	base := make([]string, 0, len(os.Environ())+8)
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "PATH=") || strings.HasPrefix(e, "S3_") || strings.HasPrefix(e, "HOME=") || strings.HasPrefix(e, "TASK_TIMEOUT=") {
			continue
		}
		base = append(base, e)
	}
	base = append(base,
		"PATH="+podmanDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"PVP_PODMAN_LOG="+logPath,
		"HOME="+podmanDir,
		"AWS_EC2_METADATA_DISABLED=true",
		"TASK_TIMEOUT=60",
	)
	if withS3 {
		base = append(base,
			"S3_ENDPOINT_URL="+endpoint,
			"S3_ACCESS_KEY=probe",
			"S3_SECRET_KEY=probe",
			"S3_USE_PATH_STYLE=true",
			"S3_BUCKET_NAME=container-volume",
		)
	}
	return base
}
