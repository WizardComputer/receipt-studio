//go:build !desktop

package desktop

import (
	"context"

	"receiptstudio.local/agent/internal/agent"
)

const DesktopBuild = false

var DesktopActive bool

func ReportDesktopError(message string) {}

func RunDesktop(ctx context.Context, stop context.CancelFunc, origin, dir string, args []string, setup bool, agent *agent.Agent) {
	<-ctx.Done()
}
