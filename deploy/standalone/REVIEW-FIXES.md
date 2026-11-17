# Review fixes for Standalone customization

These changes address the numbered review against the CE 2.45.3 customization.
They do not claim Business Edition parity or a complete security audit.

| Review | Implemented behavior |
| --- | --- |
| 1 | `/users/me` decorates effective Standalone roles, matching user inspection. |
| 2a | Docker helper routes and Engine proxy routes are classified separately, including the URL after the top-level router strips `/api/endpoints`. |
| 2b | Dashboard and stack counters use effective scoped resource access; stack action policies remain restrictive. |
| 2c | Recreate and GPU inspection resolve the canonical container ID and direct/inherited ownership. Recreate uses the canonical ID when transferring resource control and webhook associations. Operator cannot recreate/change image. |
| 3 | Migration checks authorization on the target environment before deployment. A source-stack authorization context does not exempt the target. Explicit role environments continue to reject migration; target stack-policy boundaries also apply. |
| 4 | Image/distribution routes accept namespace and registry path segments without accepting encoded separators or traversal. |
| 5 | Group Access supports explicit roles and legacy CE grants for mixed environment groups. Agent Docker Hub lookup is allowlisted, with registry access checked before forwarding credentials. Volume browsing remains limited to the existing Environment Admin volume-only grant. |
| 6a | Standard respects `AllowStackManagementForRegularUsers`; only Environment Admin receives the scoped bypass. |
| 6b | Compose deploy and Start honor Environment Admin for regular-user bind mount/privileged restrictions. Deployment resolves policy access rather than trusting decorated user response fields. |
| 7a | Explicit Standard cannot call `/commit`. Legacy CE calls pass through canonical/inherited container ownership checks. |
| 7b | Both image export GET routes are blocked by the stack-policy boundary for non-admin users. |
| 8 | Overrides only add mounts to services declared in the current Compose, including during rename/removal of a service with ActiveRevision. Target metadata/history remain unchanged until explicit retirement, so Compose rollback restores the original association. Apply/rollback to absent services is rejected. Admins can retire missing-service targets; referenced IDs cannot be rebound or reused. |
| 9 | Equal-role Team ties prefer explicit grants over legacy grants. Writes serialize per stack, with reference-counted lock cleanup, rather than blocking all stacks behind one healthcheck. |
| 10 | Git actions require stack edit permission, so Operator no longer sees them. |

Regression tests are in the affected Go packages and the group Access UI test.
The role smoke additionally checks current-user roles, dashboard counts relative
to pre-existing host stacks, helper image routes and private-container denial.
The failed first integration attempt exposed the stripped-prefix variant in
issue 2a; it was added to the permanent route regression tests before rerunning.

See [VALIDATION.md](VALIDATION.md) for the final image and observed test results,
[ROLES.md](ROLES.md) for role semantics, and [README.md](README.md) for retiring
configuration targets. Existing workloads are not upgraded by building the image.
