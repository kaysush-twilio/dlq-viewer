package sqs

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type Client struct {
	sqs    *sqs.Client
	region string
}

type Message struct {
	MessageID     string
	Body          string
	ReceiptHandle string
	Attributes    map[string]string
	Timestamp     time.Time
	RetryCount    string
}

func NewClient(ctx context.Context, region, profile string) (*Client, error) {
	var opts []func(*config.LoadOptions) error

	opts = append(opts, config.WithRegion(region))

	if profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(profile))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &Client{
		sqs:    sqs.NewFromConfig(cfg),
		region: region,
	}, nil
}

func (c *Client) GetQueueURL(ctx context.Context, queueName string) (string, error) {
	result, err := c.sqs.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: aws.String(queueName),
	})
	if err != nil {
		return "", fmt.Errorf("failed to get queue URL for %s: %w", queueName, err)
	}
	return *result.QueueUrl, nil
}

func (c *Client) GetQueueAttributes(ctx context.Context, queueURL string) (approxMessages int, err error) {
	result, err := c.sqs.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl: aws.String(queueURL),
		AttributeNames: []types.QueueAttributeName{
			types.QueueAttributeNameApproximateNumberOfMessages,
		},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get queue attributes: %w", err)
	}

	if val, ok := result.Attributes[string(types.QueueAttributeNameApproximateNumberOfMessages)]; ok {
		fmt.Sscanf(val, "%d", &approxMessages)
	}
	return approxMessages, nil
}

func (c *Client) ReceiveMessages(ctx context.Context, queueURL string, maxMessages int) ([]Message, error) {
	result, err := c.sqs.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:              aws.String(queueURL),
		MaxNumberOfMessages:   int32(min(maxMessages, 10)),
		WaitTimeSeconds:       1,
		VisibilityTimeout:     0, // Don't hide messages, just peek
		MessageAttributeNames: []string{"All"},
		AttributeNames:        []types.QueueAttributeName{types.QueueAttributeNameAll},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to receive messages: %w", err)
	}

	messages := make([]Message, 0, len(result.Messages))
	for _, msg := range result.Messages {
		m := Message{
			MessageID:     aws.ToString(msg.MessageId),
			Body:          aws.ToString(msg.Body),
			ReceiptHandle: aws.ToString(msg.ReceiptHandle),
			Attributes:    make(map[string]string),
		}

		for k, v := range msg.Attributes {
			m.Attributes[k] = v
		}

		for k, v := range msg.MessageAttributes {
			if v.StringValue != nil {
				m.Attributes[k] = *v.StringValue
			}
		}

		if sentTs, ok := msg.Attributes["SentTimestamp"]; ok {
			var ts int64
			fmt.Sscanf(sentTs, "%d", &ts)
			m.Timestamp = time.UnixMilli(ts)
		}

		if retry, ok := m.Attributes["retryAttempt"]; ok {
			m.RetryCount = retry
		}

		messages = append(messages, m)
	}

	// Sort by timestamp in reverse chronological order
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp.After(messages[j].Timestamp)
	})

	return messages, nil
}

func (c *Client) PeekAllMessages(ctx context.Context, queueURL string, maxTotal int) ([]Message, error) {
	var allMessages []Message
	seen := make(map[string]bool)

	for len(allMessages) < maxTotal {
		msgs, err := c.ReceiveMessages(ctx, queueURL, 10)
		if err != nil {
			return allMessages, err
		}

		if len(msgs) == 0 {
			break
		}

		newFound := false
		for _, m := range msgs {
			if !seen[m.MessageID] {
				seen[m.MessageID] = true
				allMessages = append(allMessages, m)
				newFound = true
			}
		}

		if !newFound {
			break
		}
	}

	// Sort all by timestamp reverse chronological
	sort.Slice(allMessages, func(i, j int) bool {
		return allMessages[i].Timestamp.After(allMessages[j].Timestamp)
	})

	return allMessages, nil
}
