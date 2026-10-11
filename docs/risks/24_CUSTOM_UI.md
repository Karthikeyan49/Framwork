# Custom UI Ceiling

## Solution
Create a versioned UI Component Registry.

Every component defines:
- props schema
- compatible field types
- events/actions
- accessibility behavior
- mobile behavior
- permissions/capability hints
- renderer version

Levels:
1. built-in primitive
2. composed layout
3. approved custom React component
4. separate bespoke surface as last resort

Custom components never receive database credentials.

## Verification
E2E, accessibility, permission-negative tests, renderer compatibility tests.
