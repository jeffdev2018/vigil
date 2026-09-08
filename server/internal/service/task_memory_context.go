package service

// MemoryVersion identifies an immutable published version. Content is not
// duplicated into run history: deleting a memory still deletes its text.
type MemoryVersion struct {
	ID       string `json:"id"`
	Revision int32  `json:"revision"`
}

// TaskMemoryContext records the memory selected for the latest finalized claim.
// It is not an acknowledgement from the daemon or proof the model followed it.
// AgentStatus distinguishes a successful empty read from a failed read. A nil
// ProjectVersion means no project rules were included (absent/empty/expired).
type TaskMemoryContext struct {
	DispatchedAt   string          `json:"dispatched_at"`
	AgentStatus    string          `json:"agent_status"`
	AgentVersions  []MemoryVersion `json:"agent_versions"`
	ProjectVersion *MemoryVersion  `json:"project_version"`
}
