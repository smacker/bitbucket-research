# Bitbucket metadata downloader research

Atlassian has 3 products with the name Bitbucket.

1. Bitbucket Cloud aka https://bitbucket.org
2. Bitbucket Server
3. Bitbucket Data Center

Bitbucket Cloud and Bitbucket Server are completely different products which share only the name. I didn't test yet but Bitbucket Data Center looks just as distributed version of Bitbucket Server.

Because Cloud and Server are absolutely different products they provide different API. Which means we need to implement separate downloaders for them.

## Cloud

Source-code: https://github.com/smacker/bitbucket-research/blob/master/cloud/main.go

### Entities

Objects we are interested in:

- Repositories
- Pull Requests
- Pull Request Comments
- Issues
- Issue Comments (broken, need fix in the go wrapper)
- Issues Export (doesn't work for me, maybe because none of the repos has issues)

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

Source-code:

### Entities

### Schema

### Performance
