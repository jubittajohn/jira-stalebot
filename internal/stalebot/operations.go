package stalebot

import (
	"strings"
	"time"

	jira "github.com/andygrunwald/go-jira/v2/onpremise"
	"k8s.io/apimachinery/pkg/util/sets"
)

type Operation string

const (
	None             Operation = "None"
	AddStaleLabel    Operation = "AddStaleLabel"
	RemoveStaleLabel Operation = "RemoveStaleLabel"
	Close            Operation = "Close"
)

func (c *Config) IssueOperation(now time.Time, i *jira.Issue) Operation {
	// No updates to issues that are complete
	if i.Fields.Status.StatusCategory.Key == jira.StatusCategoryComplete {
		return None
	}

	// Check if issue is even eligible for stale bot processing.
	issueLabels := sets.NewString(i.Fields.Labels...)
	if len(c.ExemptLabels) > 0 {
		// No update to issues that have ANY exempt labels
		if issueLabels.HasAny(c.ExemptLabels...) {
			return None
		}
	} else if issueLabels.HasAll(c.OnlyLabels...) {
		// No update to issues that have ALL only labels
		return None
	}

	// Staleness Lifecycle Step 1: Add a stale label
	// If the issue does not already have a stale label, we'll check its last update time.
	if !issueLabels.Has(c.StaleLabel) {
		// No update if it has not yet been "daysUntilStale" days since the last update
		if time.Time(i.Fields.Updated).After(now.Add(-time.Hour * 24 * time.Duration(c.DaysUntilStale))) {
			return None
		}
		return AddStaleLabel
	}

	// Staleness Lifecycle Step 2: Close rotten issues
	// At this point, we know the issue has the stale label (progressing beyond step 1 guarantees this).
	//
	// If the last update added the stale label (i.e. there have been no updates since the stale label
	// was added), then we'll check its last update time.
	if lastUpdateAddedStaleLabel(i, c.StaleLabel) {
		// No update if it has not yet been "daysUntilClose" days since the last update
		if time.Time(i.Fields.Updated).After(now.Add(-time.Hour * 24 * time.Duration(c.DaysUntilClose))) {
			return None
		}
		return Close
	}

	// Staleness Lifecycle Step 3: Unmark updated issues
	// By now, we kno that the last update did not add a stale label, so we remove the stale label.
	//
	// NOTE: It doesn't matter when the last update was with respect to the update that added the stale label.
	// The fact that there was an update after the stale label was added but before the stale bot ran again
	// means that the next encounter of this issue by the stale bot should remove the label.
	return RemoveStaleLabel
}

func lastUpdateAddedStaleLabel(i *jira.Issue, staleLabel string) bool {
	if i.Changelog != nil && len(i.Changelog.Histories) > 0 {
		lastUpdate := i.Changelog.Histories[len(i.Changelog.Histories)-1]
		for _, item := range lastUpdate.Items {
			if item.Field == "labels" {
				from := sets.NewString(strings.Split(item.FromString, " ")...)
				to := sets.NewString(strings.Split(item.ToString, " ")...)
				if !from.Has(staleLabel) && to.Has(staleLabel) {
					return true
				}
			}
		}
	}
	return false
}
