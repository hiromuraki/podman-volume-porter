package core

import (
	"reflect"
	"testing"
)

func TestFilterObjectKeys(t *testing.T) {
	tests := []struct {
		name       string
		volumeName string
		objKeys    []string
		want       []string
	}{
		{
			name:       "常规混合数据过滤",
			volumeName: "seaweed-config",
			objKeys: []string{
				"seaweed-config/20260310T083907Z_daily.tar.zstd",  // ✅ 完美匹配
				"seaweed-config/20260309T152031Z_weekly.tar.zstd", // ✅ 完美匹配
				"other-volume/20260310T083907Z_daily.tar.zstd",    // ❌ 不同的 volume
				"seaweed-config/20260310T08390Z_daily.tar.zstd",   // ❌ 错误的时间格式 (少一位)
				"seaweed-config/20260308T101010Z_daily.tar.gz",    // ❌ 错误的后缀 (由于你移除了 gz 兼容，这里会被干掉)
			},
			want: []string{
				"seaweed-config/20260310T083907Z_daily.tar.zstd",
				"seaweed-config/20260309T152031Z_weekly.tar.zstd",
			},
		},
		{
			name:       "卷名包含正则特殊字符",
			volumeName: "db.data*prod",
			objKeys: []string{
				"db.data*prod/20260310T083907Z_daily.tar.zstd", // ✅ 匹配
				"dbXdataYprod/20260310T083907Z_daily.tar.zstd", // ❌ 确保 QuoteMeta 兜底了，而不是被当成正则通配符
			},
			want: []string{
				"db.data*prod/20260310T083907Z_daily.tar.zstd",
			},
		},
		{
			name:       "空输入与全不匹配",
			volumeName: "test-vol",
			objKeys:    []string{"nginx/readme.md"},
			want:       nil, // 在 Go 中，未初始化的切片值为 nil
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterObjectKeys(tt.objKeys, tt.volumeName)

			// Go 的 reflect.DeepEqual 对 nil 和 []string{} 是敏感的。
			// 为了防止你以后在代码里改成 return []string{} 导致测试报错，这里加个小容错：
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("filterObjectKeys() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSortObjectKeys(t *testing.T) {
	tests := []struct {
		name    string
		objKeys []string
		want    []string
	}{
		{
			name: "乱序时间戳排序 (降序：最新在前)",
			objKeys: []string{
				"seaweed/20260301T100000Z_daily.tar.zstd",  // 最老
				"seaweed/20260310T100000Z_daily.tar.zstd",  // 最新
				"seaweed/20260305T100000Z_weekly.tar.zstd", // 中间
			},
			want: []string{
				"seaweed/20260310T100000Z_daily.tar.zstd",
				"seaweed/20260305T100000Z_weekly.tar.zstd",
				"seaweed/20260301T100000Z_daily.tar.zstd",
			},
		},
		{
			name: "包含异常数据 (不含时间戳的文件会沉底)",
			objKeys: []string{
				"seaweed/20260301T100000Z_daily.tar.zstd",
				"seaweed/README.md", // 无效数据
				"seaweed/20260310T100000Z_daily.tar.zstd",
			},
			want: []string{
				"seaweed/20260310T100000Z_daily.tar.zstd",
				"seaweed/20260301T100000Z_daily.tar.zstd",
				"seaweed/README.md",
				// 💡 原理：正则 FindString 找不到时会返回空字符串 ""。
				// 在 Go 的字符串比较中，"" < "20260310..."，所以有效日期排在前面，空字符串沉到最后。
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 因为 sortObjectKeys 是原址(in-place)修改数据
			// 我们直接复制一份供比较，防止污染原始用例定义（尽管这里影响不大）
			inputKeys := make([]string, len(tt.objKeys))
			copy(inputKeys, tt.objKeys)

			sortObjectKeys(inputKeys)

			if !reflect.DeepEqual(inputKeys, tt.want) {
				t.Errorf("sortObjectKeys() = \n%v\nwant \n%v", inputKeys, tt.want)
			}
		})
	}
}
