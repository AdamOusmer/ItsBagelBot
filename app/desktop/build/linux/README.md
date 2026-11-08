The desktop executable and `bagel-network-worker` must be installed beside one
another. The worker is always built with `CGO_ENABLED=0`, including when Docker
builds the desktop UI. AppImage, deb, RPM and Arch packages include it.

Linux requires the distribution's `bubblewrap` package (`/usr/bin/bwrap`), a
system CA certificate bundle, and working user/network/PID namespaces and
seccomp. The deb, RPM and Arch dependency lists include `bubblewrap` and
`ca-certificates`. AppImage users must install these prerequisites through their
normal distribution package manager. No installer downloads a sandbox or changes
kernel policy. If the OS denies the required isolation, networking fails closed.

The AppImage task preserves Wails' GTK/WebKit preparation, adds the worker to
`usr/bin`, and repacks that prepared AppDir. This is necessary because the legacy
Wails generator accepts only one executable and recreates the AppDir.
