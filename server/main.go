package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"

	bitbucketv1 "github.com/gfleury/go-bitbucket-v1"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/mitchellh/mapstructure"
	"github.com/wbrefvem/go-bitbucket"
)

const defaultLimit = 1000

func GetProjectsResponse(r *bitbucketv1.APIResponse) ([]bitbucketv1.Project, error) {
	var m []bitbucketv1.Project
	err := mapstructure.Decode(r.Values["values"], &m)
	return m, err
}

func GetPullRequestsResponse(r *bitbucketv1.APIResponse) ([]bitbucketv1.PullRequest, error) {
	var m []bitbucketv1.PullRequest
	err := mapstructure.Decode(r.Values["values"], &m)
	return m, err
}

func getProjects(c *bitbucketv1.APIClient) ([]bitbucketv1.Project, error) {
	var projects []bitbucketv1.Project

	start := 0
	for {
		resp, err := c.DefaultApi.GetProjects(map[string]interface{}{
			"limit": defaultLimit, "start": start})
		if err != nil {
			return nil, fmt.Errorf("projects req failed: %v", err)
		}
		projectsPerPage, err := GetProjectsResponse(resp)
		if err != nil {
			return nil, fmt.Errorf("projects decoding failed: %v", err)
		}
		projects = append(projects, projectsPerPage...)

		isLastPage := resp.Values["isLastPage"].(bool)
		if isLastPage {
			break
		}

		start = int(resp.Values["nextPageStart"].(float64))
	}

	return projects, nil
}

func getRepositories(c *bitbucketv1.APIClient, projectKey string) ([]bitbucketv1.Repository, error) {
	var repositories []bitbucketv1.Repository

	start := 0
	for {
		resp, err := c.DefaultApi.GetRepositoriesWithOptions(projectKey, map[string]interface{}{
			"limit": defaultLimit, "start": start})
		if err != nil {
			return nil, fmt.Errorf("repos req failed: %v", err)
		}
		pageRepos, err := bitbucketv1.GetRepositoriesResponse(resp)
		if err != nil {
			return nil, fmt.Errorf("repos decoding failed: %v", err)
		}
		repositories = append(repositories, pageRepos...)

		isLastPage := resp.Values["isLastPage"].(bool)
		if isLastPage {
			break
		}

		start = int(resp.Values["nextPageStart"].(float64))
	}

	return repositories, nil
}

func getPullRequests(c *bitbucketv1.APIClient, projectKey, repositorySlug string) ([]bitbucketv1.PullRequest, error) {
	var prs []bitbucketv1.PullRequest

	start := 0
	for {
		resp, err := c.DefaultApi.GetPullRequestsPage(projectKey, repositorySlug, map[string]interface{}{
			"limit": defaultLimit, "start": start, "state": "ALL"})
		if err != nil {
			return nil, fmt.Errorf("prs req failed: %v", err)
		}
		pagePRs, err := GetPullRequestsResponse(resp)
		if err != nil {
			return nil, fmt.Errorf("prs decoding failed: %v", err)
		}
		prs = append(prs, pagePRs...)

		isLastPage := resp.Values["isLastPage"].(bool)
		if isLastPage {
			break
		}

		start = int(resp.Values["nextPageStart"].(float64))
	}

	return prs, nil
}

type Commit struct {
	ID        string
	DisplayID string
	Message   string
	Author    bitbucket.User
	// AuthorTimestamp
	committer bitbucket.User
	// CommitterTimestamp
	// Parents
}

type Diff struct {
	Source      struct{}
	Destination struct{}
	Hunks       []struct {
		Segments []struct {
			Type  string
			Lines []struct {
				Destination int
				Source      int
				Line        string
			}
		}
	}
}

type DiffResp struct {
	Diffs []Diff
}

type PullRequest struct {
	bitbucketv1.PullRequest
	Commits        int
	ChangedFiles   int
	Additions      int
	Deletions      int
	Comments       int
	ReviewComments int

	ClosedAt int64
	MergedAt int64
	MergedBy bitbucketv1.User
}

func enrichPullRequest(c *bitbucketv1.APIClient, projectKey, repositorySlug string, pr bitbucketv1.PullRequest) (*PullRequest, error) {
	var commits []Commit
	start := 0
	for {
		resp, err := c.DefaultApi.GetPullRequestCommitsWithOptions(projectKey, repositorySlug, pr.ID, map[string]interface{}{
			"limit": defaultLimit, "start": start})
		if err != nil {
			return nil, fmt.Errorf("prs commits req failed: %v", err)
		}

		var pageCommits []Commit
		err = mapstructure.Decode(resp.Values["values"], &pageCommits)
		if err != nil {
			return nil, fmt.Errorf("prs commits decoding failed: %v", err)
		}
		commits = append(commits, pageCommits...)

		isLastPage := resp.Values["isLastPage"].(bool)
		if isLastPage {
			break
		}

		start = int(resp.Values["nextPageStart"].(float64))
	}

	resp, err := c.DefaultApi.GetPullRequestDiff(projectKey, repositorySlug, pr.ID, nil)
	if err != nil {
		return nil, fmt.Errorf("prs commits req failed: %v", err)
	}

	var diffResp DiffResp
	err = mapstructure.Decode(resp.Values, &diffResp)
	if err != nil {
		return nil, fmt.Errorf("prs diff decoding failed: %v", err)
	}

	var additions, deletions int
	for _, d := range diffResp.Diffs {
		for _, h := range d.Hunks {
			for _, s := range h.Segments {
				if s.Type == "ADDED" {
					additions += len(s.Lines)
				}
				if s.Type == "REMOVED" {
					deletions += len(s.Lines)
				}
			}
		}
	}

	return &PullRequest{
		PullRequest:  pr,
		Commits:      len(commits),
		ChangedFiles: len(diffResp.Diffs),
		Additions:    additions,
		Deletions:    deletions,
	}, nil
}

type Comment struct {
	ID          int
	Text        string
	Author      bitbucketv1.User
	CreatedDate int64
	UpdatedDate int64
	Comments    []Comment
	// tasks
}

type Review struct {
	ID          int
	State       string
	User        bitbucketv1.User
	CreatedDate int64
}

type PRStateUpdate struct {
	State string
	User  bitbucketv1.User
	Date  int64
}

type CommentAnchor struct {
	Line     int
	LineType string
	FileType string
	Path     string
	SrcPath  string
	FromHash string
	ToHash   string
}

type Activity struct {
	ID          int
	CreatedDate int64
	User        bitbucketv1.User
	Action      string
	// fields below are only for comments
	CommentAction string
	Comment       Comment
	// fields below are only for comments in code
	CommentAnchor *CommentAnchor
	Diff          *struct {
		Hunks []struct {
			DestinationLine int
			DestinationSpan int
			SourceLine      int
			SourceSpan      int
			// segments
		}
	}
}

type DiffComment struct {
	Comment
	CommentAnchor
}

func GetActivitiesResponse(r *bitbucketv1.APIResponse) ([]Activity, error) {
	var m []Activity
	err := mapstructure.Decode(r.Values["values"], &m)
	return m, err
}

func expandComment(c Comment) []Comment {
	comments := []Comment{c}
	for _, cc := range c.Comments {
		comments = append(comments, expandComment(cc)...)
	}

	return comments
}

func expandDiffComment(c Comment, a CommentAnchor) []DiffComment {
	comments := []DiffComment{DiffComment{
		Comment:       c,
		CommentAnchor: a,
	}}
	for _, cc := range c.Comments {
		comments = append(comments, expandDiffComment(cc, a)...)
	}

	return comments
}

func getPRActivity(c *bitbucketv1.APIClient, projectKey, repositorySlug string, pullRequestID int) ([]Comment, []DiffComment, []Review, *PRStateUpdate, error) {
	var comments []Comment
	var diffComments []DiffComment
	var reviews []Review
	var state *PRStateUpdate

	start := 0
	for {
		resp, err := c.DefaultApi.GetPullRequestActivity(projectKey, repositorySlug, pullRequestID, map[string]interface{}{
			"limit": defaultLimit, "start": start,
		})
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("activities req failed: %v", err)
		}

		pageActivities, err := GetActivitiesResponse(resp)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("activities decoding failed: %v", err)
		}

		for _, a := range pageActivities {
			switch a.Action {
			case "COMMENTED":
				if a.CommentAction != "ADDED" {
					continue
				}
				if a.CommentAnchor != nil {
					diffComments = append(diffComments, expandDiffComment(a.Comment, *a.CommentAnchor)...)
				} else {
					comments = append(comments, expandComment(a.Comment)...)
				}

			case "APPROVED":
				reviews = append(reviews, Review{
					ID:          a.ID,
					State:       "APPROVED",
					User:        a.User,
					CreatedDate: a.CreatedDate,
				})
			case "REVIEWED":
				reviews = append(reviews, Review{
					ID:          a.ID,
					State:       "CHANGES_REQUESTED",
					User:        a.User,
					CreatedDate: a.CreatedDate,
				})
			case "MERGED":
				state = &PRStateUpdate{
					State: "MERGED",
					User:  a.User,
					Date:  a.CreatedDate,
				}
			case "DECLINED":
				state = &PRStateUpdate{
					State: "CLOSED",
					User:  a.User,
					Date:  a.CreatedDate,
				}
			}

		}
		isLastPage := resp.Values["isLastPage"].(bool)
		if isLastPage {
			break
		}

		start = int(resp.Values["nextPageStart"].(float64))
	}

	return comments, diffComments, reviews, state, nil
}

func GetUsersResponse(r *bitbucketv1.APIResponse) ([]bitbucketv1.User, error) {
	var m []bitbucketv1.User
	err := mapstructure.Decode(r.Values["values"], &m)
	return m, err
}

func getUsers(c *bitbucketv1.APIClient) ([]bitbucketv1.User, error) {
	var users []bitbucketv1.User

	start := 0
	for {
		resp, err := c.DefaultApi.GetUsers(map[string]interface{}{
			"limit": defaultLimit, "start": start})
		if err != nil {
			return nil, fmt.Errorf("users req failed: %v", err)
		}
		pageUsers, err := GetUsersResponse(resp)
		if err != nil {
			return nil, fmt.Errorf("users decoding failed: %v", err)
		}
		users = append(users, pageUsers...)

		isLastPage := resp.Values["isLastPage"].(bool)
		if isLastPage {
			break
		}

		start = int(resp.Values["nextPageStart"].(float64))
	}
	return users, nil
}

func Migrate(databaseURL string) error {
	m, err := migrate.New("file://migrations", databaseURL)
	if err != nil {
		return err
	}
	return m.Up()
}

func run() error {
	// postgres://smacker@127.0.0.1:5432/bbserver?sslmode=disable
	connStr := os.Getenv("DB")
	// http://localhost:7990/rest
	basePath := os.Getenv("BASE_PATH")
	// admin
	login := os.Getenv("LOGIN")
	// admin
	pass := os.Getenv("PASSWORD")

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return err
	}

	if err = db.Ping(); err != nil {
		return err
	}

	if err = Migrate(connStr); err != nil && err != migrate.ErrNoChange {
		return err
	}

	storer := DB{DB: db}
	storer.Version(1)

	err = storer.Begin()
	if err != nil {
		return fmt.Errorf("could not call Begin(): %v", err)
	}

	basicAuth := bitbucketv1.BasicAuth{UserName: login, Password: pass}
	ctx := context.WithValue(context.Background(), bitbucketv1.ContextBasicAuth, basicAuth)
	cfg := bitbucketv1.NewConfiguration(basePath)
	cfg.HTTPClient = &http.Client{
		Transport: &retryTransport{http.DefaultTransport},
	}
	c := bitbucketv1.NewAPIClient(ctx, cfg)

	projects, err := getProjects(c)
	if err != nil {
		return err
	}

	for _, project := range projects {
		if err := storer.SaveOrganization(project); err != nil {
			return err
		}

		repos, err := getRepositories(c, project.Key)
		if err != nil {
			return err
		}

		for _, repo := range repos {
			if err := storer.SaveRepository(project, repo); err != nil {
				return err
			}

			prs, err := getPullRequests(c, project.Key, repo.Slug)
			if err != nil {
				return err
			}

			for _, pr := range prs {
				epr, err := enrichPullRequest(c, project.Key, repo.Slug, pr)
				if err != nil {
					return err
				}

				comments, diffComments, reviews, stateUpdate, err := getPRActivity(c, project.Key, repo.Slug, pr.ID)
				if err != nil {
					return err
				}

				epr.Comments = len(comments)
				epr.ReviewComments = len(reviews)
				if stateUpdate != nil {
					if stateUpdate.State == "MERGED" {
						epr.MergedAt = stateUpdate.Date
						epr.MergedBy = stateUpdate.User
					} else if stateUpdate.State == "CLOSED" {
						epr.ClosedAt = stateUpdate.Date
					}
				}

				if err := storer.SavePullRequest(project.Key, repo.Slug, *epr); err != nil {
					return err
				}

				for _, comment := range comments {
					if err := storer.SavePullRequestComment(project.Key, repo.Slug, pr.ID, comment); err != nil {
						return err
					}
				}

				for _, comment := range diffComments {
					if err := storer.SavePullRequestReviewComment(project.Key, repo.Slug, pr.ID, comment); err != nil {
						return err
					}
				}

				for _, review := range reviews {
					if err := storer.SavePullRequestReview(project.Key, repo.Slug, pr.ID, review); err != nil {
						return err
					}
				}
			}
		}
	}

	users, err := getUsers(c)
	if err != nil {
		return err
	}
	for _, user := range users {
		if err := storer.SaveUser(user); err != nil {
			return err
		}
	}

	if err := storer.SetActiveVersion(1); err != nil {
		return err
	}

	return storer.Commit()
}

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}
