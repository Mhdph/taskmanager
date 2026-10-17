package search

type ResultType string

const (
	ResultTypeProject ResultType = "project"
	ResultTypeTask    ResultType = "task"
	ResultTypePerson  ResultType = "person"
	ResultTypeFile    ResultType = "file"
	ResultTypeComment ResultType = "comment"
)

type Result struct {
	Type ResultType

	ID string

	ProjectID *string
	TaskID    *string

	// Title depends on the type: project/task name, member display name,
	// attachment filename, or a comment snippet.
	Title string

	Description *string
}

type Response struct {
	Projects []Result
	Tasks    []Result

	People   []Result
	Files    []Result
	Comments []Result
}
