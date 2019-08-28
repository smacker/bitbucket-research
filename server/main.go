package main

import (
	"context"
	"fmt"

	bitbucketv1 "github.com/gfleury/go-bitbucket-v1"
	"github.com/mitchellh/mapstructure"
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
			"limit": defaultLimit, "start": start})
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

type Comment struct {
	ID          int
	Text        string
	Author      bitbucketv1.User
	CreatedDate int64
	UpdatedDate int64
	Comments    []Comment
	// tasks
}

type Activity struct {
	ID            int
	CreatedDate   int64
	User          bitbucketv1.User
	Action        string
	CommentAction string
	Comment       Comment
	//commentAnchor - for comments in code
	//diff - for comments in code
}

func GetActivitiesResponse(r *bitbucketv1.APIResponse) ([]Activity, error) {
	var m []Activity
	err := mapstructure.Decode(r.Values["values"], &m)
	return m, err
}

// API provides special endpoint for getting only comments but
// c.DefaultApi.GetComments_7 is broken, it doesn't replace values in URL
// and API also requires `path` param which is a path of a file
// which makes it impossible to use to retrive general comments
//
// Pagination isn't supported by go wrapper, will return only 25 entities
func getPRComments(c *bitbucketv1.APIClient, projectKey, repositorySlug string, pullRequestID int) ([]Comment, error) {
	var comments []Comment

	// start := 0
	// for {
	resp, err := c.DefaultApi.GetPullRequestActivity(projectKey, repositorySlug, pullRequestID)
	//map[string]interface{}{"limit": defaultLimit, "start": start}
	if err != nil {
		return nil, fmt.Errorf("activities req failed: %v", err)
	}
	pageActivities, err := GetActivitiesResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("activities decoding failed: %v", err)
	}

	for _, a := range pageActivities {
		// get only original versions of comments, can be improved
		if a.Action != "COMMENTED" || a.CommentAction != "ADDED" {
			continue
		}
		comments = append(comments, a.Comment)
	}
	// isLastPage := resp.Values["isLastPage"].(bool)
	// if isLastPage {
	// 	break
	// }

	//start = int(resp.Values["nextPageStart"].(float64))
	//}

	return comments, nil
}

func GetUsersResponse(r *bitbucketv1.APIResponse) ([]bitbucketv1.User, error) {
	var m []bitbucketv1.User
	err := mapstructure.Decode(r.Values["values"], &m)
	return m, err
}

// GetUsers() calls /api/1.0/admin/users which requires addtional permission
// GetUsers_26() calls /api/1.0/users but doesn't support pagination
func getUsers_(c *bitbucketv1.APIClient) ([]bitbucketv1.User, error) {
	var users []bitbucketv1.User
	// ctx param is unused
	resp, err := c.DefaultApi.GetUsers_26(nil)
	if err != nil {
		return nil, fmt.Errorf("users req failed: %v", err)
	}
	users, err = GetUsersResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("users decoding failed: %v", err)
	}

	return users, nil
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

type Group struct {
	Name string
}

func GetGroupsResponse(r *bitbucketv1.APIResponse) ([]Group, error) {
	var m []Group
	err := mapstructure.Decode(r.Values["values"], &m)
	return m, err
}

func getGroups(c *bitbucketv1.APIClient) ([]Group, error) {
	var groups []Group

	start := 0
	for {
		resp, err := c.DefaultApi.GetGroups(map[string]interface{}{
			"limit": defaultLimit, "start": start})
		if err != nil {
			return nil, fmt.Errorf("groups req failed: %v", err)
		}
		pageGroups, err := GetGroupsResponse(resp)
		if err != nil {
			return nil, fmt.Errorf("groups decoding failed: %v", err)
		}
		groups = append(groups, pageGroups...)

		isLastPage := resp.Values["isLastPage"].(bool)
		if isLastPage {
			break
		}

		start = int(resp.Values["nextPageStart"].(float64))
	}

	return groups, nil
}

func main() {
	basicAuth := bitbucketv1.BasicAuth{UserName: "admin", Password: "admin"}
	ctx := context.WithValue(context.Background(), bitbucketv1.ContextBasicAuth, basicAuth)
	cfg := bitbucketv1.NewConfiguration("http://localhost:7990/rest")
	c := bitbucketv1.NewAPIClient(ctx, cfg)

	projects, err := getProjects(c)
	if err != nil {
		panic(err)
	}
	for _, project := range projects {
		repos, err := getRepositories(c, project.Key)
		if err != nil {
			panic(err)
		}

		for _, repo := range repos {
			prs, err := getPullRequests(c, project.Key, repo.Slug)
			if err != nil {
				panic(err)
			}

			for _, pr := range prs {
				comments, err := getPRComments(c, project.Key, repo.Slug, pr.ID)
				if err != nil {
					panic(err)
				}

				for _, comment := range comments {
					fmt.Printf("%+v\n", comment)
				}

				fmt.Printf("%+v\n", pr)
			}

			fmt.Printf("%+v\n", repo)
		}

		fmt.Printf("%+v\n", project)
	}

	users, err := getUsers(c)
	if err != nil {
		panic(err)
	}
	for _, user := range users {
		fmt.Printf("%+v\n", user)
	}

	groups, err := getGroups(c)
	if err != nil {
		panic(err)
	}
	for _, group := range groups {
		fmt.Printf("%+v\n", group)
	}
}
