package core

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
)

type Engine struct {
	Storage S3Storage
	Logger  Logger
	UI      UI
}

type Logger interface {
	Info(message string)
	Success(message string)
	Warning(message string)
	Error(message string)
}

type ConsoleLogger struct{}

// shouldColorize 决定是否输出 ANSI 颜色：显式设置 NO_COLOR 或输出不是终端时禁用。
func shouldColorize(noColor bool, isTerminal bool) bool {
	return !noColor && isTerminal
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func colorize(color string, message string, file *os.File) string {
	_, noColor := os.LookupEnv("NO_COLOR")
	if !shouldColorize(noColor, isTerminal(file)) {
		return message
	}
	return color + message + colorReset
}

func (ConsoleLogger) Info(message string) {
	fmt.Printf("[INFO] %s\n", message)
}

func (ConsoleLogger) Success(message string) {
	fmt.Println(colorize(colorGreen, "[SUCCESS] "+message, os.Stdout))
}

func (ConsoleLogger) Warning(message string) {
	fmt.Println(colorize(colorYellow, "[WARNING] "+message, os.Stdout))
}

func (ConsoleLogger) Error(message string) {
	fmt.Fprintln(os.Stderr, colorize(colorRed, "[ERROR] "+message, os.Stderr))
}

type UI interface {
	Confirm(prompt string) (bool, error)
}

type ConsoleUI struct{}

func (c ConsoleUI) Confirm(prompt string) (bool, error) {
	fmt.Printf("%s (y/N): ", colorize(colorYellow, prompt, os.Stdout))
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("读取输入失败: %w", err)
	}

	input = strings.TrimSpace(strings.ToLower(input))
	return input == "y" || input == "yes", nil
}
