// +build !dev

package commons

import (
	"gopkg.in/src-d/go-git.v4"
	"golang.org/x/oauth2"
	"github.com/gofrs/uuid"
	http "gopkg.in/src-d/go-git.v4/plumbing/transport/http"
	datastructures "github.com/bbernhard/imagemonkey-core/datastructures"
	"github.com/google/go-github/github"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"os"
	"errors"
	"context"
	"time"
)


type LabelsRepository struct {
	projectOwner              string
	repositoryName            string
	repositoryUrl             string
	githubToken               string
	gitWorktree               *git.Worktree
	gitRepository             *git.Repository
	gitCheckoutDir            string
	autoGeneratedLabelsWriter *LabelsWriter
	autoGeneratedMetaLabelsWriter *MetaLabelsWriter
}

func NewLabelsRepository(projectOwner string, repositoryName string, gitCheckoutDir string) *LabelsRepository {
	return &LabelsRepository{
		projectOwner:              projectOwner,
		repositoryName:            repositoryName,
		gitCheckoutDir:            gitCheckoutDir,
		repositoryUrl:             "https://github.com/" + projectOwner + "/" + repositoryName,
		autoGeneratedLabelsWriter: NewLabelsWriter(gitCheckoutDir + "/en/includes/labels/autogenerated.libsonnet"),
		autoGeneratedMetaLabelsWriter: NewMetaLabelsWriter(gitCheckoutDir + "/en/includes/metalabels/autogenerated.libsonnet"),
	}
}

func (p *LabelsRepository) SetToken(token string) {
	p.githubToken = token
}

func (p *LabelsRepository) Clone() error {
	if _, err := os.Stat(p.gitCheckoutDir); os.IsNotExist(err) {
		p.gitRepository, err = git.PlainClone(p.gitCheckoutDir, false, &git.CloneOptions{
			URL:      p.repositoryUrl,
			Progress: os.Stdout,
		})

		if err != nil {
			return errors.New("Couldn't clone " + p.repositoryUrl + ": " + err.Error())
		}

		p.gitWorktree, err = p.gitRepository.Worktree()
		if err != nil {
			return errors.New("Couldn't get worktree: " + err.Error())
		}
	} else { //git dir exists
		p.gitRepository, err = git.PlainOpen(p.gitCheckoutDir)
		if err != nil {
			return errors.New("Couldn't open " + p.gitCheckoutDir + ": " + err.Error())
		}

		p.gitWorktree, err = p.gitRepository.Worktree()
		if err != nil {
			return errors.New("Couldn't get worktree: " + err.Error())
		}
	}

	return nil
}

func (p *LabelsRepository) AddLabelAndPushToRepo(trendingLabel datastructures.TrendingLabelBotTask) (string, error) {
	branchNameUuid, err := uuid.NewV4()
	if err != nil {
		return "", errors.New("Couldn't create branch name: " + err.Error())
	}

	err = createGitBranch(p.gitWorktree, branchNameUuid.String())
	if err != nil {
		return "", errors.New("Couldn't create git branch " + branchNameUuid.String() + ": " + err.Error())
	}


	if trendingLabel.LabelType == "normal" {
		labelEntry, err := generateLabelEntry(trendingLabel.RenameTo, trendingLabel.Plural, trendingLabel.Description)
		if err != nil {
			return "", errors.New("Couldn't generate label entry: " + err.Error())
		}

		err = p.autoGeneratedLabelsWriter.Add(trendingLabel.Name, labelEntry)
		if err != nil {
			return "", errors.New("Couldn't add label: " + err.Error())
		}

		err = gitAdd(p.gitWorktree, p.autoGeneratedLabelsWriter.GetFilename())
		if err != nil {
			return "", errors.New("Couldn't add file: " + err.Error())
		}
	} else if trendingLabel.LabelType == "meta" {
		metaLabelEntry, err := generateMetaLabelEntry(trendingLabel.RenameTo, trendingLabel.Plural, trendingLabel.Description)
		if err != nil {
			return "", errors.New("Couldn't generate label entry: " + err.Error())
		}

		err = p.autoGeneratedMetaLabelsWriter.Add(trendingLabel.Name, metaLabelEntry)
		if err != nil {
			return "", errors.New("Couldn't add label: " + err.Error())
		}

		err = gitAdd(p.gitWorktree, p.autoGeneratedMetaLabelsWriter.GetFilename())
		if err != nil {
			return "", errors.New("Couldn't add file: " + err.Error())
		}
	} else {
		return "", errors.New("Invalid label type " + trendingLabel.LabelType)
	}

	err = gitCommit(p.gitWorktree, "added label "+trendingLabel.Name)
	if err != nil {
		return "", errors.New("Couldn't commit: " + err.Error())
	}

	err = gitPush(p.gitRepository, p.githubToken)
	if err != nil {
		return "", errors.New("Couldn't push: " + err.Error())
	}

	return branchNameUuid.String(), nil
}

func (p *LabelsRepository) MergeRemoteBranchIntoMaster(branchName string) error {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: p.githubToken},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := github.NewClient(tc)

	//create a new comment
	newPullRequest := &github.NewPullRequest{
		Title: github.String("test"),
		Head:  github.String(branchName),
		Base:  github.String("master"),
	}

	pullRequest, _, err := client.PullRequests.Create(ctx, p.projectOwner, p.repositoryName, newPullRequest)
	if err != nil {
		return err
	}

	_, _, err = client.PullRequests.Merge(ctx, p.projectOwner, p.repositoryName, *pullRequest.Number, "merged", &github.PullRequestOptions{})

	return err
}

func (p *LabelsRepository) RemoveRemoteBranch(branchName string) error {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: p.githubToken},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := github.NewClient(tc)

	ref, _, err := client.Git.GetRef(ctx, p.projectOwner, p.repositoryName, "heads/"+branchName)
	if err != nil {
		return err
	}

	_, err = client.Git.DeleteRef(ctx, p.projectOwner, p.repositoryName, ref.GetRef())
	return err
}

func (p *LabelsRepository) RemoveLocal() error {
	return os.RemoveAll(p.gitCheckoutDir)
}

func gitPull(worktree *git.Worktree) error {
	err := worktree.Pull(&git.PullOptions{RemoteName: "origin"})
	if err != nil && err == git.NoErrAlreadyUpToDate {
		return nil
	}

	return nil
}

func gitPush(repository *git.Repository, apiToken string) error {
	err := repository.Push(&git.PushOptions{
		RemoteName: "origin",
		Auth: &http.BasicAuth{
			Username: "imagemonkeybot",
			Password: apiToken,
		},
	})
	return err
}

func createGitBranch(worktree *git.Worktree, name string) error {
	branch := plumbing.NewBranchReferenceName(name)
	return worktree.Checkout(&git.CheckoutOptions{Branch: branch, Create: true})
}

func gitAdd(worktree *git.Worktree, filename string) error {
	_, err := worktree.Add(filename)
	return err
}

func gitCommit(worktree *git.Worktree, commitMsg string) error {
	_, err := worktree.Commit(commitMsg, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "ImageMonkeyBot",
			Email: "bot@imagemonkey.io",
			When:  time.Now(),
		},
	})

	return err
}
