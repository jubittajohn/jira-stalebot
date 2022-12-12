package stalebot

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"sigs.k8s.io/yaml"
)

type Config struct {
	JiraBaseURL string `json:"jiraBaseURL"`
	Project     string `json:"project"`

	DaysUntilStale int `json:"daysUntilStale"`
	DaysUntilClose int `json:"daysUntilClose"`

	OnlyLabels   []string `json:"onlyLabels"`
	ExemptLabels []string `json:"exemptLabels"`

	StaleLabel    string `json:"staleLabel"`
	MarkComment   string `json:"markComment"`
	UnmarkComment string `json:"unmarkComment"`

	CloseStatus  string `json:"closeStatus"`
	CloseComment string `json:"closeComment"`

	LimitPerRun int `json:"limitPerRun"`
}

const (
	defaultStaleLabel     = "lifecycle-stale"
	defaultDaysUntilStale = 90
	defaultDaysUntilClose = 14
	defaultLimitPerRun    = 100
)

var (
	defaultMarkCommentFunc = func(c Config) string {
		return fmt.Sprintf("[STALEBOT COMMENT] This issue is stale because it has not had activity for %d days. "+
			"Comment, remove label %q, or make any another update to this issue to avoid closure in %d days.",
			c.DaysUntilStale, c.StaleLabel, c.DaysUntilClose)
	}
	defaultUnmarkCommentFunc = func(c Config) string {
		return fmt.Sprintf("[STALEBOT COMMENT] A recent update was detected, so this issue is no longer stale. "+
			"Removing stale label %q.", c.StaleLabel)
	}
)

func LoadConfig(configFile string) (*Config, error) {
	configData, err := os.ReadFile(configFile)
	if err != nil {
		return nil, err
	}
	c := &Config{}
	if err := yaml.Unmarshal(configData, c); err != nil {
		return nil, err
	}

	c.setDefaults()

	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) setDefaults() {
	if c.StaleLabel == "" {
		c.StaleLabel = defaultStaleLabel
	}
	if c.DaysUntilStale <= 0 {
		c.DaysUntilStale = defaultDaysUntilStale
	}
	if c.DaysUntilClose <= 0 {
		c.DaysUntilClose = defaultDaysUntilClose
	}
	if c.LimitPerRun <= 0 {
		c.LimitPerRun = defaultLimitPerRun
	}
	if c.MarkComment == "" {
		c.MarkComment = defaultMarkCommentFunc(*c)
	}
	if c.UnmarkComment == "" {
		c.UnmarkComment = defaultUnmarkCommentFunc(*c)
	}
}

func (c *Config) EligibleIssuesQuery() string {
	ands := []string{
		fmt.Sprintf("project = %s", c.Project),
		fmt.Sprintf("statusCategory != Done"),
	}
	ands = append(ands, c.exemptOrOnlyLabels()...)
	return completeQuery(ands)
}

func (c *Config) exemptOrOnlyLabels() []string {
	ands := make([]string, 0)
	if len(c.ExemptLabels) > 0 {
		ands = append(ands, fmt.Sprintf("(labels not in (%s) OR labels is EMPTY)", strings.Join(c.ExemptLabels, ",")))
	} else if len(c.OnlyLabels) > 0 {
		for _, l := range c.OnlyLabels {
			ands = append(ands, fmt.Sprintf("labels = %s", l))
		}
	}
	return ands
}

func completeQuery(ands []string) string {
	return fmt.Sprintf("%s ORDER BY updatedDate DESC", strings.Join(ands, " AND "))
}

func (c *Config) Validate() error {
	validateErrors := []error{}
	if c.JiraBaseURL == "" {
		validateErrors = append(validateErrors, fmt.Errorf("config must specify `jiraBaseURL`"))
	}
	if !isValidProjectKey(c.Project) {
		validateErrors = append(validateErrors, fmt.Errorf("config must specify valid project key (two or more uppercase letters)"))
	}
	if len(c.OnlyLabels) > 0 && len(c.ExemptLabels) > 0 {
		validateErrors = append(validateErrors, fmt.Errorf("config must not specify both onlyLabels and exemptLabels"))
	}
	for _, l := range c.OnlyLabels {
		if !isValidLabel(l) {
			validateErrors = append(validateErrors, fmt.Errorf("config contains invalid label `%s` in onlyLabels", l))
		}
	}
	for _, l := range c.ExemptLabels {
		if !isValidLabel(l) {
			validateErrors = append(validateErrors, fmt.Errorf("config contains invalid label `%s` in exemptLabels", l))
		}
	}

	if !isValidLabel(c.StaleLabel) {
		validateErrors = append(validateErrors, fmt.Errorf("config must not specify invalid staleLabel `%s`", c.StaleLabel))
	}

	if !isValidStatusName(c.CloseStatus) {
		validateErrors = append(validateErrors, fmt.Errorf("config must not specify invalid closeStatus `%s`", c.CloseStatus))
	}

	return newAggregateError(validateErrors)
}

func isValidProjectKey(project string) bool {
	return regexp.MustCompile("^[A-Z]{2,}$").MatchString(project)
}

func isValidLabel(label string) bool {
	return regexp.MustCompile("^[0-9A-Za-z-]+$").MatchString(label)
}

func isValidStatusName(name string) bool {
	return len(name) > 0 && !strings.Contains(name, `"`)
}

type aggregateError []error

func newAggregateError(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}
	nonNilErrors := errs[:0]
	for _, err := range errs {
		if err != nil {
			nonNilErrors = append(nonNilErrors, err)
		}
	}
	return aggregateError(nonNilErrors)
}

func (errs aggregateError) Error() string {
	errMsgs := make([]string, 0, len(errs))
	for _, err := range errs {
		errMsgs = append(errMsgs, err.Error())
	}

	return fmt.Sprintf("multiple errors: %s", strings.Join(errMsgs, "; "))
}
