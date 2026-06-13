package ui

import (
	"embed"
	"io/fs"
)

// assetsEmbed holds the static asset tree (CSS, JS, images) compiled into the
// binary so serving does not depend on the process working directory or on a
// Containerfile COPY. The distroless image runs with WORKDIR=/home/nonroot, so
// a relative on-disk path would not resolve; embedding removes that footgun.
//
//go:embed assets
var assetsEmbed embed.FS

// AssetsFS is the embedded asset tree rooted at the assets directory, suitable
// for http.FS and the fiber filesystem middleware.
var AssetsFS = func() fs.FS {
	sub, err := fs.Sub(assetsEmbed, "assets")
	if err != nil {
		panic(err)
	}
	return sub
}()
