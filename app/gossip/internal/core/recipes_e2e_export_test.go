// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import "crypto/tls"

func TrustWARPUpstreamForE2E(cfg *tls.Config) { warpTransport.TLSClientConfig = cfg }
