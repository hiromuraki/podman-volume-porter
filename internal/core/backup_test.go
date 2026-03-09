package core

import (
	"reflect"
	"testing"
	"time"
)

func TestGetVolumeNames(t *testing.T) {
	// 准备模拟的卷名数据
	allVolumes := []string{
		"seaweed-config",
		"seaweed-data",
		"vaultwarden-vol",
		"mysql-db",
		"redis-cache",
		"test",
	}

	// 定义测试用例表格
	tests := []struct {
		name        string   // 测试用例描述
		pattern     string   // 输入的匹配模式
		wantMatched []string // 期望得到的结果
	}{
		{
			name:        "全匹配通配符",
			pattern:     "*",
			wantMatched: allVolumes,
		},
		{
			name:        "后缀匹配",
			pattern:     "*-data",
			wantMatched: []string{"seaweed-data"},
		},
		{
			name:        "前缀匹配",
			pattern:     "seaweed-*",
			wantMatched: []string{"seaweed-config", "seaweed-data"},
		},
		{
			name:        "中间包含匹配",
			pattern:     "*weed*",
			wantMatched: []string{"seaweed-config", "seaweed-data"},
		},
		{
			name:        "精确匹配",
			pattern:     "test",
			wantMatched: []string{"test"},
		},
		{
			name:        "无任何匹配",
			pattern:     "nginx*",
			wantMatched: []string{},
		},
		{
			name:        "特殊字符匹配 (?)",
			pattern:     "tes?",
			wantMatched: []string{"test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterVolumeNames(allVolumes, tt.pattern)

			if !reflect.DeepEqual(got, tt.wantMatched) {
				t.Errorf("getVolumeNames() got = %v, want %v", got, tt.wantMatched)
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
			name: "每月1号 - 月度备份",
			// 2026-03-01 是周日
			now:     time.Date(2026, 3, 1, 10, 30, 0, 0, time.UTC),
			wantKey: "seaweed-config/20260301T103000Z_monthly.tar.gz",
		},
		{
			name: "周一但不是1号 - 周备份",
			// 2026-03-02 是周一
			now:     time.Date(2026, 3, 2, 10, 30, 0, 0, time.UTC),
			wantKey: "seaweed-config/20260302T103000Z_weekly.tar.gz",
		},
		{
			name: "普通日期 - 日常备份",
			// 2026-03-03 是周二
			now:     time.Date(2026, 3, 3, 10, 30, 0, 0, time.UTC),
			wantKey: "seaweed-config/20260303T103000Z_daily.tar.gz",
		},
		{
			name: "边界：1号正好是周一 - 优先月度备份",
			// 2026-06-01 是周一
			now:     time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC),
			wantKey: "seaweed-config/20260601T150000Z_monthly.tar.gz",
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
