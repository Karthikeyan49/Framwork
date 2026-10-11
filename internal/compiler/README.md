# compiler

Reserved module boundary. No implementation in M0. See docs/architecture/12_MODULE_BOUNDARIES.md.

Expose explicit contracts when its milestone starts. Storage and implementation details stay private; consumers must not import private subpackages or access module tables.
