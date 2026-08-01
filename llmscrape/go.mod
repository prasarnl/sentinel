// Shared by both sentinel/agent and sentinel/server, which scrape the same
// inference-runtime metrics endpoints from different sides: the agent from
// localhost, the server over the network for hosts with no agent. Keeping one
// implementation means a runtime's metric rename is fixed once rather than
// twice — the failure mode being silent (an unmatched name yields nil, which
// is indistinguishable from a runtime that cannot report the metric).
//
// Deliberately stdlib-only, so vendoring it into the agent's cross-compiled
// static binary costs nothing.
module sentinel/llmscrape

go 1.26.4
