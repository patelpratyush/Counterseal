# Policy engine

The second implementation slice adds deterministic monotonic-delegation checks
and CEL approval conditions to the envelope core. It does not implement a server,
a policy registry, revocation, approval storage, or tool-call enforcement.

## Contract

`policy.Engine.Diff(parent, child, now)` returns `ALLOW` or `DENY` with sorted,
structured violations. The explicit time makes results reproducible. Both
policies must be valid version-1 envelopes and unexpired at that time.

A child must reference its parent's ID, have a distinct ID, and name the parent's
recipient (including version) as its issuer. Purpose and policy version are
inherited unchanged. Its current depth is the parent's depth plus one and must
fit within its own maximum, which cannot exceed the parent's maximum. Authority
expansion flags are rejected on either envelope.

Actions and data classes are exact, case-sensitive identifiers. Empty sets grant
nothing. Allowed actions and data classes can only shrink. Every explicit denial
must be retained, although children can add denials. Retaining a denied action in
an allowed list does not remove the denial; a future action enforcer must give
explicit denials precedence. Diff does not authorize individual tool calls.

Resources are grouped by exact category. Each child scope must be contained in
a scope of the same category in the parent. Supported scope syntax:

- Exact opaque identifiers such as `48319`.
- `*`, meaning all resources within this category.
- A terminal `/*`, such as `customer/*`, meaning a literal prefix ending in `/`.

Other wildcard forms are rejected. Resource names are not filesystem paths or
URLs and receive no decoding or normalization. An adapter must use the same
canonical resource identifiers when enforcing these rules.

The child's expiration cannot be later than the parent's. Exact equality with
the evaluation time is expired.

## Approval inheritance and evaluation

CEL expressions compile in an environment with `refund`, `order`, `customer`,
and `request` maps. Conditions must return booleans. Expression size is limited
to 4096 bytes; evaluations have a CEL cost limit of 10,000. Integration follows
the [CEL-Go compile/check/evaluate API](https://github.com/google/cel-go).

For every parent requirement, a child requirement for the same role must cover
all cases where that parent requirement applies. The proof supports:

- Identical expressions after CEL formatting.
- An unconditional child condition (`true`).
- Simple comparisons of the same field to integer literals using `>`, `>=`, `<`,
  and `<=`, with the same direction and a provably stronger approval threshold.

For example, `refund.amount > 500` can become `refund.amount > 300`.
`refund.amount >= 500` cannot become `refund.amount > 500`. Decimal thresholds,
reversed comparisons, and arbitrary Boolean rewrites require preserving the
original requirement and adding the new one separately. An unproven implication
is denied; the engine never infers safety from sampled request values.

`Conditions.RequiredRoles` returns sorted, deduplicated roles for a request.
Missing data, malformed expressions, evaluation errors, or invalid roles return
an error. Callers must deny on error. The return value lists obligations; it does
not assert that approval exists. Approval identity, scope, expiry, and replay
protection belong to the later server/action-enforcement slices.

## CLI and trust boundary

`policy validate ENVELOPE` checks one policy's structure, expiry, and expressions.
`policy diff PARENT CHILD` checks the delegation relationship. Both accept JSON
or YAML envelope documents and `--format text|json`. Unknown fields, duplicate
keys, and multiple documents are rejected. ALLOW exits zero; DENY or input errors
exit nonzero. YAML uses the same envelope schema, not the future policy-registry
configuration schema. Quote schema strings such as `version: "1"` and numeric
resource IDs.

Diff without keys analyzes policy content only and labels output accordingly.
Supplying both `--parent-key` and `--child-key` also verifies both signatures.
Keys are caller-supplied trust anchors; issuer-to-key binding, chain lookup, and
revocation are not provided. JSON output includes `checks` so consumers can
distinguish content analysis from checks that include signatures.

The existing `envelope create` command remains JSON-only.

## Verification

Tests cover delegation invariants, threshold inclusivity, CEL failures,
deterministic output, CLI error exits, strict decoding, and signature tampering.
A generated corpus exercises 100 valid and 100 invalid delegations. Fuzz targets
check added-action expansion and approval-threshold weakening. `BenchmarkDiff`
measures a representative delegation comparison including CEL compilation.
