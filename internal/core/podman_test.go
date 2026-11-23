package core

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// withFakePodman 在 PATH 最前面放一个假的 podman 可执行文件。
func withFakePodman(t *testing.T, script string) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "podman")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("写入假 podman 失败: %v", err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestVolumeExists(t *testing.T) {
	withFakePodman(t, "#!/bin/sh\nexit \"${FAKE_PODMAN_EXIT:-0}\"\n")

	tests := []struct {
		name    string
		exit    string
		want    bool
		wantErr bool
	}{
		{name: "卷存在", exit: "0", want: true},
		{name: "卷不存在", exit: "1", want: false},
		{name: "podman 执行失败", exit: "125", want: false, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("FAKE_PODMAN_EXIT", tt.exit)

			got, err := podman.volumeExists(context.Background(), "test-vol")
			if (err != nil) != tt.wantErr {
				t.Fatalf("volumeExists() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("volumeExists() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetAllVolumeNames(t *testing.T) {
	tests := []struct {
		name    string
		script  string
		want    []string
		wantErr bool
	}{
		{
			name:   "解析卷列表并忽略空行",
			script: "#!/bin/sh\nprintf 'vol-a\\nvol-b\\n\\n'\n",
			want:   []string{"vol-a", "vol-b"},
		},
		{
			name:    "podman 执行失败",
			script:  "#!/bin/sh\necho 'boom' >&2\nexit 125\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withFakePodman(t, tt.script)

			got, err := podman.getAllVolumeNames(context.Background())
			if (err != nil) != tt.wantErr {
				t.Fatalf("getAllVolumeNames() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getAllVolumeNames() = %v, want %v", got, tt.want)
			}
		})
	}
}
