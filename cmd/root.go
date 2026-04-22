package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	env        string
	region     string
	cell       string
	awsProfile string
)

var rootCmd = &cobra.Command{
	Use:   "dlq-viewer",
	Short: "A TUI application to view identity-pipeline DLQ messages",
	Long: `dlq-viewer is a terminal UI application that allows you to view messages
from the identity-pipeline Dead Letter Queues (DLQs).

It supports viewing messages from:
- Retryable DLQ: For transient failures that can be retried
- Failed DLQ: For persistent failures that cannot be recovered

Queue names are derived from environment, region, and cell:
- identity-pipeline-retryable-{env}-{region}-{cell}
- identity-pipeline-failed-{env}-{region}-{cell}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI()
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&env, "env", "e", "dev", "Environment (dev, stage, prod)")
	rootCmd.PersistentFlags().StringVarP(&region, "region", "r", "us-east-1", "AWS region")
	rootCmd.PersistentFlags().StringVarP(&cell, "cell", "c", "cell-1", "Cell identifier")
	rootCmd.PersistentFlags().StringVarP(&awsProfile, "profile", "p", "", "AWS profile to use for credentials")
}

func getQueueNames() (retryable, failed string) {
	retryable = fmt.Sprintf("identity-pipeline-retryable-%s-%s-%s", env, region, cell)
	failed = fmt.Sprintf("identity-pipeline-failed-%s-%s-%s", env, region, cell)
	return
}
