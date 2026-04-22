package cmd

import (
	"context"
	"fmt"

	"github.com/kaysush-twilio/dlq-viewer/internal/sqs"
	"github.com/kaysush-twilio/dlq-viewer/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func runTUI() error {
	retryableQueue, failedQueue := getQueueNames()

	client, err := sqs.NewClient(context.Background(), region, awsProfile)
	if err != nil {
		return fmt.Errorf("failed to create SQS client: %w", err)
	}

	config := tui.Config{
		RetryableQueueName: retryableQueue,
		FailedQueueName:    failedQueue,
		Environment:        env,
		Region:             region,
		Cell:               cell,
		SQSClient:          client,
	}

	model := tui.NewModel(config)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("error running TUI: %w", err)
	}

	return nil
}
