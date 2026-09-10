package service

// Native golden runs (N19): a fixed eval suite over the native tool loop.
// ScriptedLLM proves the loop and tool contracts stay green when the tool
// set drifts; the suite itself is the gate — a failing case fails CI.
//
// Classes mirror Eval Lab (K24) task classes so a future seed into an
// eval_suite can reuse the same slugs without rewriting the brief.

const (
	// NativeGoldenSuiteName is the Eval Lab–facing name for this suite.
	NativeGoldenSuiteName = "native_golden_runs"
	// nativeGoldenPassThreshold is the minimum pass rate (0–1). Every case
	// must pass today; lowering it would hide a broken tool.
	nativeGoldenPassThreshold = 1.0
)

// NativeGoldenCase is one étalon task: identity + expected tool. The
// runnable body lives in the test file so production builds stay free of
// fixture helpers.
type NativeGoldenCase struct {
	ID           string // stable slug, e.g. "G01_helpdesk_comment"
	Class        string // helpdesk | issues | notes | schedule
	Title        string
	ExpectedTool string
}

// NativeGoldenCatalog is the locked list of 10 étalon tasks. Adding or
// removing a case is a deliberate product change — update the test and
// this catalog together.
func NativeGoldenCatalog() []NativeGoldenCase {
	return []NativeGoldenCase{
		{ID: "G01_helpdesk_comment", Class: "helpdesk", Title: "Post a helpdesk reply as a comment", ExpectedTool: "add_comment"},
		{ID: "G02_get_issue", Class: "issues", Title: "Read the assigned issue", ExpectedTool: "get_issue"},
		{ID: "G03_list_issues", Class: "issues", Title: "List workspace issues", ExpectedTool: "list_issues"},
		{ID: "G04_update_issue", Class: "issues", Title: "Rename the assigned issue", ExpectedTool: "update_issue"},
		{ID: "G05_transition_issue", Class: "issues", Title: "Move the issue to in_progress", ExpectedTool: "transition_issue"},
		{ID: "G06_create_issue", Class: "issues", Title: "File a quick-create issue", ExpectedTool: "create_issue"},
		{ID: "G07_save_note", Class: "notes", Title: "Save a knowledge note", ExpectedTool: "save_note"},
		{ID: "G08_update_note", Class: "notes", Title: "Update a living document note", ExpectedTool: "update_note"},
		{ID: "G09_search_workspace", Class: "helpdesk", Title: "Search the workspace", ExpectedTool: "search_workspace"},
		{ID: "G10_schedule_followup", Class: "schedule", Title: "Schedule a follow-up reminder", ExpectedTool: "schedule_followup"},
	}
}
