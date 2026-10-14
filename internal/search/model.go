package search

type ResultType string

const (
	ResultTypeProject ResultType = "project"
	ResultTypeTask    ResultType = "task"
)

type Result struct {
	Type ResultType

	ID string

	ProjectID *string

	Title string

	Description *string
}

type Response struct {
	Projects []Result
	Tasks    []Result
}
