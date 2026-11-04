// Package jsonschema publishes immutable authoring contracts, not compiler IR.
package jsonschema

import "embed"

//go:embed application-v1.schema.json application-v1.1.schema.json application-v1.2.schema.json application-v1.3.schema.json application-v1.4.schema.json application-v1.5.schema.json
var Files embed.FS
