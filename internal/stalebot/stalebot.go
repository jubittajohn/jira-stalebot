package stalebot

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	jira "github.com/andygrunwald/go-jira/v2/onpremise"
	"github.com/go-logr/logr"
)

type Stalebot struct {
	Client *jira.Client
	Config Config
	DryRun bool
	Prompt bool
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
			Fields:     []string{"key,issuetype,summary,labels,status,changelog,updated"},
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

			if op == None {
				continue
			}

			if bot.Prompt {
				confirmed, err := promptToConfirm(ctx, op, &issue)
				if err != nil {
					return fmt.Errorf("confirm operation: %v", err)
				}
				if !confirmed {
					continue
				}
			}

			if bot.DryRun {
				issueLogger.Info("dry-run operation", "op", op)
				continue
			}

			issueLogger.Info("performing operation", "op", op)
			var err error
			switch op {
			case None:
				continue
			case AddStaleLabel:
				err = bot.addStaleLabel(ctx, &issue)
			case RemoveStaleLabel:
				err = bot.removeStaleLabel(ctx, &issue)
			case Close:
				err = bot.closeIssue(ctx, &issue)
			}
			if err != nil {
				return fmt.Errorf("operation %q failed on issue %q: %v", op, issue.Key, err)
			}
			issueLogger.Info("operation succeeded", "op", op)

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

type update struct {
	Labels []labels `json:"labels" structs:"labels"`
}

type labels struct {
	Add    string `json:"add,omitempty" structs:"add"`
	Remove string `json:"remove,omitempty" structs:"remove"`
}

func (bot *Stalebot) addStaleLabel(ctx context.Context, issue *jira.Issue) error {
	if _, _, err := bot.Client.Issue.AddComment(ctx, issue.ID, &jira.Comment{Body: bot.Config.MarkComment}); err != nil {
		return fmt.Errorf("add mark comment to issue: %v", err)
	}

	reqBody := map[string]interface{}{"update": update{Labels: []labels{{Add: bot.Config.StaleLabel}}}}
	resp, err := bot.Client.Issue.UpdateIssue(ctx, issue.ID, reqBody)
	if err != nil {
		return fmt.Errorf("add stale label %q to issue: %v", bot.Config.StaleLabel, jira.NewJiraError(resp, err))
	}
	return nil
}

func (bot *Stalebot) removeStaleLabel(ctx context.Context, issue *jira.Issue) error {
	if _, _, err := bot.Client.Issue.AddComment(ctx, issue.ID, &jira.Comment{Body: bot.Config.UnmarkComment}); err != nil {
		return fmt.Errorf("add unmark comment to issue: %v", err)
	}

	reqBody := map[string]interface{}{"update": update{Labels: []labels{{Remove: bot.Config.StaleLabel}}}}
	resp, err := bot.Client.Issue.UpdateIssue(ctx, issue.ID, reqBody)
	if err != nil {
		err = jira.NewJiraError(resp, err)
		return fmt.Errorf("remove stale label %q from issue: %v", bot.Config.StaleLabel, jira.NewJiraError(resp, err))
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

func promptToConfirm(ctx context.Context, op Operation, issue *jira.Issue) (bool, error) {
	reader := bufio.NewReader(os.Stdin)

	type result struct {
		answer bool
		err    error
	}
	res := make(chan result)

	go func() {
		for {
			msg := fmt.Sprintf("Perform operation %q on %s %s: %s?", op, issue.Fields.Type.Name, issue.Key, issue.Fields.Summary)
			fmt.Printf("%s [y/N]: ", msg)

			response, err := reader.ReadString('\n')
			if err != nil {
				res <- result{answer: false, err: fmt.Errorf("read input from prompt: %v", err)}
				return
			}

			response = strings.ToLower(strings.TrimSpace(response))

			if response == "y" || response == "yes" {
				res <- result{answer: true, err: nil}
				return
			} else if response == "" || response == "n" || response == "no" {
				res <- result{answer: false, err: nil}
				return
			}
		}
	}()
	select {
	case <-ctx.Done():
		fmt.Printf("\n")
		return false, ctx.Err()
	case r := <-res:
		return r.answer, r.err
	}
}
