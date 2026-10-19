# ERP Foundational Types

## Money
Fixed decimal amount + ISO-4217 currency. Never floating point.

## FX snapshot
Persist source/target currency, rate, source, effective_at with posted transactions.

## Date/time
Persist instants in UTC plus IANA zone identifiers such as tenant/user/business timezone.

## Business calendar
First-class working days, holidays, business hours and fiscal calendars.

## Document numbers
Separate draft IDs from final/legal numbers. Where gapless numbering is required, allocate final number in the same posting transaction with suitable locking/sequence control.
