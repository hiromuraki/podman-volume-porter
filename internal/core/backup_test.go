package core

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFilterVolumeNames(t *testing.T) {
	// 准备模拟的卷名数据
	allVolumes := []string{
		"seaweed-config",
		"seaweed-data",
		"vaultwarden-vol",
		"mysql-db",
		"redis-cache",
		"test",
	}

	tests := []struct {
		name         string   // 测试用例描述
		includes     []string // 包含模式
		excludes     []string // 排除模式
		wantMatched  []string // 期望得到的结果
		wantExcluded []string // 期望被排除的卷
		wantErr      bool
	}{
		{
			name:        "全匹配通配符",
			includes:    []string{"*"},
			wantMatched: allVolumes,
		},
		{
			name:        "后缀匹配",
			includes:    []string{"*-data"},
			wantMatched: []string{"seaweed-data"},
		},
		{
			name:        "前缀匹配",
			includes:    []string{"seaweed-*"},
			wantMatched: []string{"seaweed-config", "seaweed-data"},
		},
		{
			name:        "中间包含匹配",
			includes:    []string{"*weed*"},
			wantMatched: []string{"seaweed-config", "seaweed-data"},
		},
		{
			name:        "精确匹配",
			includes:    []string{"test"},
			wantMatched: []string{"test"},
		},
		{
			name:        "无任何匹配",
			includes:    []string{"nginx*"},
			wantMatched: []string{},
		},
		{
			name:        "特殊字符匹配 (?)",
			includes:    []string{"tes?"},
			wantMatched: []string{"test"},
		},
		{
			name:        "多个包含模式取并集",
			includes:    []string{"*-data", "mysql-*"},
			wantMatched: []string{"seaweed-data", "mysql-db"},
		},
		{
			name:     "同时命中包含与排除时排除优先",
			includes: []string{"*"},
			excludes: []string{"*-cache"},
			wantMatched: []string{
				"seaweed-config",
				"seaweed-data",
				"vaultwarden-vol",
				"mysql-db",
				"test",
			},
			wantExcluded: []string{"redis-cache"},
		},
		{
			name:     "多个排除模式",
			includes: []string{"*"},
			excludes: []string{"*-cache", "test"},
			wantMatched: []string{
				"seaweed-config",
				"seaweed-data",
				"vaultwarden-vol",
				"mysql-db",
			},
			wantExcluded: []string{"redis-cache", "test"},
		},
		{
			name:         "排除后为空",
			includes:     []string{"*-data"},
			excludes:     []string{"seaweed-*"},
			wantMatched:  []string{},
			wantExcluded: []string{"seaweed-data"},
		},
		{
			name:        "排除模式不命中",
			includes:    []string{"test"},
			excludes:    []string{"nginx*"},
			wantMatched: []string{"test"},
		},
		{
			name:     "包含模式语法错误",
			includes: []string{"["},
			wantErr:  true,
		},
		{
			name:     "排除模式语法错误",
			includes: []string{"*"},
			excludes: []string{"["},
			wantErr:  true,
		},
		{
			name:    "缺少包含模式",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMatched, gotExcluded, err := filterVolumeNames(allVolumes, tt.includes, tt.excludes)
			if (err != nil) != tt.wantErr {
				t.Fatalf("filterVolumeNames() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			if !reflect.DeepEqual(gotMatched, tt.wantMatched) {
				t.Errorf("filterVolumeNames() matched = %v, want %v", gotMatched, tt.wantMatched)
			}
			if !reflect.DeepEqual(gotExcluded, tt.wantExcluded) {
				t.Errorf("filterVolumeNames() excluded = %v, want %v", gotExcluded, tt.wantExcluded)
			}
		})
	}
}

func TestGetBackupKey(t *testing.T) {
	// 定义固定卷名
	vol := "seaweed-config"

	tests := []struct {
		name    string
		now     time.Time
		wantKey string
	}{
		{
			name:    "UTC 时间",
			now:     time.Date(2026, 3, 9, 15, 20, 27, 0, time.UTC),
			wantKey: "seaweed-config/20260309T152027Z.tar.zstd",
		},
		{
			name: "本地时间转换为 UTC 时间戳",
			// 2026-03-01 04:00 +08:00 == 2026-02-28T20:00:00Z
			now:     time.Date(2026, 3, 1, 4, 0, 0, 0, time.FixedZone("CST", 8*3600)),
			wantKey: "seaweed-config/20260228T200000Z.tar.zstd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getBackupKey(vol, tt.now)
			if got != tt.wantKey {
				t.Errorf("getBackupKey() = %v, want %v", got, tt.wantKey)
			}
		})
	}
}

type recordingLogger struct {
	infos     []string
	successes []string
	warnings  []string
	errors    []string
}

func (l *recordingLogger) Info(message string)    { l.infos = append(l.infos, message) }
func (l *recordingLogger) Success(message string) { l.successes = append(l.successes, message) }
func (l *recordingLogger) Warning(message string) { l.warnings = append(l.warnings, message) }
func (l *recordingLogger) Error(message string)   { l.errors = append(l.errors, message) }

func TestBackupActionDryRun(t *testing.T) {
	withFakePodman(t, `#!/bin/sh
echo app-data
echo app-cache
echo tmp-cache
`)

	logger := &recordingLogger{}
	engine := Engine{Logger: logger}

	if err := engine.BackupAction(context.Background(), []string{"app-*"}, []string{"*-cache"}, false, true); err != nil {
		t.Fatalf("BackupAction() error = %v", err)
	}

	joined := strings.Join(logger.infos, "\n")
	if !strings.Contains(joined, "匹配到 1 个卷") {
		t.Errorf("dry-run 输出缺少数量: %v", logger.infos)
	}
	if !strings.Contains(joined, "app-data") {
		t.Errorf("dry-run 输出缺少卷名: %v", logger.infos)
	}
	if !strings.Contains(joined, "已排除 1 个卷") {
		t.Errorf("dry-run 输出缺少排除信息: %v", logger.infos)
	}
	if strings.Contains(joined, "tmp-cache") {
		t.Errorf("dry-run 输出包含未匹配的卷: %v", logger.infos)
	}
	if len(logger.successes) != 0 {
		t.Errorf("dry-run 不应执行备份: %v", logger.successes)
	}
}

func TestBackupActionAllExcluded(t *testing.T) {
	withFakePodman(t, `#!/bin/sh
echo app-data
echo app-cache
`)

	engine := Engine{Logger: &recordingLogger{}}

	err := engine.BackupAction(context.Background(), []string{"app-*"}, []string{"*"}, false, true)
	if err == nil || !strings.Contains(err.Error(), "均被 --exclude 排除") {
		t.Fatalf("BackupAction() error = %v, 期望提示全部被排除", err)
	}
}
