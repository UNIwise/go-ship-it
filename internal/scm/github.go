package scm

import (
	"context"
	"fmt"
	"io"
	"net/http"

	semver "github.com/Masterminds/semver/v3"
	"github.com/google/go-github/v33/github"
	"github.com/pkg/errors"
)

type GithubClient interface {
	CreateMilestone(ctx context.Context, title string) (*github.Milestone, error)
	AddPRtoMilestone(ctx context.Context, pull *github.PullRequest, milestone *github.Milestone) error
	GetReleaseByTag(ctx context.Context, tag string) (*github.RepositoryRelease, error)
	DeleteRelease(ctx context.Context, r *github.RepositoryRelease) error
	DeleteTag(ctx context.Context, tag string) error
	EditRelease(ctx context.Context, id int64, release *github.RepositoryRelease) (*github.RepositoryRelease, error)
	GetRef(ctx context.Context, r string) (*github.Reference, error)
	CreateRef(ctx context.Context, r *github.Reference) error
	CreateRelease(ctx context.Context, r *github.RepositoryRelease) error
	GetRefs(ctx context.Context, pattern string) ([]*github.Reference, error)
	GetCommitRange(ctx context.Context, base, head string) (*github.CommitsComparison, error)
	GetPullsInCommitRange(ctx context.Context, commits []*github.RepositoryCommit) ([]*github.PullRequest, error)
	GetLatestTag(ctx context.Context) (tag string, ver *semver.Version, err error)
	GetFile(ctx context.Context, ref, file string) (io.ReadCloser, error)
	GetRepo() Repo
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

func (c *GithubClientImpl) CreateMilestone(ctx context.Context, title string) (*github.Milestone, error) {
	milestone, _, err := c.client.Issues.CreateMilestone(ctx, c.repo.GetOwner().GetLogin(), c.repo.GetName(), &github.Milestone{
		Title: github.String(title),
		State: github.String("closed"),
	})
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to create milestone '%s'", title)
	}
	return milestone, nil
}

func (c *GithubClientImpl) AddPRtoMilestone(ctx context.Context, pull *github.PullRequest, milestone *github.Milestone) error {
	_, _, err := c.client.Issues.Edit(ctx, c.repo.GetOwner().GetLogin(), c.repo.GetName(), pull.GetNumber(), &github.IssueRequest{
		Milestone: milestone.Number,
	})
	if err != nil {
		return errors.Wrapf(err, "Failed to add pull request '%d' to milestone '%d'", pull.GetNumber(), milestone.GetNumber())
	}
	return nil
}

func (c *GithubClientImpl) GetReleaseByTag(ctx context.Context, tag string) (*github.RepositoryRelease, error) {
	release, _, err := c.client.Repositories.GetReleaseByTag(ctx, c.repo.GetOwner().GetLogin(), c.repo.GetName(), tag)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to find release from tag '%s'", tag)
	}
	return release, nil
}

func (c *GithubClientImpl) DeleteRelease(ctx context.Context, release *github.RepositoryRelease) error {
	_, err := c.client.Repositories.DeleteRelease(ctx, c.repo.GetOwner().GetLogin(), c.repo.GetName(), release.GetID())
	if err != nil {
		return errors.Wrapf(err, "Failed to delete release '%d'", release.GetID())
	}
	return nil
}

func (c *GithubClientImpl) DeleteTag(ctx context.Context, tag string) error {
	_, err := c.client.Git.DeleteRef(ctx, c.repo.GetOwner().GetLogin(), c.repo.GetName(), fmt.Sprintf("refs/tags/%s", tag))
	if err != nil {
		return errors.Wrapf(err, "Failed to delete tag '%s'", tag)
	}
	return nil
}

func (c *GithubClientImpl) EditRelease(ctx context.Context, id int64, release *github.RepositoryRelease) (*github.RepositoryRelease, error) {
	r, _, err := c.client.Repositories.EditRelease(ctx, c.repo.GetOwner().GetLogin(), c.repo.GetName(), id, release)
	return r, err
}

func (c *GithubClientImpl) GetRef(ctx context.Context, r string) (*github.Reference, error) {
	ref, _, err := c.client.Git.GetRef(ctx, c.repo.GetOwner().GetLogin(), c.repo.GetName(), r)
	return ref, err
}

func (c *GithubClientImpl) CreateRef(ctx context.Context, r *github.Reference) error {
	_, _, err := c.client.Git.CreateRef(ctx, c.repo.GetOwner().GetLogin(), c.repo.GetName(), r)
	return err
}

func (c *GithubClientImpl) CreateRelease(ctx context.Context, r *github.RepositoryRelease) error {
	_, _, err := c.client.Repositories.CreateRelease(ctx, c.repo.GetOwner().GetLogin(), c.repo.GetName(), r)
	return err
}

func (c *GithubClientImpl) GetRefs(ctx context.Context, pattern string) ([]*github.Reference, error) {
	return c.paginateRefs(ctx, &github.ReferenceListOptions{Ref: pattern, ListOptions: github.ListOptions{PerPage: 25}})
}

func (c *GithubClientImpl) GetCommitRange(ctx context.Context, base, head string) (*github.CommitsComparison, error) {
	comparison, _, err := c.client.Repositories.CompareCommits(ctx, c.repo.GetOwner().GetLogin(), c.repo.GetName(), base, head)
	if err != nil {
		return nil, err
	}
	return comparison, nil
}

func (c *GithubClientImpl) GetPullsInCommitRange(ctx context.Context, commits []*github.RepositoryCommit) ([]*github.PullRequest, error) {
	max := 100
	if len(commits) < max {
		max = len(commits)
	}
	unique := map[int]interface{}{}
	pulls := []*github.PullRequest{}
	for _, commit := range commits[:max] {
		prs, err := c.paginatePullsWithCommit(ctx, commit.GetSHA(), &github.PullRequestListOptions{})
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

func (c *GithubClientImpl) GetFile(ctx context.Context, ref, file string) (io.ReadCloser, error) {
	r, _, err := c.client.Repositories.DownloadContents(ctx, c.repo.GetOwner().GetLogin(), c.repo.GetName(), file, &github.RepositoryContentGetOptions{
		Ref: ref,
	})
	return r, err
}

func (c *GithubClientImpl) GetRepo() Repo {
	return c.repo
}

func (c *GithubClientImpl) GetLatestTag(ctx context.Context) (tag string, ver *semver.Version, err error) {
	release, _, err := c.client.Repositories.GetLatestRelease(ctx, c.repo.GetOwner().GetLogin(), c.repo.GetName())
	if err != nil {
		return "", nil, errors.Wrap(err, "Failed to get latest release")
	}
	version, err := semver.NewVersion(release.GetTagName())
	if err != nil {
		return "", nil, errors.Wrap(err, "Failed to parse tag as semver")
	}
	return release.GetTagName(), version, nil
}

func (c *GithubClientImpl) paginatePullsWithCommit(ctx context.Context, sha string, opts *github.PullRequestListOptions) ([]*github.PullRequest, error) {
	page := 0
	pulls := []*github.PullRequest{}
	for {
		prs, out, err := c.client.PullRequests.ListPullRequestsWithCommit(ctx, c.repo.GetOwner().GetLogin(), c.repo.GetName(), sha, &github.PullRequestListOptions{
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

func (c *GithubClientImpl) paginateRefs(ctx context.Context, opts *github.ReferenceListOptions) ([]*github.Reference, error) {
	page := 0
	references := []*github.Reference{}
	for {
		refs, out, err := c.client.Git.ListMatchingRefs(ctx, c.repo.GetOwner().GetLogin(), c.repo.GetName(), &github.ReferenceListOptions{
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
