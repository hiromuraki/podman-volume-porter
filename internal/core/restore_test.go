package core

import (
	"context"
	"reflect"
	"testing"
)

func TestFilterBackupObjects(t *testing.T) {
	objects := []ObjectInfo{
		{Key: "vol/20260310T083907Z_daily.tar.zstd"},
		{Key: "vol/20260309T152031Z_weekly.tar.zstd"},
		{Key: "other/20260310T083907Z_daily.tar.zstd"},
		{Key: "vol/20260310T08390Z_daily.tar.zstd"},
		{Key: "vol/20260308T101010Z_daily.tar.gz"},
		{Key: "vol/README.md"},
	}
	want := []ObjectInfo{
		{Key: "vol/20260310T083907Z_daily.tar.zstd"},
		{Key: "vol/20260309T152031Z_weekly.tar.zstd"},
	}

	got := filterBackupObjects(objects, "vol")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("filterBackupObjects() = %v, want %v", got, want)
	}
}

func TestSortBackupObjects(t *testing.T) {
	objects := []ObjectInfo{
		{Key: "vol/20260301T100000Z_daily.tar.zstd"},
		{Key: "vol/20260310T100000Z_daily.tar.zstd"},
		{Key: "vol/20260305T100000Z_weekly.tar.zstd"},
	}
	want := []string{
		"vol/20260310T100000Z_daily.tar.zstd",
		"vol/20260305T100000Z_weekly.tar.zstd",
		"vol/20260301T100000Z_daily.tar.zstd",
	}

	sortBackupObjects(objects)
	for i, obj := range objects {
		if obj.Key != want[i] {
			t.Errorf("sortBackupObjects()[%d] = %q, want %q", i, obj.Key, want[i])
		}
	}
}

func TestFindBestMatchedKeyRejectsInvalidObjectName(t *testing.T) {
	engine := Engine{}

	invalid := []string{
		"202603",
		"20260301",
		"vol-a/20260309T152027Z_daily.tar.zstd",
		"20260309T152027Z_daily.tar.gz",
	}

	for _, name := range invalid {
		if _, err := engine.findBestMatchedKey(context.Background(), "vol", name); err == nil {
			t.Errorf("findBestMatchedKey(%q) 期望报错，实际通过", name)
		}
	}
}
