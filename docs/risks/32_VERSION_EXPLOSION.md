# Version Explosion

## Solution
Use layering:
base product → industry template → tenant overlay → environment overlay.

Store immutable content-addressed definitions and structural diffs. During promotion produce a fully resolved release bundle with hash.

Keep all production releases for audit; expire abandoned drafts by policy.

## Verification
Simulate thousands of tenant overlays; compile/cache behavior and storage growth must remain bounded.
