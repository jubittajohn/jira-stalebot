package stalebot

import (
	"fmt"
	"os"
	"strings"

	"github.com/adrg/xdg"
)

const (
	patEnvVar         = "JIRA_STALEBOT_PAT"
	xdgConfigFilePath = "jira-stalebot/pat"
)

func LoadPersonalAccessToken() (string, error) {
	if pat, ok := os.LookupEnv(patEnvVar); ok {
		return pat, nil
	}

	patFile, err := xdg.SearchConfigFile(xdgConfigFilePath)
	if err != nil {
		return "", fmt.Errorf("%s environment variable not set and personal access token file not found: %v", patEnvVar, err)
	}
	patBytes, err := os.ReadFile(patFile)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(patBytes)), nil
}
