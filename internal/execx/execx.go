// Package execx runs helper programs (git, notification tools) without flashing a
// console window on Windows.
package execx

import (
	"context"
	"os/exec"
)

// Command is exec.CommandContext with the console window hidden on Windows.
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	hide(cmd)
	return cmd
}
