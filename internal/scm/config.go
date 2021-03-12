package scm

import (
	"context"

	"github.com/go-playground/validator"
	"github.com/google/go-github/v32/github"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

var configValidator *validator.Validate = validator.New()

type LabelsConfig struct {
	Major string `yaml:"major,omitempty"`
	Minor string `yaml:"minor,omitempty"`
}

type StrategyConf struct {
	Type string `yaml:"type,omitempty" validate:"oneof=pre-release full-release"`
}

type Config struct {
	TargetBranch string       `yaml:"targetBranch,omitempty" validate:"required"`
	Labels       LabelsConfig `yaml:"labels,omitempty"`
	Strategy     StrategyConf `yaml:"strategy,omitempty"`
}

type FullReleaseStrategy struct {
}

type PreReleaseStrategy struct {
}

type ChannelStrategy struct {
}

type Strategy interface {
	Match(string) bool
	Release(c *GithubClientImpl, sha string) (interface{}, error)
}

func (c *GithubClientImpl) getConfig(ref string) (Strategy, error) {
	config := &Config{
		TargetBranch: c.repo.GetDefaultBranch(),
		Labels: LabelsConfig{
			Major: "major",
			Minor: "minor",
		},
		Strategy: StrategyConf{
			Type: "pre-release",
		},
	}
	reader, err := c.GetFile(ref, ".ship-it")
	if err != nil {
		c.log.Debug("Error getting config from github, using defaults ", err)
		return nil, nil
	}
	defer reader.Close()
	decoder := yaml.NewDecoder(reader)
	if err := decoder.Decode(config); err != nil {
		return nil, errors.Wrap(err, "Error decoding config file")
	}
	if err := configValidator.Struct(config); err != nil {
		return nil, errors.Wrap(err, "Could not validate configuration")
	}
	return nil, nil
}

func (*PreReleaseStrategy) Match(s string) bool {
	return false
}

func (*PreReleaseStrategy) Release(c *GithubClientImpl, sha string) (interface{}, error) {
	v, err := c.GetLatestTag()
	if err != nil {
		return nil, err
	}

	n := v.IncPatch()
}

func (*FullReleaseStrategy) Release(c *GithubClientImpl, sha string) (interface{}, error) {
	v, err := c.GetLatestTag()
	if err != nil {
		return nil, err
	}

	n := v.IncPatch()
	
	c.Pulls
} 

// func (*ChannelStrategy) Release(c *GithubClientImpl, sha string) (interface{}, error) {
// 	r, resp, err := c.client.Repositories.ListReleases(context.TODO(), "", "", &github.ListOptions{})
// 	for _, v := range r {
// 		v.
		
// 	}	
// }