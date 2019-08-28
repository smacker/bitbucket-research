# Bitbucket metadata downloader research

Atlassian has 3 products with the name Bitbucket.

1. Bitbucket Cloud aka https://bitbucket.org
2. Bitbucket Server
3. Bitbucket Data Center

Bitbucket Cloud and Bitbucket Server are completely different products which share only the name. I didn't test yet but Bitbucket Data Center looks just as distributed version of Bitbucket Server.

Because Cloud and Server are absolutely different products they provide different API. Which means we need to implement separate downloaders for them.

## Cloud

- Source-code: https://github.com/smacker/bitbucket-research/blob/master/cloud/main.go
- Swagger API defenitions: https://developer.atlassian.com/bitbucket/api/2/reference/resource/
- Go client: https://github.com/wbrefvem/go-bitbucket

### Entities

Objects we are interested in:

- Repositories
- Pull Requests
- Pull Request Comments
- Issues
- Issue Comments (broken, need fix in the go wrapper)
- Issues Export (doesn't work for me, maybe because none of the repos has issues)

FIXME: need to add teams/users

### Schema

```go
type Repository struct {
	Links *RepositoryLinks 
	// The repository's immutable id. This can be used as a substitute for the slug segment in URLs. Doing this guarantees your URLs will survive renaming of the repository by its owner, or even transfer of the repository to a different user.
	Uuid string 
	// The concatenation of the repository owner's username and the slugified name, e.g. \"evzijst/interruptingcow\". This is the same string used in Bitbucket URLs.
	FullName string 
	IsPrivate bool 
	Parent *Repository 
	Scm string 
	Owner *Account 
	Name string 
	Description string 
	CreatedOn time.Time 
	UpdatedOn time.Time 
	Size int32 
	Language string 
	HasIssues bool 
	HasWiki bool 
	// Controls the rules for forking this repository.  * **allow_forks**: unrestricted forking * **no_public_forks**: restrict forking to private forks (forks cannot   be made public later) * **no_forks**: deny all forking 
	ForkPolicy string 
	Project *Project 
	Mainbranch *Branch
}

type Pullrequest struct {
	Links *PullrequestLinks 
	// The pull request's unique ID. Note that pull request IDs are only unique within their associated repository.
	Id int32 
	// Title of the pull request.
	Title string 
	Rendered *PullrequestRendered 
	Summary *IssueContent 
	// The pull request's current status.
	State string 
	Author *Account 
	Source *PullrequestEndpoint 
	Destination *PullrequestEndpoint 
	MergeCommit *PullrequestMergeCommit 
	// The number of comments for a specific pull request.
	CommentCount int32 
	// The number of open tasks for a specific pull request.
	TaskCount int32 
	// A boolean flag indicating if merging the pull request closes the source branch.
	CloseSourceBranch bool 
	ClosedBy *Account 
	// Explains why a pull request was declined. This field is only applicable to pull requests in rejected state.
	Reason string 
	// The ISO8601 timestamp the request was created.
	CreatedOn time.Time 
	// The ISO8601 timestamp the request was last updated.
	UpdatedOn time.Time 
	// The list of users that were added as reviewers on this pull request when it was created. For performance reasons, the API only includes this list on a pull request's `self` URL.
	Reviewers []Account 
	// The list of users that are collaborating on this pull request.         Collaborators are user that:          * are added to the pull request as a reviewer (part of the reviewers           list)         * are not explicit reviewers, but have commented on the pull request         * are not explicit reviewers, but have approved the pull request          Each user is wrapped in an object that indicates the user's role and         whether they have approved the pull request. For performance reasons,         the API only returns this list when an API requests a pull request by         id.         
	Participants []Participant
}

type Participant struct {
	User *User 
	Role string 
	Approved bool 
	// The ISO8601 timestamp of the participant's action. For approvers, this is the time of their approval. For commenters and pull request reviewers who are not approvers, this is the time they last commented, or null if they have not commented.
	ParticipatedOn time.Time
}

type PullrequestComment struct {
	Id int32 
	CreatedOn time.Time 
	UpdatedOn time.Time 
	Content *IssueContent 
	User *User 
	Deleted bool 
	Parent *Comment 
	Inline *CommentInline 
	Links *CommentLinks 
	Pullrequest *Pullrequest
}

type Issue struct {
	Links *IssueLinks 
	Id int32 
	Repository *Repository 
	Title string 
	Reporter *User 
	Assignee *User 
	CreatedOn time.Time 
	UpdatedOn time.Time 
	EditedOn time.Time 
	State string 
	Kind string 
	Priority string 
	Milestone *Milestone 
	Version *Version 
	Component *Component 
	Votes int32 
	Content *IssueContent
}

type IssueComment struct {
	Id int32 
	CreatedOn time.Time 
	UpdatedOn time.Time 
	Content *IssueContent 
	User *User 
	Deleted bool 
	Parent *Comment 
	Inline *CommentInline 
	Links *CommentLinks 
	Issue *Issue
}
```

### Performance

Downloading all metadata from `Unity-Technologies` organization took 5 minutes on my machine.

Some stats:
```
$ cat output.txt | grep 'repo {' | wc -l
      39
$ cat output.txt | grep 'pr {' | wc -l
     507
$ cat output.txt | grep 'comment {' | wc -l
     610
$ cat output.txt | grep 'issue {' | wc -l
       0
```

### Notes

- Need to find some organization with issues for test
- Need to fix issues comments
- Need to check if issues export works (on org with issues)
- Go wrapper doesn't support changing perPage parameter
- Documentation doesn't say anything about request limits
- Different type of authorization are supported: OAuth2, Basic HTTP, AccessToken
- Simplest one is Basic HTTP which works similar to Github token

## Server

- Source-code: https://github.com/smacker/bitbucket-research/blob/master/server/main.go
- Swagger API defenitions: https://docs.atlassian.com/bitbucket-server/rest/6.6.0/bitbucket-rest.html
- Go client: https://github.com/gfleury/go-bitbucket-v1

### Entities

- Groups
- Users
- Projects
- Repositories
- Pull Requests
- Pull Requests Comments
- Pull Requests Tasks?

### Schema

```go
type Project struct {
	Key         string `json:"key"`
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Public      bool   `json:"public"`
	Type        string `json:"type"`
	Links       Links  `json:"links"`
}

type Repository struct {
	Slug          string  `json:"slug"`
	ID            int     `json:"id"`
	Name          string  `json:"name"`
	ScmID         string  `json:"scmId"`
	State         string  `json:"state"`
	StatusMessage string  `json:"statusMessage"`
	Forkable      bool    `json:"forkable"`
	Project       Project `json:"project"`
	Public        bool    `json:"public"`
	Links         struct {
		Clone []CloneLink `json:"clone"`
		Self  []SelfLink  `json:"self"`
	} `json:"links"`
}

type PullRequest struct {
	ID           int                `json:"id"`
	Version      int32              `json:"version"`
	Title        string             `json:"title"`
	Description  string             `json:"description"`
	State        string             `json:"state"`
	Open         bool               `json:"open"`
	Closed       bool               `json:"closed"`
	CreatedDate  int64              `json:"createdDate"`
	UpdatedDate  int64              `json:"updatedDate"`
	FromRef      PullRequestRef     `json:"fromRef"`
	ToRef        PullRequestRef     `json:"toRef"`
	Locked       bool               `json:"locked"`
	Author       UserWithMetadata   `json:"author"`
	Reviewers    []UserWithMetadata `json:"reviewers"`
	Participants []UserWithMetadata `json:"participants"`
	Properties   struct {
		MergeResult       MergeResult `json:"mergeResult"`
		ResolvedTaskCount int         `json:"resolvedTaskCount"`
		OpenTaskCount     int         `json:"openTaskCount"`
	} `json:"properties"`
	Links Links `json:"links"`
}

type UserWithMetadata struct {
	User     UserWithLinks `json:"user"`
	Role     string        `json:"role"`
	Approved bool          `json:"approved"`
	Status   string        `json:"status"`
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

type User struct {
	Name        string `json:"name"`
	Email       string `json:"emailAddress"`
	ID          int    `json:"id"`
	DisplayName string `json:"displayName"`
	Active      bool   `json:"active"`
	Slug        string `json:"slug"`
	Type        string `json:"type"`
}
```

### Performance

Bitbucket server accepts big numbers as perPage param. There are no request limits and downloader can be deployed close to the server. So it should be fast.

### Notes

- Go-wrapper doesn't have defined types for Activity or Comments
- Go wrapper doesn't support pagination for Activities endpoint
- Auth is similar to cloud one: OAuth2, Basic HTTP, AccessToken, APIKey
- Api has 2 endpoints for getting users, one works well but requires additional permissions, another doesn't support pagination
