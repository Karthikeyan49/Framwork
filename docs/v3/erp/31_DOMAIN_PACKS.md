# ERP Domain Packs

Dynamic metadata does not redefine invariant-heavy ERP logic.

## Accounting Pack
Typed:
chart of accounts, journal, journal lines, fiscal period, currency, tax references, posting engine, reversal, trial balance.

Hard invariants:
- debits = credits
- closed periods reject posting
- posted journals are not silently mutated
- deterministic currency rounding

## Inventory Pack
Typed:
item, warehouse/location, stock movement, reservation, transfer, adjustment, valuation policy.

AI may configure domain packs; AI does not redefine invariants.
