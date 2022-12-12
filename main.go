package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	jira "github.com/andygrunwald/go-jira/v2/onpremise"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/joelanford/jira-stalebot/internal/stalebot"
)

var (
	zapLevel = zap.NewAtomicLevel()
)

func setupLogger() logr.Logger {
	zCfg := zap.NewProductionConfig()
	if (isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())) ||
		(isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())) ||
		(isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd())) {
		zCfg = zap.NewDevelopmentConfig()
	}
	zCfg.Level = zapLevel

	z, err := zCfg.Build(zap.AddStacktrace(zapcore.DPanicLevel))
	if err != nil {
		panic(err)
	}
	return zapr.NewLogger(z)
}

func main() {
	log := setupLogger()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if err := rootCmd(log).ExecuteContext(ctx); err != nil {
		exitError(log, "execution error", err)
	}
}

func rootCmd(log logr.Logger) *cobra.Command {
	var (
		configFile string
		dryRun     bool
		verbosity  uint
		skipPrompt bool
	)
	cmd := &cobra.Command{
		Use: "jira-stalebot",
		Run: func(cmd *cobra.Command, args []string) {
			zapLevel.SetLevel(-zapcore.Level(verbosity))

			setupLog := log.WithName("setup")
			pat, err := stalebot.LoadPersonalAccessToken()
			if err != nil {
				exitError(setupLog, "load personal access token", err)
			}

			cfg, err := stalebot.LoadConfig(configFile)
			if err != nil {
				exitError(setupLog, "load stalebot config", err)
			}

			tp := &jira.PATAuthTransport{Token: pat}
			cl, err := jira.NewClient(cfg.JiraBaseURL, tp.Client())
			if err != nil {
				exitError(setupLog, "create jira client", err)
			}

			stalebotLog := log.WithName("stalebot")
			bot := stalebot.Stalebot{
				Client: cl,
				Config: *cfg,
				DryRun: dryRun,
				Prompt: !skipPrompt,
				Logger: stalebotLog,
			}
			if err := bot.Run(cmd.Context()); err != nil {
				exitError(stalebotLog, "run stalebot", err)
			}
		},
	}
	cmd.Flags().StringVar(&configFile, "config", "config.yaml", "Stalebot config file")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Dry run (don't make any changes)")
	cmd.Flags().UintVarP(&verbosity, "verbosity", "v", 0, "Log verbosity (higher number is more verbose)")
	cmd.Flags().BoolVarP(&skipPrompt, "yes", "y", false, "skip confirmation prompts for operations")
	return cmd
}

func exitError(l logr.Logger, msg string, err error) {
	l.Error(err, msg)
	os.Exit(1)
}
