package rules

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"io"
	"strings"
	"time"
)

// This reviewed archive is part of DSL2 semantics, not host zoneinfo or ZONEINFO.
//
//go:embed tzdata/zoneinfo.zip
var zoneArchive []byte

// TZDataHash is the digest of the embedded archive. The archive is constant for the
// life of the process and Restore reads the digest for every stored plan, so it is
// computed once, at package initialisation (the pure rule engine imports no sync).
var tzDataHash = func() string { h := sha256.Sum256(zoneArchive); return hex.EncodeToString(h[:]) }()

func TZDataHash() string { return tzDataHash }
func location(name string) (*time.Location, error) {
	if name == "" || name == "Local" || len(name) > 100 || strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
		return nil, ErrDate
	}
	if name == "UTC" {
		return time.UTC, nil
	}
	z, e := zip.NewReader(bytes.NewReader(zoneArchive), int64(len(zoneArchive)))
	if e != nil {
		return nil, ErrDate
	}
	for _, f := range z.File {
		if f.Name != name {
			continue
		}
		r, e := f.Open()
		if e != nil {
			return nil, ErrDate
		}
		raw, e := io.ReadAll(io.LimitReader(r, 1<<20))
		_ = r.Close()
		if e != nil || len(raw) >= 1<<20 {
			return nil, ErrDate
		}
		l, e := time.LoadLocationFromTZData(name, raw)
		if e != nil {
			return nil, ErrDate
		}
		return l, nil
	}
	return nil, ErrDate
}
