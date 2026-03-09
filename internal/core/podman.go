package core

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
)

func VolumeExists(ctx context.Context, volumeName string) bool {
	cmd := exec.CommandContext(ctx, "podman", "volume", "import", volumeName, "-")
	err := cmd.Run()
	return err == nil
}

func GetAllVolumeNames() []string {
	cmd := exec.Command("podman", "volume", "ls", "--format", "{{.Name}}")
	var out bytes.Buffer
	cmd.Stdout = &out

	err := cmd.Run()
	if err != nil {
		return []string{}
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")

	var volumeNames []string
	for _, line := range lines {
		if name := strings.TrimSpace(line); name != "" {
			volumeNames = append(volumeNames, name)
		}
	}
	return volumeNames
}
