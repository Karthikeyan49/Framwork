//go:build integration && race

package integration

// raceBuild reports whether the race detector is compiled in. Tests whose subject is a cost envelope
// (the 3 s runtime transaction) size their fixtures down under it: the detector slows the code several times.
const raceBuild = true
