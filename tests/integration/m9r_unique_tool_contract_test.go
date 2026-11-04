//go:build integration

package integration

import (
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// Authoring schema 1.5.0 (the tool contract) is a superset of 1.4.0 and carries field.unique. Before migration 00090 the SQL port
// runtime.register_unique_constraint selected the field with schema_version='1.4.0', so under 1.5.0 the claim could never be
// registered (HTTP 400 invalid_index) and every write to the entity stayed refused (409 unique integrity not ready): remediation
// review B-05 and independent review IR-01. The same lifecycle is exercised under both contracts.
func TestM9RUniqueLifecycleIsTheSameUnderEveryIntegrityCapableSchema(t *testing.T) {
	for _, version := range []string{metadata.IntegritySchemaVersion, metadata.ToolWorkflowSchemaVersion} {
		t.Run(version, func(t *testing.T) {
			m8Database(t)
			_, h, base, token, _, _ := uniqueFixtureAt(t, 60, false, version)
			path := base + "/unique-constraints/field.contact.memo0"
			// Fail closed before registration: neither a value nor an absent value is written without a READY claim.
			uniqueCreate(t, h, base, token, "before-registration", 409)
			uniqueCreate(t, h, base, token, nil, 409)
			x := uniqueReady(t, h, path, token)
			if x.State != "READY" || x.Generation != 1 || x.ScannedRows != 60 || x.VerifiedRows != 60 {
				t.Fatalf("unique build under %s: %+v", version, x)
			}
			uniqueCreate(t, h, base, token, "brand-new", 200)
			uniqueCreate(t, h, base, token, nil, 200)
			uniqueCreate(t, h, base, token, "brand-new", 409)
			uniqueCreate(t, h, base, token, "unique-1", 409)
		})
	}
}
