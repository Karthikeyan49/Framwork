package migrations

import (
	"regexp"
	"sort"
	"strconv"
	"testing"
)

// Proposed by the upgrade and ordering rehearsal (docs/verification/UPGRADE_AND_ORDERING_REHEARSAL.md); the owner has not decided the
// numbering rule yet (docs/migration/LINE_INTEGRATION_PLAN.md, "Migration numbers from here on"), and this test makes the proposal
// checkable. It encodes: one increasing sequence, 00001-00057 then 00080 upward without gaps, nothing in the abandoned
// 00058-00079 reservation, no number used twice. goose refuses a duplicate only when a provider is opened (cmd/migrate then prints
// only "migration provider initialization failed"), and refuses a file below the highest applied version only on an upgraded
// database, while a fresh install applies the same file in a different order. This check fails before any database is involved.
func TestMigrationNumbersFormOneIncreasingSequence(t *testing.T) {
	entries, err := Files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`^([0-9]{5})_[a-z0-9_]+\.sql$`)
	files := map[int]string{}
	versions := []int{}
	for _, entry := range entries {
		match := pattern.FindStringSubmatch(entry.Name())
		if match == nil {
			t.Errorf("%s: a migration is named NNNNN_lower_snake_case.sql", entry.Name())
			continue
		}
		version, _ := strconv.Atoi(match[1])
		if other, ok := files[version]; ok {
			t.Errorf("version %05d is used twice: %s and %s (the line that merges later renumbers its own unmerged migration)", version, other, entry.Name())
			continue
		}
		files[version] = entry.Name()
		versions = append(versions, version)
	}
	sort.Ints(versions)
	if len(versions) < 57 {
		t.Fatalf("only %d migrations embedded", len(versions))
	}
	next := 1
	for _, version := range versions {
		if next == 58 {
			next = 80 // 00058-00079 were never applied by any database and stay unused
		}
		if version != next {
			t.Fatalf("migration numbers are 00001-00057 and then 00080 upward without gaps: expected %05d, found %s (a new migration takes the highest number plus one; a number below the highest is refused by every upgraded database)", next, files[version])
		}
		next++
	}
}
