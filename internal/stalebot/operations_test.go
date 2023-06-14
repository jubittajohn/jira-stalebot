package stalebot_test

import (
	"fmt"
	"time"

	jira "github.com/andygrunwald/go-jira/v2/cloud"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/joelanford/jira-stalebot/internal/stalebot"
)

var (
	now          = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	day          = time.Hour * 24
	minus120days = now.Add(-day * 120)
	minus60days  = now.Add(-day * 60)
)

var _ = Describe("Operations", func() {
	exemptLabels := sets.NewString("lifecycle-frozen", "stablebot-exempt")
	onlyLabels := sets.NewString("check-stale", "stablebot-allow")

	var (
		issue *jira.Issue
		cfg   *stalebot.Config
	)
	BeforeEach(func() {
		issue = &jira.Issue{
			Self: "",
			Key:  "TEST-100",
			Fields: &jira.IssueFields{
				Updated: jira.Time{},
				Status:  &jira.Status{},
				Labels:  []string{},
			},
			Changelog: &jira.Changelog{},
		}
		cfg = &stalebot.Config{
			DaysUntilStale: 90,
			DaysUntilClose: 30,
			StaleLabel:     "lifecycle-stale",
		}
	})

	AssertOperation := func(expectedOperation stalebot.Operation) {
		It(fmt.Sprintf("results in operation %s", expectedOperation), func() {
			actualOperation := cfg.IssueOperation(now, issue)
			Expect(actualOperation).To(Equal(expectedOperation))
		})
	}

	AssertAll := func() {
		When("issue is complete", func() {
			BeforeEach(func() {
				issue.Fields.Status.StatusCategory = jira.StatusCategory{Key: jira.StatusCategoryComplete}
			})
			AssertOperation(stalebot.None)
		})
		When("issue lacks stale label", func() {
			When("issue was updated before stale days ago", func() {
				BeforeEach(func() {
					issue.Fields.Updated = jira.Time(minus120days)
				})
				AssertOperation(stalebot.AddStaleLabel)
			})
			When("issue was updated after stale days ago", func() {
				BeforeEach(func() {
					issue.Fields.Updated = jira.Time(minus60days)
				})
				AssertOperation(stalebot.None)
			})
		})
		When("issue has stale label", func() {
			BeforeEach(func() {
				issue.Fields.Labels = append(issue.Fields.Labels, cfg.StaleLabel)
			})

			WhenLastUpdateAddedStaleLabel := func(assert func()) {
				When("last issue update added stale label", func() {
					BeforeEach(func() {
						issue.Changelog.Histories = append(issue.Changelog.Histories, jira.ChangelogHistory{Items: []jira.ChangelogItems{{
							Field:      "labels",
							FromString: "",
							ToString:   cfg.StaleLabel,
						}}})
					})
					assert()
				})
			}
			WhenLastUpdateDidNotAddStaleLabel := func(assert func()) {
				When("last issue update did not add stale label", func() {
					BeforeEach(func() {
						issue.Changelog.Histories = append(issue.Changelog.Histories, jira.ChangelogHistory{Items: []jira.ChangelogItems{{
							Field:      "labels",
							FromString: "foo",
							ToString:   "bar",
						}}})
					})
					assert()
				})
			}

			When("issue was updated before close days ago", func() {
				BeforeEach(func() {
					issue.Fields.Updated = jira.Time(minus60days)
				})
				WhenLastUpdateAddedStaleLabel(func() {
					AssertOperation(stalebot.Close)
				})
				WhenLastUpdateDidNotAddStaleLabel(func() {
					AssertOperation(stalebot.RemoveStaleLabel)
				})
			})
			When("issue was updated after close days ago", func() {
				BeforeEach(func() {
					issue.Fields.Updated = jira.Time(now)
				})
				WhenLastUpdateAddedStaleLabel(func() {
					It(fmt.Sprintf("results in operation %s", stalebot.None), func() {
						actualOperation := cfg.IssueOperation(now, issue)
						Expect(actualOperation).To(Equal(stalebot.None))
					})
					AssertOperation(stalebot.None)
				})
				WhenLastUpdateDidNotAddStaleLabel(func() {
					AssertOperation(stalebot.RemoveStaleLabel)
				})
			})
		})
	}

	When("config uses exemptLabels", func() {
		BeforeEach(func() {
			cfg.ExemptLabels = exemptLabels.List()
		})
		When("issue is exempt", func() {
			BeforeEach(func() {
				issue.Fields.Labels = onlyLabels.Insert(exemptLabels.UnsortedList()[0]).UnsortedList()
			})
			AssertOperation(stalebot.None)
		})
		AssertAll()
	})
	When("config uses onlyLabels", func() {
		BeforeEach(func() {
			cfg.OnlyLabels = onlyLabels.List()
		})
		When("issue is exempt", func() {
			BeforeEach(func() {
				issue.Fields.Labels = onlyLabels.Insert(exemptLabels.UnsortedList()[0]).UnsortedList()
			})
			AssertOperation(stalebot.None)
		})
		AssertAll()
	})
})
