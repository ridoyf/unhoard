//go:build !production

package main

import "os"

// Dev builds only: UNHOARD_DEVTOOLS_PORT opens the WebView's DevTools protocol on
// localhost so the UI can be driven by scripts. Release builds use the production tag.
func init() {
	if port := os.Getenv("UNHOARD_DEVTOOLS_PORT"); port != "" {
		browserArgs = append(browserArgs, "--remote-debugging-port="+port)
	}
}
