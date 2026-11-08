`package.ps1` preserves the existing Wails-generated manifest, assets and metadata,
then adds `BagelNetworkWorker.exe` before final packaging and signing. Both the
MakeAppx and optional MSIX Packaging Tool paths require the Windows SDK's
`MakeAppx.exe` for this final step. Certificate signing uses the final package.
No additional manifest capability, installer elevation or firewall rule is added.

The companion remains beside the desktop executable in NSIS and MSIX packages.
The desktop launches it with its separate zero-capability AppContainer token;
installing the MSIX does not grant capabilities to that worker.
