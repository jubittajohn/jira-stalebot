package stalebot

import (
	"context"
	"fmt"
	"time"

	jira "github.com/andygrunwald/go-jira/v2/onpremise"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/sets"
)

type Stalebot struct {
	Client *jira.Client
	Config Config
	Debug  bool
	DryRun bool
	Logger logr.Logger
}

func (bot *Stalebot) Run(ctx context.Context) error {
	if bot.Client == nil {
		panic("stalebot requires a client: client is nil")
	}

	if err := bot.Config.Validate(); err != nil {
		return fmt.Errorf("invalid stalebot config: %v", err)
	}

	eligibleIssuesQuery := bot.Config.EligibleIssuesQuery()
	last := 0
	now := time.Now()
	processed := 0
	opCounts := map[Operation]int{}

	bot.Logger.Info("querying jira", "jql", eligibleIssuesQuery)
	for {
		opt := &jira.SearchOptions{
			MaxResults: 1000, // Max results can go up to 1000
			StartAt:    last,
			Fields:     []string{"key,summary,labels,status,changelog,updated"},
			Expand:     "changelog",
		}

		chunk, resp, err := bot.Client.Issue.Search(ctx, eligibleIssuesQuery, opt)
		if err != nil {
			return fmt.Errorf("search for eligible issues: %v", err)
		}

		for _, issue := range chunk {
			issueLogger := bot.Logger.WithValues("key", issue.Key)
			op := bot.Config.IssueOperation(now, &issue)
			opCounts[op] += 1
			issueLogger.Info("planned operation", "op", op)
		}

		processed += len(chunk)

		total := resp.Total
		last = resp.StartAt + len(chunk)
		if last >= total {
			break
		}
	}
	bot.Logger.Info("found eligible issues", "count", processed)
	bot.Logger.Info("operations", string(AddStaleLabel), opCounts[AddStaleLabel], string(RemoveStaleLabel), opCounts[RemoveStaleLabel], string(Close), opCounts[Close])
	return nil
}

func (bot *Stalebot) addStaleLabel(ctx context.Context, issue *jira.Issue) error {
	issue.Fields.Labels = append(issue.Fields.Labels, bot.Config.StaleLabel)
	if _, _, err := bot.Client.Issue.Update(ctx, issue, &jira.UpdateQueryOptions{NotifyUsers: true}); err != nil {
		return fmt.Errorf("add stale label %q to issue: %v", bot.Config.StaleLabel, err)
	}
	if _, _, err := bot.Client.Issue.AddComment(ctx, issue.ID, &jira.Comment{Body: bot.Config.MarkComment}); err != nil {
		return fmt.Errorf("add mark comment to issue: %v", err)
	}
	return nil
}

func (bot *Stalebot) removeStaleLabel(ctx context.Context, issue *jira.Issue) error {
	issue.Fields.Labels = sets.NewString(issue.Fields.Labels...).Delete(bot.Config.StaleLabel).List()
	if _, _, err := bot.Client.Issue.Update(ctx, issue, &jira.UpdateQueryOptions{}); err != nil {
		return fmt.Errorf("remove stale label %q from issue: %v", bot.Config.StaleLabel, err)
	}
	if _, _, err := bot.Client.Issue.AddComment(ctx, issue.ID, &jira.Comment{Body: bot.Config.UnmarkComment}); err != nil {
		return fmt.Errorf("add unmark comment to issue: %v", err)
	}
	return nil
}

func (bot *Stalebot) closeIssue(ctx context.Context, issue *jira.Issue) error {
	transitions, _, err := bot.Client.Issue.GetTransitions(ctx, issue.ID)
	if err != nil {
		return fmt.Errorf("get transitions for issue: %v", err)
	}
	tID, err := transitionID(transitions, bot.Config.CloseStatus)
	if err != nil {
		return fmt.Errorf("get transition ID: %v", err)
	}
	if _, err := bot.Client.Issue.DoTransition(ctx, issue.ID, tID); err != nil {
		return fmt.Errorf("transition to status %q: %v", bot.Config.CloseStatus, err)
	}
	if _, _, err := bot.Client.Issue.AddComment(ctx, issue.ID, &jira.Comment{Body: bot.Config.CloseComment}); err != nil {
		return fmt.Errorf("add close comment to issue: %v", err)
	}
	return nil
}

func transitionID(transitions []jira.Transition, statusName string) (string, error) {
	for _, t := range transitions {
		if t.To.Name == statusName {
			return t.ID, nil
		}
	}
	return "", fmt.Errorf("no transition found to status %q", statusName)
}
