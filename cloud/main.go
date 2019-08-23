package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/wbrefvem/go-bitbucket"
)

// FIXME looks like all endpoints support pageSize but they aren't exposed in go-wrapper

func getRepositories(ctx context.Context, c *bitbucket.APIClient, owner string) ([]bitbucket.Repository, error) {
	var repos []bitbucket.Repository
	var results bitbucket.PaginatedRepositories
	var err error

	for {
		if results.Next == "" {
			results, _, err = c.RepositoriesApi.RepositoriesUsernameGet(ctx, owner, nil)
		} else {
			results, _, err = c.PagingApi.RepositoriesPageGet(ctx, results.Next)
		}
		if err != nil {
			return nil, err
		}

		repos = append(repos, results.Values...)
		if results.Next == "" {
			break
		}
	}

	return repos, nil
}

var errUnavailable = fmt.Errorf("resource is unavailable")

func getPullRequests(ctx context.Context, c *bitbucket.APIClient, repo bitbucket.Repository) ([]bitbucket.Pullrequest, error) {
	var prs []bitbucket.Pullrequest
	var results bitbucket.PaginatedPullrequests
	var resp *http.Response
	var err error

	// REST API supports passing multiple states to request but Go wrapper doesn't
	states := []string{"OPEN", "MERGED", "SUPERSEDED", "DECLINED"}
	for _, state := range states {
		for {
			if results.Next == "" {
				results, resp, err = c.PullrequestsApi.RepositoriesUsernameRepoSlugPullrequestsGet(ctx, repo.Owner.Uuid, repo.Uuid, map[string]interface{}{
					"state": state,
				})
			} else {
				results, resp, err = c.PagingApi.PullrequestsPageGet(ctx, results.Next)
			}
			if err != nil {
				// for some of my old projects API returns 404 for pull requests endpoints
				// though I can access PRs page in UI (there are no prs)
				if resp.StatusCode == http.StatusNotFound {
					return nil, errUnavailable
				}
				return nil, err
			}

			prs = append(prs, results.Values...)
			if results.Next == "" {
				break
			}
		}
	}

	return prs, nil
}

func getPRComments(ctx context.Context, c *bitbucket.APIClient, repo bitbucket.Repository, pr bitbucket.Pullrequest) ([]bitbucket.PullrequestComment, error) {
	var comments []bitbucket.PullrequestComment
	var results bitbucket.PaginatedPullrequestComments
	var err error

	for {
		if results.Next == "" {
			results, _, err = c.PullrequestsApi.RepositoriesUsernameRepoSlugPullrequestsPullRequestIdCommentsGet(ctx, repo.Owner.Uuid, repo.Uuid, pr.Id)
		} else {
			results, _, err = c.PagingApi.PullrequestCommentsPageGet(ctx, results.Next)
		}
		if err != nil {
			return nil, err
		}

		comments = append(comments, results.Values...)
		if results.Next == "" {
			break
		}
	}

	return comments, nil
}

func getIssues(ctx context.Context, c *bitbucket.APIClient, repo bitbucket.Repository) ([]bitbucket.Issue, error) {
	var issues []bitbucket.Issue
	var results bitbucket.PaginatedIssues
	var err error

	for {
		if results.Next == "" {
			results, _, err = c.IssueTrackerApi.RepositoriesUsernameRepoSlugIssuesGet(ctx, repo.Owner.Uuid, repo.Uuid)
		} else {
			results, _, err = c.PagingApi.IssuesPageGet(ctx, results.Next)
		}
		if err != nil {
			return nil, err
		}

		issues = append(issues, results.Values...)
		if results.Next == "" {
			break
		}
	}

	return nil, nil
}

func getIssueComments(ctx context.Context, c *bitbucket.APIClient, repo bitbucket.Repository, issue bitbucket.Issue) ([]bitbucket.IssueComment, error) {
	var comments []bitbucket.IssueComment
	var results bitbucket.PaginatedIssueComments
	var err error

	for {
		if results.Next == "" {
			results, _, err = c.IssueTrackerApi.RepositoriesUsernameRepoSlugIssuesIssueIdCommentsGet(ctx, strconv.Itoa(int(issue.Id)), repo.Owner.Uuid, repo.Uuid, nil)
		} else {
			panic("pagination for issue is unsupported by go wrapper")
			//results, _, err = c.PagingApi.IssuesPageGet(ctx, results.Next)
		}
		if err != nil {
			return nil, err
		}

		comments = append(comments, results.Values...)
		if results.Next == "" {
			break
		}
	}

	return nil, nil
}

type taskStatus struct {
	Status string
	Phase  string
	Total  int
	Count  int
}

func addAuth(ctx context.Context, req *http.Request) {
	if ctx == nil {
		return
	}

	if auth, ok := ctx.Value(bitbucket.ContextBasicAuth).(bitbucket.BasicAuth); ok {
		req.SetBasicAuth(auth.UserName, auth.Password)
	}
}

// requires to be admin
func getIssuesFast(ctx context.Context, c *bitbucket.APIClient, repo bitbucket.Repository) ([]bitbucket.Issue, error) {
	resp, err := c.DefaultApi.RepositoriesUsernameRepoSlugIssuesExportPost(ctx, repo.Owner.Uuid, repo.Uuid)
	if err != nil {
		return nil, fmt.Errorf("export request: %v", err)
	}

	if resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("can't get issues: %d", resp.StatusCode)
	}

	url, err := resp.Location()
	if err != nil {
		return nil, err
	}

	for {
		req, err := http.NewRequest("GET", url.String(), nil)
		if err != nil {
			return nil, err
		}

		addAuth(ctx, req)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("export zip: %v", err)
		}
		if resp.StatusCode != http.StatusAccepted {
			return nil, fmt.Errorf("can't get issues task: %d", resp.StatusCode)
		}
		// copy response into memory because we might need to read it twice
		b, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		var taskStatus taskStatus
		err = json.Unmarshal(b, &taskStatus)
		if err == nil {
			fmt.Println("taskStatus", taskStatus)
			time.Sleep(time.Second)
			continue
		}

		r, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			return nil, err
		}
		for _, f := range r.File {
			fmt.Printf("Contents of %s:\n", f.Name)
			rc, err := f.Open()
			if err != nil {
				log.Fatal(err)
			}
			_, err = io.CopyN(os.Stdout, rc, 68)
			if err != nil {
				log.Fatal(err)
			}
			rc.Close()
			fmt.Println()
		}
		break
	}

	return nil, nil
}

func main() {

	owner := "Unity-Technologies"
	//owner := "smacker"

	ctx := context.Background()

	// this is how to use auth
	// basicAuth := bitbucket.BasicAuth{
	// 	UserName: "",
	// 	Password: "",
	// }
	// ctx := context.WithValue(context.Background(), bitbucket.ContextBasicAuth, basicAuth)

	c := bitbucket.NewAPIClient(bitbucket.NewConfiguration())
	repos, err := getRepositories(ctx, c, owner)
	if err != nil {
		panic(err)
	}

	for _, repo := range repos {
		prs, err := getPullRequests(ctx, c, repo)
		if err != nil && err != errUnavailable {
			panic(err)
		}

		for _, pr := range prs {
			comments, err := getPRComments(ctx, c, repo, pr)
			if err != nil {
				panic(err)
			}
			for _, comment := range comments {
				fmt.Println("comment", comment)
			}

			fmt.Println("pr", pr)
		}

		if repo.HasIssues {
			// returns Bad Request, need to investigate
			// _, err := getIssuesFast(ctx, c, repo)
			// if err != nil {
			// 	panic(err)
			// }

			issues, err := getIssues(ctx, c, repo)
			if err != nil {
				panic(err)
			}

			for _, issue := range issues {
				comments, err := getIssueComments(ctx, c, repo, issue)
				if err != nil {
					panic(err)
				}
				for _, comment := range comments {
					fmt.Println("comment", comment)
				}

				fmt.Println("issue", issue)
			}
		}

		fmt.Println("repo", repo)
	}
}
