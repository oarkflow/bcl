# Production Controls Walkthrough

This runnable example exercises the production hardening controls in the Condition service:

- Strict publish validation
- Required tests before publish
- Approval-gated activation
- Strict evaluation
- Shadow evaluation
- Emergency disable and enable
- Production readiness reporting
- Audit-chain verification

Run it from the `condition` module:

```sh
go run ./examples/condition_decision_platform/production_controls
```

The example uses the `feature-gating` decision definition because it has a compact schema, a strict BCL declaration, a local module source, and a passing test fixture.
