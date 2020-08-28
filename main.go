package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"sort"
	"time"
)

type extension struct {
	Code         string `json:"code"`
	TypeName     string `json:"typeName"`
	VariableName string `json:"pageSize"`
}

type location struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type graphQLError struct {
	Path       []string   `json:"path"`
	Extensions *extension `json:"extensions"`
	Locations  []location `json:"locations"`
	Message    string     `json:"message"`
}

func (e *graphQLError) String() string {
	return e.Message
}

type errorResponse struct {
	Errors []graphQLError `json:"errors"`
}

type author struct {
	Login string `json:"login"`
}

type pullRequest struct {
	Number    int64  `json:"number"`
	Title     string `json:"title"`
	Author    author `json:"author"`
	CreatedAt string `json:"createdAt"`
}

type pullRequests struct {
	Nodes []*pullRequest `json:"nodes"`
}

type repository struct {
	Name         string       `json:"name"`
	PullRequests pullRequests `json:"pullRequests"`
}

type cursor struct {
	Cursor string `json:"cursor"`
}

type repositories struct {
	TotalCount int64         `json:"totalCount"`
	Nodes      []*repository `json:"nodes"`
	Edges      []*cursor     `json:"edges"`
}

type organization struct {
	Login        string        `json:"login"`
	Repositories *repositories `json:"repositories"`
}

type data struct {
	Organization *organization `json:"organization"`
}

type response struct {
	Data *data `json:"data"`
}

type Printable struct {
	Organization *organization
	Repository   *repository
	PullRequest  *pullRequest
	CreatedDate  time.Time
}

func main() {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		fmt.Println("Failed to get github token")
		return
	}

	doneCh := make(chan bool)
	printableCh := make(chan *Printable)

	orgNames := []string{"gdbu", "hatch1fy", "hatchify", "hatch-integrations", "vroomy"}

	for _, org := range orgNames {
		org := org
		go func() {
			err := getPrintables(token, org, printableCh)
			if err != nil {
				fmt.Printf("error walking %s: %v", org, err)
			} else {
				fmt.Println("Finished with", org)
			}
			doneCh <- true
		}()
	}

	var printables []*Printable
	done := 0
	for done < len(orgNames) {
		select {
		case printable := <-printableCh:
			printables = append(printables, printable)

		case <-doneCh:
			done++
		}
	}

	fmt.Printf("Found %d open pull requests\n", len(printables))

	sort.Slice(printables, func(i, j int) bool {
		return printables[i].CreatedDate.Unix() < printables[j].CreatedDate.Unix()
	})

	now := time.Now()
	for _, p := range printables {
		age := now.Sub(p.CreatedDate)
		label := getBucketLabel(age)
		if label != "" {
			fmt.Println(label)
		}

		fmt.Printf("%d days | github.com/%s/%s/pull/%d: %s <%s>\n",
			int(age.Hours()/24),
			p.Organization.Login,
			p.Repository.Name,
			p.PullRequest.Number,
			p.PullRequest.Title,
			p.PullRequest.Author.Login,
		)
	}

	fmt.Printf("\n\nSummary of %d PRs\n", len(printables))

	var count = 0
	for _, bucket := range buckets {
		count += bucket.count
		fmt.Printf("- %s: %d PRs\n", bucket.label, bucket.count)
	}
	fmt.Printf("- Newer than %s: %d\n", buckets[len(buckets)-1].label, len(printables)-count)
}

type bucket struct {
	olderThan time.Duration
	label     string
	found     bool
	count     int
}

const Day = time.Hour * 24

var buckets = []*bucket{
	{
		olderThan: 365 * Day,
		label:     "one year",
	},
	{
		olderThan: 6 * 30 * Day,
		label:     "six months",
	},
	{
		olderThan: 3 * 30 * Day,
		label:     "three months",
	},
	{
		olderThan: 30 * Day,
		label:     "one month",
	},
	{
		olderThan: 7 * Day,
		label:     "one week",
	},
}

func getBucketLabel(age time.Duration) string {
	for _, bucket := range buckets {
		if age <= bucket.olderThan {
			continue
		}

		bucket.count += 1

		if bucket.found {
			return ""
		}

		bucket.found = true
		return "Older than " + bucket.label
	}

	return ""
}

func getPrintables(token string, orgName string, printableCh chan *Printable) error {
	client := http.Client{}
	query := `
query getAllRepos($orgName: String = "hatch1fy", $after: String, $pageSize: Int!) {
  organization(login: $orgName) {
    login
    repositories(first: $pageSize, orderBy: {field: NAME, direction: ASC}, after: $after) {
      totalCount
      nodes {
        name
        pullRequests(first: 10, states: OPEN) {
          nodes {
            number
            title
            author {
              login
            }
            createdAt
          }
        }
      }
      edges {
        cursor
      }
    }
  }
}
`
	pageSize := 100

	vars := map[string]interface{}{
		"orgName":  orgName,
		"after":    nil,
		"pageSize": pageSize,
	}

	body := map[string]interface{}{
		"query":     query,
		"variables": vars,
	}

	pageNumber := 1

	for {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal json: %v", err)
		}

		reader := bytes.NewReader(buf)
		_, err = reader.Seek(0, io.SeekStart)
		if err != nil {
			return fmt.Errorf("failed to seek reader: %v", err)
		}

		req, err := http.NewRequest("POST", "https://api.github.com/graphql", reader)
		if err != nil {
			return fmt.Errorf("failed to create new request: %v", err)
		}

		req.Header.Add("Content-Type", "application/json")
		req.Header.Add("Authorization", "token "+token)

		fmt.Printf("Getting %s repositories, page #%d\n", orgName, pageNumber)
		res, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to make request: %v", err)
		}

		if res.StatusCode != 200 {
			return fmt.Errorf("github returned %d", res.StatusCode)
		}

		responseBody, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return errors.New("cannot read response body")
		}

		var errRes errorResponse
		err = json.Unmarshal(responseBody, &errRes)
		if err != nil {
			return fmt.Errorf("failed to unmarshal error: %v", err)
		}

		if errRes.Errors != nil {
			for _, e := range errRes.Errors {
				return fmt.Errorf("failed to make graphql request: %s", e.String())
			}
		}

		var model response
		err = json.Unmarshal(responseBody, &model)
		if err != nil {
			return fmt.Errorf("failed to unmarshal response: %v", err)
		}

		repos := model.Data.Organization.Repositories
		for _, repo := range repos.Nodes {
			for _, pr := range repo.PullRequests.Nodes {
				c, _ := time.Parse(time.RFC3339, pr.CreatedAt)

				printable := &Printable{
					Organization: model.Data.Organization,
					Repository:   repo,
					PullRequest:  pr,
					CreatedDate:  c,
				}
				printableCh <- printable
			}
		}

		if len(repos.Edges) != pageSize {
			return nil
		}

		for _, cursor := range repos.Edges {
			vars["after"] = cursor.Cursor
		}

		pageNumber += 1
	}
}
