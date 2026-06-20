package web

import "embed"

// FS contains the AppDeck browser interface.
//
//go:embed index.html app.js styles.css
var FS embed.FS
