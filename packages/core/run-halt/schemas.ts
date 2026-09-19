// Fleet halt (K05 / m169): one switch that stops a workspace's agents. The
// shape is shared with the approvals feed envelope — reuse rather than a
// second parallel schema, since GET /api/run-halt returns the exact same
// object `service.RunHaltFromSettings` produces there.
export { RunHaltSchema, EMPTY_RUN_HALT, type RunHalt } from "../approvals/schemas";
