package providers

import "embed"

// FS contains the official providers released with rses.
//
//go:embed ssh-tunnel cloudflared-tcp
var FS embed.FS
