package cmd

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

	"findorgpulls/lib"
)

type Row struct {
	Organization *lib.Organization
	Repository   *lib.Repository
	PullRequest  *lib.PullRequest
	CreatedDate  time.Time
	Age          time.Duration
}

func (row *Row) String() string {
	return fmt.Sprintf("%d days | github.com/%s/%s/pull/%d: %s <%s>\n",
		int(row.Age.Hours()/24),
		row.Organization.Login,
		row.Repository.Name,
		row.PullRequest.Number,
		row.PullRequest.Title,
		row.PullRequest.Author.Login,
	)
}

func FindOrgPulls() {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		fmt.Println("Failed to get github token")
		return
	}

	doneCh := make(chan bool)
	rowCh := make(chan *Row)

	orgNames := []string{"gdbu", "hatch1fy", "hatchify", "hatch-integrations", "vroomy"}

	for _, org := range orgNames {
		org := org
		go func() {
			err := getRows(token, org, rowCh)
			if err != nil {
				fmt.Printf("error walking %s: %v", org, err)
			} else {
				fmt.Println("Finished with", org)
			}
			doneCh <- true
		}()
	}

	var rows []*Row
	done := 0
	for done < len(orgNames) {
		select {
		case row := <-rowCh:
			rows = append(rows, row)

		case <-doneCh:
			done++
		}
	}

	fmt.Printf("Found %d open pull requests\n", len(rows))

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].CreatedDate.Unix() < rows[j].CreatedDate.Unix()
	})

	for _, p := range rows {
		label := getBucketLabel(p.Age)
		if label != "" {
			fmt.Println(label)
		}

		fmt.Printf(p.String())
	}

	fmt.Printf("\n\nSummary of %d PRs\n", len(rows))

	var count = 0
	for _, bucket := range buckets {
		count += bucket.count
		fmt.Printf("- %s: %d PRs\n", bucket.label, bucket.count)
	}
	fmt.Printf("- Newer than %s: %d\n", buckets[len(buckets)-1].label, len(rows)-count)
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

func getRows(token string, orgName string, rowCh chan *Row) error {
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

	now := time.Now()

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

		var errRes lib.ErrorResponse
		err = json.Unmarshal(responseBody, &errRes)
		if err != nil {
			return fmt.Errorf("failed to unmarshal error: %v", err)
		}

		if errRes.Errors != nil {
			for _, e := range errRes.Errors {
				return fmt.Errorf("failed to make graphql request: %s", e.String())
			}
		}

		var model lib.Response
		err = json.Unmarshal(responseBody, &model)
		if err != nil {
			return fmt.Errorf("failed to unmarshal response: %v", err)
		}

		repos := model.Data.Organization.Repositories
		for _, repo := range repos.Nodes {
			for _, pr := range repo.PullRequests.Nodes {
				c, _ := time.Parse(time.RFC3339, pr.CreatedAt)
				age := now.Sub(c)

				row := &Row{
					Organization: model.Data.Organization,
					Repository:   repo,
					PullRequest:  pr,
					CreatedDate:  c,
					Age:          age,
				}
				rowCh <- row
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
