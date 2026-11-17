# Standalone role management

Global administrators can configure the five existing roles at Users > Roles.
Policies live in the existing roles database bucket (`StandalonePolicy`), seeded
once after database migration. Existing role authorization records are preserved.
Use Save role to persist changes; Restore defaults only updates the draft.
Updates use a version check and return 409 on stale edits.

The feature matrix narrows each role's built-in maximum capabilities. It does not
promote Read-only into an operator or change which resources a role owns. It
applies to explicit StandaloneRole assignments, including team/environment-group
inheritance. Global administrators and legacy CE grants remain unchanged.
Unsupported permissions are disabled in the editor and rejected by the API.

Each new API request reads the current policy. Existing open console/stream
sessions are not terminated. Other browser sessions need a refresh to update UI
controls. The API remains authoritative even with stale UI hints.

Read permissions are prerequisites for operations on that resource. Stack edits
also require deployment permission. Snapshot aggregates are suppressed when a
constituent View permission is disabled. Resource ownership, stack action policies,
environment security settings and managed-builder protection remain in force.
Raw Docker management/exec rights are powerful and are not an isolation mechanism
between application features; use stack action policies for constrained operators.

Effective access viewer shows the resolved role, source user/team/group, saved
policy version and feature switches, resource scope, environment restrictions and
stack policy count. These are configuration diagnostics, not a per-resource
permission simulator. Use Manage access to change the assignment at its source.

Validation (2026-11-18): all tests passed in the authorization, security, roles,
users, stacks, endpoints and Docker proxy Go packages. Three UI tests passed;
TypeScript and focused ESLint passed. An isolated Docker instance verified DB
seeding, admin-only editing (403 for non-admin), immediate proxy denial with an
existing user token, stale-save conflict (409), viewer/policy agreement,
snapshot suppression, whole-role disable and persistence across restart.
The temporary instance and its data volume were removed. Live UI inspection
confirmed role editing controls and the existing user's team-derived access.
