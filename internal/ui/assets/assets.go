package assets

import _ "embed"

// GhostPNG is the static sprite used by desktop notifications.
//
//go:embed ghost.png
var GhostPNG []byte

// GhostFramesPNG holds the pet's six-frame idle loop.
//
//go:embed ghost-frames.png
var GhostFramesPNG []byte
