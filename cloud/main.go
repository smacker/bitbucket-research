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
	"strings"
	"time"

	"github.com/go-xorm/xorm"
	_ "github.com/lib/pq"
	"github.com/wbrefvem/go-bitbucket"
	"xorm.io/core"
)

const (
	retries  = 10
	delay    = 10 * time.Millisecond
	truncate = 10 * time.Second
)

func Retry(f func() error) error {
	d := delay
	var i uint

	for ; ; i++ {
		err := f()
		if err == nil {
			return nil
		}

		if i == retries {
			return err
		}

		fmt.Println(err, "retrying in %v", d)
		time.Sleep(d)

		d = d * (1<<i + 1)
		if d > truncate {
			d = truncate
		}
	}
}

type RetryTransport struct {
	T http.RoundTripper
}

func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var r *http.Response
	var err error
	Retry(func() error {
		r, err = t.T.RoundTrip(req)
		return err
	})

	return r, err
}

// FIXME looks like all endpoints support pageSize but they aren't exposed in go-wrapper

type Repository struct {
	// The repository's immutable id. This can be used as a substitute for the slug segment in URLs. Doing this guarantees your URLs will survive renaming of the repository by its owner, or even transfer of the repository to a different user.
	Uuid string `xorm:"uuid"`
	// The concatenation of the repository owner's username and the slugified name, e.g. \"evzijst/interruptingcow\". This is the same string used in Bitbucket URLs.
	FullName  string
	IsPrivate bool
	//ParentID *Repository `xorm:"parent_id"`
	Scm           string
	OwnerUsername string
	OwnerNickname string
	OwnerUuid     string
	Name          string
	Description   string `xorm:"text"`
	CreatedOn     time.Time
	UpdatedOn     time.Time
	Size          int32
	Language      string
	HasIssues     bool
	HasWiki       bool
	//  Controls the rules for forking this repository.  * **allow_forks**: unrestricted forking * **no_public_forks**: restrict forking to private forks (forks cannot   be made public later) * **no_forks**: deny all forking
	ForkPolicy string

	ProjectUuid          string
	ProjectKey           string
	ProjectOwnerUsername string
	ProjectOwnerNickname string
	ProjectOwnerUuid     string
	ProjectName          string

	MainbranchName       string
	MainbranchTargetHash string
}

func getRepositories(ctx context.Context, c *bitbucket.APIClient, owner string) ([]Repository, error) {
	var repos []Repository
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

		for _, r := range results.Values {
			repo := Repository{
				Uuid:          r.Uuid[1 : len(r.Uuid)-1],
				FullName:      r.FullName,
				IsPrivate:     r.IsPrivate,
				Scm:           r.Scm,
				OwnerUsername: r.Owner.Username,
				OwnerNickname: r.Owner.Nickname,
				Name:          r.Name,
				Description:   r.Description,
				CreatedOn:     r.CreatedOn,
				UpdatedOn:     r.UpdatedOn,
				Size:          r.Size,
				Language:      r.Language,
				HasIssues:     r.HasIssues,
				HasWiki:       r.HasWiki,
				ForkPolicy:    r.ForkPolicy,
				ProjectUuid:   r.Project.Uuid[1 : len(r.Project.Uuid)-1],
				ProjectKey:    r.Project.Key,
				ProjectName:   r.Project.Name,
			}
			if len(r.Owner.Uuid) > 0 {
				repo.OwnerUuid = r.Owner.Uuid[1 : len(r.Owner.Uuid)-1]
			}
			if r.Project.Owner != nil {
				repo.ProjectOwnerUsername = r.Project.Owner.Username
				repo.ProjectOwnerNickname = r.Project.Owner.Nickname
				if len(r.Project.Owner.Uuid) > 0 {
					repo.ProjectOwnerUuid = r.Project.Owner.Uuid[1 : len(r.Project.Owner.Uuid)-1]
				}
			}
			if r.Mainbranch != nil && r.Mainbranch.Target != nil {
				repo.MainbranchTargetHash = r.Mainbranch.Target.Hash
			}

			repos = append(repos, repo)
		}
		if results.Next == "" {
			break
		}
	}

	return repos, nil
}

var errUnavailable = fmt.Errorf("resource is unavailable")

type Pullrequest struct {
	// The pull request's unique ID. Note that pull request IDs are only unique within their associated repository.
	ID int32 `xorm:"id"`
	// Title of the pull request.
	Title string `xorm:"text"`

	// Rendered *PullrequestRendered `json:"rendered,omitempty"`
	// Title *IssueContent `json:"title,omitempty"`
	// Description *IssueContent `json:"description,omitempty"`
	// Reason *IssueContent `json:"reason,omitempty"`

	Summary string `xorm:"text"`

	// The pull request's current status.
	State string

	AuthorUsername string
	AuthorNickname string
	AuthorUuid     string

	SourceRepositoryUuid     string
	SourceRepositoryFullName string
	SourceRepositoryOwner    string
	SourceRepositoryName     string
	SourceBranchName         string
	SourceCommitHash         string

	DestinationRepositoryUuid     string
	DestinationRepositoryFullName string
	DestinationRepositoryOwner    string
	DestinationRepositoryName     string
	DestinationBranchName         string
	DestinationCommitHash         string

	MergeCommitHash string

	// The number of comments for a specific pull request.
	CommentCount int32

	// The number of open tasks for a specific pull request.
	TaskCount int32

	// A boolean flag indicating if merging the pull request closes the source branch.
	CloseSourceBranch bool

	ClosedByUsername string
	ClosedByNickname string
	ClosedByUuid     string

	// Explains why a pull request was declined. This field is only applicable to pull requests in rejected state.
	Reason string `xorm:"text"`

	// The ISO8601 timestamp the request was created.
	CreatedOn time.Time

	// The ISO8601 timestamp the request was last updated.
	UpdatedOn time.Time
}

func getPullRequests(ctx context.Context, c *bitbucket.APIClient, repo Repository) ([]Pullrequest, error) {
	var prs []Pullrequest
	var results bitbucket.PaginatedPullrequests
	var resp *http.Response
	var err error

	// REST API supports passing multiple states to request but Go wrapper doesn't
	states := []string{"OPEN", "MERGED", "SUPERSEDED", "DECLINED"}
	for _, state := range states {
		for {
			if results.Next == "" {
				results, resp, err = c.PullrequestsApi.RepositoriesUsernameRepoSlugPullrequestsGet(ctx, "{"+repo.OwnerUuid+"}", "{"+repo.Uuid+"}", map[string]interface{}{
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

			for _, pr := range results.Values {
				p := Pullrequest{
					ID:                pr.Id,
					Title:             pr.Title,
					Summary:           pr.Summary.Raw,
					State:             pr.State,
					CommentCount:      pr.CommentCount,
					TaskCount:         pr.TaskCount,
					CloseSourceBranch: pr.CloseSourceBranch,
					Reason:            pr.Reason,
					CreatedOn:         pr.CreatedOn,
					UpdatedOn:         pr.UpdatedOn,
				}
				if pr.Author != nil {
					p.AuthorUsername = pr.Author.Username
					p.AuthorNickname = pr.Author.Nickname
					p.AuthorUuid = pr.Author.Uuid[1 : len(pr.Author.Uuid)-1]
				}
				if pr.Source != nil {
					if pr.Source.Repository != nil {
						p.SourceRepositoryUuid = pr.Source.Repository.Uuid[1 : len(pr.Source.Repository.Uuid)-1]
						p.SourceRepositoryFullName = pr.Source.Repository.FullName
						p.SourceRepositoryName = pr.Source.Repository.Name
						if pr.Source.Repository.Owner != nil {
							p.SourceRepositoryOwner = pr.Source.Repository.Owner.Nickname
						}
					}
					if pr.Source.Branch != nil {
						p.SourceBranchName = pr.Source.Branch.Name
					}
					if pr.Source.Commit != nil {
						p.SourceCommitHash = pr.Source.Commit.Hash
					}
				}
				if pr.Destination != nil {
					if pr.Destination.Repository != nil {
						p.DestinationRepositoryUuid = pr.Destination.Repository.Uuid[1 : len(pr.Destination.Repository.Uuid)-1]
						p.DestinationRepositoryFullName = pr.Destination.Repository.FullName
						p.DestinationRepositoryName = pr.Destination.Repository.Name
						if pr.Destination.Repository.Owner != nil {
							p.DestinationRepositoryOwner = pr.Destination.Repository.Owner.Nickname
						}
					}
					if pr.Destination.Branch != nil {
						p.DestinationBranchName = pr.Destination.Branch.Name
					}
					if pr.Destination.Commit != nil {
						p.DestinationCommitHash = pr.Destination.Commit.Hash
					}
				}
				if pr.MergeCommit != nil {
					p.MergeCommitHash = pr.MergeCommit.Hash
				}
				if pr.ClosedBy != nil {
					p.ClosedByUsername = pr.ClosedBy.Username
					p.ClosedByNickname = pr.ClosedBy.Nickname
					p.ClosedByUuid = pr.ClosedBy.Uuid[1 : len(pr.ClosedBy.Uuid)-1]
				}

				prs = append(prs, p)
			}
			if results.Next == "" {
				break
			}
		}
	}

	return prs, nil
}

type PullrequestReview struct {
	RepositoryFullName string
	Pullrequest        int32
	AuthorUsername     string
	AuthorNickname     string
	AuthorUuid         string
	Role               string
	Approved           bool
	ParticipatedOn     time.Time
}

func getPullRequestParticipants(ctx context.Context, c *bitbucket.APIClient, repo Repository, pr Pullrequest) ([]PullrequestReview, error) {
	resp, _, err := c.PullrequestsApi.RepositoriesUsernameRepoSlugPullrequestsPullRequestIdGet(ctx, "{"+repo.OwnerUuid+"}", "{"+repo.Uuid+"}", pr.ID)
	if err != nil {
		return nil, err
	}

	prrs := make([]PullrequestReview, len(resp.Participants))
	for i, p := range resp.Participants {
		prrs[i] = PullrequestReview{
			RepositoryFullName: repo.FullName,
			Pullrequest:        pr.ID,
			AuthorUsername:     p.User.Username,
			AuthorNickname:     p.User.Nickname,
			AuthorUuid:         p.User.Uuid[1 : len(p.User.Uuid)-1],
			Role:               p.Role,
			Approved:           p.Approved,
			ParticipatedOn:     p.ParticipatedOn,
		}
	}

	return prrs, nil
}

type PullrequestComment struct {
	RepositoryFullName string
	ID                 int32 `xorm:"id"`
	CreatedOn          time.Time
	UpdatedOn          time.Time
	Content            string `xorm:"text"`
	Username           string
	Nickname           string
	UserUUID           string `xorm:"user_uuid"`
	Deleted            bool
	ParentID           int32 `xorm:"parent_uuid"`
	// The comment's anchor line in the new version of the file.
	To int32
	// The comment's anchor line in the old version of the file.
	From int32
	// The path of the file this comment is anchored to.
	Path        string
	Pullrequest int32
}

func getPRComments(ctx context.Context, c *bitbucket.APIClient, repo Repository, pr Pullrequest) ([]PullrequestComment, error) {
	var comments []PullrequestComment
	var results bitbucket.PaginatedPullrequestComments
	var err error

	for {
		if results.Next == "" {
			results, _, err = c.PullrequestsApi.RepositoriesUsernameRepoSlugPullrequestsPullRequestIdCommentsGet(ctx, "{"+repo.OwnerUuid+"}", "{"+repo.Uuid+"}", pr.ID)
		} else {
			results, _, err = c.PagingApi.PullrequestCommentsPageGet(ctx, results.Next)
		}
		if err != nil {
			return nil, err
		}

		for _, c := range results.Values {
			comment := PullrequestComment{
				RepositoryFullName: repo.FullName,
				ID:                 c.Id,
				CreatedOn:          c.CreatedOn,
				UpdatedOn:          c.UpdatedOn,
				Content:            c.Content.Raw,
				Username:           c.User.Username,
				Nickname:           c.User.Nickname,
				UserUUID:           c.User.Uuid[1 : len(c.User.Uuid)-1],
				Deleted:            c.Deleted,
				Pullrequest:        c.Pullrequest.Id,
			}
			if c.Parent != nil {
				comment.ParentID = c.Parent.Id
			}
			if c.Inline != nil {
				comment.To = c.Inline.To
				comment.From = c.Inline.From
				comment.Path = c.Inline.Path
			}

			comments = append(comments, comment)
		}
		if results.Next == "" {
			break
		}
	}

	return comments, nil
}

type PullrequestCommit struct {
	RepositoryFullName string
	Pullrequest        int32
	Hash               string
	Message            string `xorm:"text"`
}

type prCommitResp struct {
	Values []struct {
		Hash    string
		Message string
	}
}

func getPRCommits(ctx context.Context, c *bitbucket.APIClient, repo Repository, pr Pullrequest) ([]PullrequestCommit, error) {
	// lib can't return more than 10 commits because of pagination
	url := "https://api.bitbucket.org/2.0/repositories/{username}/{repo_slug}/pullrequests/{pull_request_id}/commits?pagelen=100"
	url = strings.Replace(url, "{username}", fmt.Sprintf("%v", "{"+repo.OwnerUuid+"}"), -1)
	url = strings.Replace(url, "{pull_request_id}", fmt.Sprintf("%v", pr.ID), -1)
	url = strings.Replace(url, "{repo_slug}", fmt.Sprintf("%v", "{"+repo.Uuid+"}"), -1)
	r, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	if r.StatusCode >= 400 {
		// don't fail ever
		fmt.Printf("%s returned status code %d\n", url, r.StatusCode)
		return nil, nil
	}

	var dr prCommitResp
	d := json.NewDecoder(r.Body)
	if err := d.Decode(&dr); err != nil {
		return nil, err
	}

	var commits []PullrequestCommit
	for _, v := range dr.Values {
		commits = append(commits, PullrequestCommit{
			RepositoryFullName: repo.FullName,
			Pullrequest:        pr.ID,
			Hash:               v.Hash,
			Message:            v.Message,
		})
	}

	return commits, nil
}

type diffResp struct {
	Values []struct {
		LinesAdded   int `json:"lines_added"`
		LinesRemoved int `json:"lines_removed"`
	}
}

type DiffStat struct {
	RepositoryFullName string
	Pullrequest        int32
	LinesAdded         int
	LinesRemoved       int
}

func getPRDiff(ctx context.Context, c *bitbucket.APIClient, repo Repository, pr Pullrequest) (*DiffStat, error) {
	// the lib does something weird with response, body can't be read
	url := "https://api.bitbucket.org/2.0/repositories/{username}/{repo_slug}/pullrequests/{pull_request_id}/diffstat"
	url = strings.Replace(url, "{username}", fmt.Sprintf("%v", "{"+repo.OwnerUuid+"}"), -1)
	url = strings.Replace(url, "{pull_request_id}", fmt.Sprintf("%v", pr.ID), -1)
	url = strings.Replace(url, "{repo_slug}", fmt.Sprintf("%v", "{"+repo.Uuid+"}"), -1)
	r, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	if r.StatusCode >= 400 {
		// don't fail ever
		fmt.Printf("%s returned status code %d\n", url, r.StatusCode)
		return nil, nil
	}

	var dr diffResp
	d := json.NewDecoder(r.Body)
	if err := d.Decode(&dr); err != nil {
		return nil, err
	}

	result := DiffStat{
		RepositoryFullName: repo.FullName,
		Pullrequest:        pr.ID,
	}
	for _, v := range dr.Values {
		result.LinesAdded += v.LinesAdded
		result.LinesRemoved += v.LinesRemoved
	}

	return &result, nil
}

type Issue struct {
	Id                 int32
	RepositoryUuid     string
	RepositoryFullName string
	RepositoryOwner    string
	RepositoryName     string
	Title              string
	ReporterUsername   string
	ReporterNickname   string
	ReporterUuid       string
	AssigneeUsername   string
	AssigneeNickname   string
	AssigneeUuid       string
	CreatedOn          time.Time
	UpdatedOn          time.Time
	EditedOn           time.Time
	State              string
	Kind               string
	Priority           string
	MilestoneName      string
	VersionName        string
	ComponentName      string
	Votes              int32
	Content            string `xorm:"text"`
}

func getIssues(ctx context.Context, c *bitbucket.APIClient, repo Repository) ([]Issue, error) {
	var issues []Issue
	var results bitbucket.PaginatedIssues
	var err error

	for {
		if results.Next == "" {
			results, _, err = c.IssueTrackerApi.RepositoriesUsernameRepoSlugIssuesGet(ctx, "{"+repo.OwnerUuid+"}", "{"+repo.Uuid+"}")
		} else {
			results, _, err = c.PagingApi.IssuesPageGet(ctx, results.Next)
		}
		if err != nil {
			return nil, err
		}

		for _, i := range results.Values {
			issue := Issue{
				Id:                 i.Id,
				RepositoryUuid:     i.Repository.Uuid[1 : len(i.Repository.Uuid)-1],
				RepositoryFullName: i.Repository.FullName,
				RepositoryName:     i.Repository.Name,
				Title:              i.Title,
				CreatedOn:          i.CreatedOn,
				UpdatedOn:          i.UpdatedOn,
				EditedOn:           i.EditedOn,
				State:              i.State,
				Kind:               i.Kind,
				Priority:           i.Priority,
				Votes:              i.Votes,
				Content:            i.Content.Raw,
			}
			if i.Repository.Owner != nil {
				issue.RepositoryOwner = i.Repository.Owner.Nickname
			}
			if i.Reporter != nil {
				issue.ReporterUsername = i.Reporter.Username
				issue.ReporterNickname = i.Reporter.Nickname
				issue.ReporterUuid = i.Reporter.Uuid[1 : len(i.Reporter.Uuid)-1]
			}
			if i.Assignee != nil {
				issue.AssigneeUsername = i.Assignee.Username
				issue.AssigneeNickname = i.Assignee.Nickname
				issue.AssigneeUuid = i.Assignee.Uuid[1 : len(i.Assignee.Uuid)-1]
			}
			if i.Milestone != nil {
				issue.MilestoneName = i.Milestone.Name
			}
			if i.Version != nil {
				issue.VersionName = i.Version.Name
			}
			if i.Component != nil {
				issue.ComponentName = i.Component.Name
			}
			issues = append(issues, issue)
		}
		if results.Next == "" {
			break
		}
	}

	return issues, nil
}

func getIssueComments(ctx context.Context, c *bitbucket.APIClient, repo Repository, issue bitbucket.Issue) ([]bitbucket.IssueComment, error) {
	var comments []bitbucket.IssueComment
	var results bitbucket.PaginatedIssueComments
	var err error

	for {
		if results.Next == "" {
			results, _, err = c.IssueTrackerApi.RepositoriesUsernameRepoSlugIssuesIssueIdCommentsGet(ctx, strconv.Itoa(int(issue.Id)), "{"+repo.OwnerUuid+"}", "{"+repo.Uuid+"}", nil)
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

func getMembers(ctx context.Context, c *bitbucket.APIClient, owner string) ([]bitbucket.User, error) {
	// The methods is broken, it should return list of users but it returns one user
	//members, _, err := c.TeamsApi.TeamsUsernameMembersGet(ctx, owner)

	return nil, nil
}

func main() {

	// postgres://smacker@localhost/bb?sslmode=disable
	engine, err := xorm.NewEngine("postgres", os.Getenv("DB_CONNECT_STRING"))
	if err != nil {
		panic(err)
	}

	engine.SetMapper(core.GonicMapper{})
	// engine.ShowSQL(true)
	// engine.Logger().SetLevel(core.LOG_DEBUG)

	tables := []interface{}{
		new(Repository),
		new(Pullrequest),
		new(PullrequestReview),
		new(PullrequestComment),
		new(Issue),
		new(PullrequestCommit),
		new(DiffStat),
		//new(bitbucket.User),
	}

	for _, table := range tables {
		err := engine.Sync2(table)
		if err != nil {
			panic(err)
		}
	}

	owner := os.Getenv("ORGANIZATION") // Unity-Technologies
	//owner := "smacker"

	ctx := context.Background()

	// this is how to use auth
	// basicAuth := bitbucket.BasicAuth{
	// 	UserName: "",
	// 	Password: "",
	// }
	// ctx := context.WithValue(context.Background(), bitbucket.ContextBasicAuth, basicAuth)

	cfg := bitbucket.NewConfiguration()
	httpClient := http.Client{
		Transport: &RetryTransport{T: http.DefaultTransport},
	}
	cfg.HTTPClient = &httpClient
	c := bitbucket.NewAPIClient(cfg)

	// users, err := getMembers(ctx, c, owner)
	// if err != nil {
	// 	panic(err)
	// }
	// for _, user := range users {
	// 	_, err := engine.Insert(&user)
	// 	if err != nil {
	// 		panic(err)
	// 	}
	// 	fmt.Println("add user", user.Uuid)
	// }

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
			// commits, err := getPRCommits(ctx, c, repo, pr)
			// if err != nil {
			// 	panic(err)
			// }
			// for _, commit := range commits {
			// 	_, err = engine.Insert(&commit)
			// 	if err != nil {
			// 		panic(err)
			// 	}
			// 	fmt.Println("add pr commit")
			// }

			diffStat, err := getPRDiff(ctx, c, repo, pr)
			if err != nil {
				panic(err)
			}
			if diffStat != nil {
				_, err = engine.Insert(diffStat)
				if err != nil {
					panic(err)
				}
				fmt.Println("add pr diff stat")
			}

			// reviews, err := getPullRequestParticipants(ctx, c, repo, pr)
			// if err != nil {
			// 	panic(err)
			// }
			// for _, review := range reviews {
			// 	_, err = engine.Insert(&review)
			// 	if err != nil {
			// 		panic(err)
			// 	}
			// 	fmt.Println("add pr review")
			// }

			// comments, err := getPRComments(ctx, c, repo, pr)
			// if err != nil {
			// 	panic(err)
			// }
			// for _, comment := range comments {
			// 	_, err = engine.Insert(&comment)
			// 	if err != nil {
			// 		panic(err)
			// 	}
			// 	fmt.Println("add pr comment", comment.ID)
			// }

			// _, err = engine.Insert(&pr)
			// if err != nil {
			// 	panic(err)
			// }
			// fmt.Println("add pr", pr.ID)
		}

		// if repo.HasIssues {
		// 	// returns Bad Request, need to investigate
		// 	// _, err := getIssuesFast(ctx, c, repo)
		// 	// if err != nil {
		// 	// 	panic(err)
		// 	// }

		// 	issues, err := getIssues(ctx, c, repo)
		// 	if err != nil {
		// 		panic(err)
		// 	}

		// 	for _, issue := range issues {
		// 		// returns only first page and we don't really need it
		// 		// comments, err := getIssueComments(ctx, c, repo, issue)
		// 		// if err != nil {
		// 		// 	panic(err)
		// 		// }
		// 		// for _, comment := range comments {
		// 		// 	_, err = engine.Insert(&comment)
		// 		// 	if err != nil {
		// 		// 		panic(err)
		// 		// 	}
		// 		// 	fmt.Println("add issue comment", comment.Id)
		// 		// }

		// 		_, err = engine.Insert(&issue)
		// 		if err != nil {
		// 			panic(err)
		// 		}
		// 		fmt.Println("add issue", issue.Id)
		// 	}
		// }

		// _, err = engine.Insert(&repo)
		// if err != nil {
		// 	panic(err)
		// }
		// fmt.Println("add repo", repo.Uuid)
	}
}
