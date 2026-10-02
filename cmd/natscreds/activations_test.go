// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"fmt"
	"os"
	"testing"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const gatedACL = `system_account: SYS
accounts:
  SYS: {}
  DEPLOYER_RPC:
    exports:
      - service: bagel.rpc.admin.deploy.>
        accounts: [ADMIN_RPC]
      - stream: bagel.deploy.events.>
        accounts: [ADMIN_RPC]
      - service: svc.public.>
  ADMIN_RPC:
    imports:
      - service: bagel.rpc.admin.deploy.>
        from: DEPLOYER_RPC
      - stream: bagel.deploy.events.>
        from: DEPLOYER_RPC
`

func twoExporterACL(exporterASubject string) string {
	return fmt.Sprintf(`system_account: SYS
accounts:
  SYS: {}
  EXPORTER_A:
    exports:
      - service: %s
        accounts: [IMPORTER]
  EXPORTER_B:
    exports:
      - service: svc.b.>
        accounts: [IMPORTER]
  IMPORTER:
    imports:
      - service: %s
        from: EXPORTER_A
      - service: svc.b.>
        from: EXPORTER_B
`, exporterASubject, exporterASubject)
}

func activationFrom(keys *natsacl.Keys, importer, exporter string) natsacl.Activation {
	for _, a := range keys.Activations[importer] {
		if a.From == exporter {
			return a
		}
	}
	return natsacl.Activation{}
}

func decodedActivation(t *testing.T, entry natsacl.Activation) map[string]string {
	t.Helper()
	claims, err := jwt.DecodeActivationClaims(entry.Token)
	require.NoError(t, err)
	return map[string]string{
		"issuer": claims.Issuer, "subject": claims.Subject,
		"importSubject": string(claims.ImportSubject), "importType": claims.ImportType.String(),
	}
}

func TestGatedExportsGetActivationsSignedByTheExporter(t *testing.T) {
	h := newHarness(t, gatedACL)

	h.apply(t)

	keys := h.keys(t)
	require.Len(t, keys.Activations, 1, "an ungated export needs no activation")
	wantType := map[string]string{"bagel.rpc.admin.deploy.>": "service", "bagel.deploy.events.>": "stream"}
	entries := keys.Activations["ADMIN_RPC"]
	require.Len(t, entries, 2)
	for _, entry := range entries {
		assert.Equal(t, map[string]string{
			"issuer": keys.Accounts["DEPLOYER_RPC"], "subject": keys.Accounts["ADMIN_RPC"],
			"importSubject": entry.Subject, "importType": wantType[entry.Subject],
		}, decodedActivation(t, entry))
	}
}

func TestChangedExportReissuesOnlyItsActivation(t *testing.T) {
	h := newHarness(t, twoExporterACL("svc.a.>"))
	h.apply(t)
	first := h.keys(t)

	h.setACL(t, twoExporterACL("svc.a2.>"))
	h.apply(t)
	second := h.keys(t)

	assert.NotEqual(t, activationFrom(first, "IMPORTER", "EXPORTER_A").Token, activationFrom(second, "IMPORTER", "EXPORTER_A").Token,
		"EXPORTER_A's activation must be reissued once its export subject changes")
	assert.Equal(t, activationFrom(first, "IMPORTER", "EXPORTER_B").Token, activationFrom(second, "IMPORTER", "EXPORTER_B").Token,
		"EXPORTER_B's activation must be reused byte-for-byte")
}

func TestActivationNotSignedByTheExportersKeyIsNeverReused(t *testing.T) {
	h := newHarness(t, gatedACL)
	h.apply(t)
	keys := h.keys(t)
	stale := activationFrom(keys, "ADMIN_RPC", "DEPLOYER_RPC")
	wrongSigner, err := nkeys.CreateAccount()
	require.NoError(t, err)
	claims := jwt.NewActivationClaims(keys.Accounts["ADMIN_RPC"])
	claims.ImportSubject = jwt.Subject(stale.Subject)
	claims.ImportType = jwt.Service
	forged, err := claims.Encode(wrongSigner)
	require.NoError(t, err)
	for i := range keys.Activations["ADMIN_RPC"] {
		if keys.Activations["ADMIN_RPC"][i].Subject == stale.Subject {
			keys.Activations["ADMIN_RPC"][i].Token = forged
		}
	}
	tampered, err := yaml.Marshal(keys)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(h.cfg.KeysPath, tampered, 0o600))

	h.apply(t)

	reissued := h.keys(t)
	entry := activationFrom(reissued, "ADMIN_RPC", "DEPLOYER_RPC")
	assert.NotEqual(t, forged, entry.Token)
	assert.Equal(t, keys.Accounts["DEPLOYER_RPC"], decodedActivation(t, entry)["issuer"])
}
