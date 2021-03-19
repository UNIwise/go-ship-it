package scm

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/pkg/errors"

	semver "github.com/Masterminds/semver/v3"
	"github.com/google/go-github/v33/github"
)

type GithubClient interface {
	CreateMilestone(title string) (*github.Milestone, error)
	AddPRtoMilestone(pull *github.PullRequest, milestone *github.Milestone) error
	GetReleaseByTag(tag string) (*github.RepositoryRelease, error)
	DeleteRelease(r *github.RepositoryRelease) error
	DeleteTag(tag string) error
	EditRelease(id int64, release *github.RepositoryRelease) (*github.RepositoryRelease, error)
	GetRef(r string) (*github.Reference, error)
	CreateRef(r *github.Reference) error
	CreateRelease(r *github.RepositoryRelease) error
	GetRefs(pattern string) ([]*github.Reference, error)
	GetCommitRange(base, head string) (*github.CommitsComparison, error)
	GetPullsInCommitRange(commits []*github.RepositoryCommit) ([]*github.PullRequest, error)
	GetLatestTag() (tag string, ver *semver.Version, err error)
	GetRepo() Repo
	GetFile(ref, file string) (io.ReadCloser, error)
}

type Repo interface {
	GetFullName() string
	GetOwner() *github.User
	GetName() string
	GetDefaultBranch() string
}

type GithubClientImpl struct {
	client *github.Client
	repo   Repo
}

func NewGithubClient(tc *http.Client, repo Repo) *GithubClientImpl {
	cl := github.NewClient(tc)

	return &GithubClientImpl{
		client: cl,
		repo:   repo,
	}
}

func (c *GithubClientImpl) CreateMilestone(title string) (*github.Milestone, error) {
	milestone, _, err := c.client.Issues.CreateMilestone(context.TODO(), c.repo.GetOwner().GetLogin(), c.repo.GetName(), &github.Milestone{
		Title: github.String(title),
		State: github.String("closed"),
	})
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to create milestone '%s'", title)
	}
	return milestone, nil
}

func (c *GithubClientImpl) AddPRtoMilestone(pull *github.PullRequest, milestone *github.Milestone) error {
	_, _, err := c.client.Issues.Edit(context.TODO(), c.repo.GetOwner().GetLogin(), c.repo.GetName(), pull.GetNumber(), &github.IssueRequest{
		Milestone: milestone.Number,
	})
	if err != nil {
		return errors.Wrapf(err, "Failed to add pull request '%d' to milestone '%d'", pull.GetNumber(), milestone.GetNumber())
	}
	return nil
}

func (c *GithubClientImpl) GetReleaseByTag(tag string) (*github.RepositoryRelease, error) {
	release, _, err := c.client.Repositories.GetReleaseByTag(context.TODO(), c.repo.GetOwner().GetLogin(), c.repo.GetName(), tag)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to find release from tag '%s'", tag)
	}
	return release, nil
}

func (c *GithubClientImpl) DeleteRelease(release *github.RepositoryRelease) error {
	_, err := c.client.Repositories.DeleteRelease(context.TODO(), c.repo.GetOwner().GetLogin(), c.repo.GetName(), release.GetID())
	if err != nil {
		return errors.Wrapf(err, "Failed to delete release '%d'", release.GetID())
	}
	return nil
}

func (c *GithubClientImpl) DeleteTag(tag string) error {
	_, err := c.client.Git.DeleteRef(context.TODO(), c.repo.GetOwner().GetLogin(), c.repo.GetName(), fmt.Sprintf("refs/tags/%s", tag))
	if err != nil {
		return errors.Wrapf(err, "Failed to delete tag '%s'", tag)
	}
	return nil
}

func (c *GithubClientImpl) EditRelease(id int64, release *github.RepositoryRelease) (*github.RepositoryRelease, error) {
	r, _, err := c.client.Repositories.EditRelease(context.TODO(), c.repo.GetOwner().GetLogin(), c.repo.GetName(), id, release)
	return r, err
}

func (c *GithubClientImpl) GetRef(r string) (*github.Reference, error) {
	ref, _, err := c.client.Git.GetRef(context.TODO(), c.repo.GetOwner().GetLogin(), c.repo.GetName(), r)
	return ref, err
}

func (c *GithubClientImpl) CreateRef(r *github.Reference) error {
	_, _, err := c.client.Git.CreateRef(context.TODO(), c.repo.GetOwner().GetLogin(), c.repo.GetName(), r)
	return err
}

func (c *GithubClientImpl) CreateRelease(r *github.RepositoryRelease) error {
	_, _, err := c.client.Repositories.CreateRelease(context.TODO(), c.repo.GetOwner().GetLogin(), c.repo.GetName(), r)
	return err
}

func (c *GithubClientImpl) GetRefs(pattern string) ([]*github.Reference, error) {
	return c.paginateRefs(&github.ReferenceListOptions{Ref: pattern, ListOptions: github.ListOptions{PerPage: 25}})
}

func (c *GithubClientImpl) GetCommitRange(base, head string) (*github.CommitsComparison, error) {
	comparison, _, err := c.client.Repositories.CompareCommits(context.TODO(), c.repo.GetOwner().GetLogin(), c.repo.GetName(), base, head)
	if err != nil {
		return nil, err
	}
	return comparison, nil
}

func (c *GithubClientImpl) GetPullsInCommitRange(commits []*github.RepositoryCommit) ([]*github.PullRequest, error) {
	max := 100
	if len(commits) < max {
		max = len(commits)
	}
	unique := map[int]interface{}{}
	pulls := []*github.PullRequest{}
	for _, commit := range commits[:max] {
		prs, err := c.paginatePullsWithCommit(commit.GetSHA(), &github.PullRequestListOptions{})
		if err != nil {
			return nil, errors.Wrap(err, "Failed to paginate pull requests")
		}
		for _, p := range prs {
			if _, ok := unique[p.GetNumber()]; ok {
				continue
			}
			unique[p.GetNumber()] = struct{}{}
			pulls = append(pulls, p)
		}
	}
	return pulls, nil
}

func (c *GithubClientImpl) GetFile(ref, file string) (io.ReadCloser, error) {
	r, _, err := c.client.Repositories.DownloadContents(context.TODO(), c.repo.GetOwner().GetLogin(), c.repo.GetName(), file, &github.RepositoryContentGetOptions{
		Ref: ref,
	})
	return r, err
}

func (c *GithubClientImpl) GetRepo() Repo {
	return c.repo
}

func (c *GithubClientImpl) GetLatestTag() (tag string, ver *semver.Version, err error) {
	release, _, err := c.client.Repositories.GetLatestRelease(context.TODO(), c.repo.GetOwner().GetLogin(), c.repo.GetName())
	if err != nil {
		return "", nil, errors.Wrap(err, "Failed to get latest release")
	}
	version, err := semver.NewVersion(release.GetTagName())
	if err != nil {
		return "", nil, errors.Wrap(err, "Failed to parse tag as semver")
	}
	return release.GetTagName(), version, nil
}

func (c *GithubClientImpl) paginatePullsWithCommit(sha string, opts *github.PullRequestListOptions) ([]*github.PullRequest, error) {
	page := 0
	pulls := []*github.PullRequest{}
	for {
		prs, out, err := c.client.PullRequests.ListPullRequestsWithCommit(context.TODO(), c.repo.GetOwner().GetLogin(), c.repo.GetName(), sha, &github.PullRequestListOptions{
			State:     opts.State,
			Head:      opts.Head,
			Base:      opts.Base,
			Sort:      opts.Sort,
			Direction: opts.Direction,
			ListOptions: github.ListOptions{
				Page:    page,
				PerPage: opts.ListOptions.PerPage,
			},
		})
		if err != nil {
			return nil, errors.Wrap(err, "Failed to list pull requests")
		}
		pulls = append(pulls, prs...)
		if out.NextPage == 0 {
			break
		}
		page = out.NextPage
	}
	return pulls, nil
}

func (c *GithubClientImpl) paginateRefs(opts *github.ReferenceListOptions) ([]*github.Reference, error) {
	page := 0
	references := []*github.Reference{}
	for {
		refs, out, err := c.client.Git.ListMatchingRefs(context.TODO(), c.repo.GetOwner().GetLogin(), c.repo.GetName(), &github.ReferenceListOptions{
			Ref: opts.Ref,
			ListOptions: github.ListOptions{
				Page:    page,
				PerPage: opts.PerPage,
			},
		})
		if err != nil {
			return nil, errors.Wrap(err, "Failed to list references")
		}
		references = append(references, refs...)
		if out.NextPage == 0 {
			break
		}
		page = out.NextPage
	}
	return references, nil
}
