package core

import "testing"

func TestShouldColorize(t *testing.T) {
	tests := []struct {
		name       string
		noColor    bool
		isTerminal bool
		want       bool
	}{
		{name: "终端且未禁用颜色", isTerminal: true, want: true},
		{name: "设置了 NO_COLOR", noColor: true, isTerminal: true, want: false},
		{name: "输出重定向到文件", want: false},
		{name: "NO_COLOR 且非终端", noColor: true, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldColorize(tt.noColor, tt.isTerminal); got != tt.want {
				t.Errorf("shouldColorize(%v, %v) = %v, want %v", tt.noColor, tt.isTerminal, got, tt.want)
			}
		})
	}
}
