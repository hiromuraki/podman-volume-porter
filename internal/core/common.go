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
	colorBlue   = "\033[34m"
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

func (ConsoleLogger) Info(message string) {
	fmt.Printf("%s[INFO] %s%s\n", colorReset, message, colorReset)
}

func (ConsoleLogger) Success(message string) {
	fmt.Printf("%s[SUCCESS] %s%s\n", colorGreen, message, colorReset)
}

func (ConsoleLogger) Warning(message string) {
	fmt.Printf("%s[WARNING] %s%s\n", colorYellow, message, colorReset)
}

func (ConsoleLogger) Error(message string) {
	fmt.Fprintf(os.Stderr, "%s[ERROR] %s%s\n", colorRed, message, colorReset)
}

type UI interface {
	Confirm(prompt string) (bool, error)
}

type ConsoleUI struct{}

func (c ConsoleUI) Confirm(prompt string) (bool, error) {
	fmt.Printf("%s%s (y/N): %s", colorYellow, prompt, colorReset)
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("读取输入失败: %w", err)
	}

	input = strings.TrimSpace(strings.ToLower(input))
	return input == "y" || input == "yes", nil
}
