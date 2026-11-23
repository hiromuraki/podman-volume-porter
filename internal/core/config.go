package core

import (
	"fmt"
	"os"
	"strconv"
)

const defaultTaskTimeout = 7200

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
		fmt.Fprintf(os.Stderr, "环境变量 %s=%s 无效（无法解析为整型），将使用默认值 %d\n", key, valStr, fallback)
		return fallback
	}
	return valInt
}

func GetBoolEnv(key string, fallback bool) bool {
	valStr, exists := os.LookupEnv(key)
	if !exists {
		return fallback
	}

	valBool, err := strconv.ParseBool(valStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "环境变量 %s=%s 无效（无法解析为布尔值），将使用默认值 %v\n", key, valStr, fallback)
		return fallback
	}

	return valBool
}

type AppConfig struct {
	// TaskTimeout 为单次任务超时时间，单位秒。
	TaskTimeout int
}

var Config *AppConfig

func LoadConfig() {
	timeout := GetIntEnv("TASK_TIMEOUT", defaultTaskTimeout)
	if timeout <= 0 {
		fmt.Fprintf(os.Stderr, "环境变量 TASK_TIMEOUT=%d 无效（必须为正整数秒），将使用默认值 %d\n", timeout, defaultTaskTimeout)
		timeout = defaultTaskTimeout
	}

	Config = &AppConfig{
		TaskTimeout: timeout,
	}
}
