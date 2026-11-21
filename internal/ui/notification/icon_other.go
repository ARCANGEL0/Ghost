//go:build !darwin

package notification

import (
	"github.com/swadhinbiswas/ghost/internal/ui/assets"
)

// Icon contains the embedded PNG icon data for desktop notifications.
var Icon any = assets.GhostPNG
