# Schema Evolution

## Failure mode
A metadata edit requires changing a physical DB that already contains years of data.

## Solution: expand → backfill → contract
1. Expand with backward-compatible schema.
2. Run old/new runtime compatibility.
3. Backfill in checkpointed batches.
4. Validate counts/checksums/business constraints.
5. Activate new metadata.
6. Observe.
7. Contract old schema later.

## Migration classes
- SAFE: add nullable field/table/index
- CONTROLLED: type widening, required field after backfill, rename with aliases
- DANGEROUS: drop/narrow/merge/split destructive changes

Dangerous changes require elevated approval and separate cleanup release.

## Verification
Migration rehearsal on production-like data; prior runtime must remain compatible during activation window.
