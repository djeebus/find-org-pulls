package lib

type Extension struct {
	Code         string `json:"code"`
	TypeName     string `json:"typeName"`
	VariableName string `json:"pageSize"`
}

type Location struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type GraphQLError struct {
	Path       []string   `json:"path"`
	Extensions *Extension `json:"extensions"`
	Locations  []Location `json:"locations"`
	Message    string     `json:"message"`
}

func (e *GraphQLError) String() string {
	return e.Message
}

type ErrorResponse struct {
	Errors []GraphQLError `json:"errors"`
}

type Author struct {
	Login string `json:"login"`
}

type PullRequest struct {
	Number    int64  `json:"number"`
	Title     string `json:"title"`
	Author    Author `json:"author"`
	CreatedAt string `json:"createdAt"`
}

type PullRequests struct {
	Nodes []*PullRequest `json:"nodes"`
}

type Repository struct {
	Name         string       `json:"name"`
	PullRequests PullRequests `json:"pullRequests"`
}

type Cursor struct {
	Cursor string `json:"cursor"`
}

type Repositories struct {
	TotalCount int64         `json:"totalCount"`
	Nodes      []*Repository `json:"nodes"`
	Edges      []*Cursor     `json:"edges"`
}

type Organization struct {
	Login        string        `json:"login"`
	Repositories *Repositories `json:"repositories"`
}

type Data struct {
	Organization *Organization `json:"organization"`
}

type Response struct {
	Data *Data `json:"data"`
}
