package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/shurcooL/githubql"
)

// Checker has a githubql client to run queries and also knows about
// the current repositories releases to compare against.
type Checker struct {
	logger   log.Logger
	client   *githubql.Client
	tags map[string]Repository
}

// Run the queries and comparisons for the given repositories in a given interval.
func (c *Checker) Run(interval time.Duration, repositories []string, tags chan<- Repository) {
	if c.tags == nil {
		c.tags = make(map[string]Repository)
	}

	for {
		for _, repoName := range repositories {
			s := strings.Split(repoName, "/")
			owner, name := s[0], s[1]

			nextRepo, err := c.query(owner, name)
			if err != nil {
				level.Warn(c.logger).Log(
					"msg", "failed to query the repository's tags",
					"owner", owner,
					"name", name,
					"err", err,
				)
				continue
			}

			// For debugging uncomment this next line
			// tags <- nextRepo

			currRepo, ok := c.tags[repoName]

			// We've queried the repository for the first time.
			// Saving the current state to compare with the next iteration.
			if !ok {
				c.tags[repoName] = nextRepo
				continue
			}

			if nextRepo.Tag.Name != currRepo.Tag.Name {
				tags <- nextRepo
				c.tags[repoName] = nextRepo
			} else {
				level.Debug(c.logger).Log(
					"msg", "no new tag for repository",
					"owner", owner,
					"name", name,
				)
			}
		}
		time.Sleep(interval)
	}
}

// This should be improved in the future to make batch requests for all watched repositories at once
// TODO: https://github.com/shurcooL/githubql/issues/17

func (c *Checker) query(owner, name string) (Repository, error) {
	var query struct {
		Repository struct {
			ID          githubql.ID
			Name        githubql.String
			Description githubql.String
			URL         githubql.URI

			Refs struct {
				Edges []struct {
					Node struct {
						ID          githubql.ID
						Name        githubql.String
					}
				}
			} `graphql:"refs(last: 1, refPrefix:\"refs/tags/\", orderBy:{direction: ASC, field: TAG_COMMIT_DATE})"`
		} `graphql:"repository(owner: $owner, name: $name)"`
	}

	variables := map[string]interface{}{
		"owner": githubql.String(owner),
		"name":  githubql.String(name),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.client.Query(ctx, &query, variables); err != nil {
		return Repository{}, err
	}

	repositoryID, ok := query.Repository.ID.(string)
	if !ok {
		return Repository{}, fmt.Errorf("can't convert repository id to string: %v", query.Repository.ID)
	}

	if len(query.Repository.Refs.Edges) == 0 {
		return Repository{}, fmt.Errorf("can't find any tags for %s/%s", owner, name)
	}
	latestTag := query.Repository.Refs.Edges[0].Node

	tagID, ok := latestTag.ID.(string)
	if !ok {
		return Repository{}, fmt.Errorf("can't convert tag id to string: %v", latestTag.ID)
	}

	return Repository{
		ID:          repositoryID,
		Name:        string(query.Repository.Name),
		Owner:       owner,
		Description: string(query.Repository.Description),
		URL:         *query.Repository.URL.URL,

		Tag: Tag{
			ID:          tagID,
			Name:        string(latestTag.Name),
		},
	}, nil
}
