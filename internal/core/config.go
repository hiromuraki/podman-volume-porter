package core

import (
	"fmt"
	"os"
	"strconv"
)

func GetEnv(key string, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func GetIntEnv(key string, fallback int) int {
	valStr, exists := os.LookupEnv(key)
	if !exists {
		return fallback
	}
	valInt, err := strconv.Atoi(valStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "环境变量 %s=%s 无效，将使用默认值 %d\n", key, valStr, fallback)
		return fallback
	}
	return valInt
}

type AppConfig struct {
	BackupBucketName string
	TaskTimeout      int
}

var Config *AppConfig

func LoadConfig() {
	Config = &AppConfig{
		BackupBucketName: GetEnv("BACKUP_BUCKET_NAME", "container-volume"),
		TaskTimeout:      GetIntEnv("TASK_TIMEOUT", 7200),
	}
}
