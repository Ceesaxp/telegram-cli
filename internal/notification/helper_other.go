//go:build !unix

package notification

import "os/exec"

// killGroupOnCancel leaves cmd's cancellation as CommandContext set it,
// killing the process itself. Process groups are a unix idea, and no
// platform outside unix has a helper here to run.
func killGroupOnCancel(*exec.Cmd) {}
