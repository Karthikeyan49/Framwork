# Query Engine

Typed Query AST supports:
source entity, projection, filter, sort, pagination, declared relationships, bounded aggregations.

Forbidden:
raw SQL, arbitrary DB functions, arbitrary tables, uncontrolled recursive joins.

Compiler maps semantic IDs to physical schema.
Runtime parameterizes all values and applies time/page/cost limits.
