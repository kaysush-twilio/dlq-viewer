package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kaysush-twilio/dlq-viewer/internal/sqs"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Config struct {
	RetryableQueueName string
	FailedQueueName    string
	Environment        string
	Region             string
	Cell               string
	SQSClient          *sqs.Client
}

type QueueType int

const (
	RetryableQueue QueueType = iota
	FailedQueue
)

type Model struct {
	config           Config
	activeQueue      QueueType
	messages         []sqs.Message
	selectedIdx      int
	viewport         viewport.Model
	viewportReady    bool
	detailView       bool
	loading          bool
	err              error
	width            int
	height           int
	retryableURL     string
	failedURL        string
	retryableCount   int
	failedCount      int
	lastRefresh      time.Time
}

type messagesLoadedMsg struct {
	messages []sqs.Message
	err      error
}

type queueInfoMsg struct {
	retryableURL   string
	failedURL      string
	retryableCount int
	failedCount    int
	err            error
}

type tickMsg time.Time

func NewModel(config Config) Model {
	return Model{
		config:      config,
		activeQueue: RetryableQueue,
		loading:     true,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(loadQueueInfo(m.config), tickCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(30*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func loadQueueInfo(config Config) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		retryableURL, err := config.SQSClient.GetQueueURL(ctx, config.RetryableQueueName)
		if err != nil {
			return queueInfoMsg{err: err}
		}

		failedURL, err := config.SQSClient.GetQueueURL(ctx, config.FailedQueueName)
		if err != nil {
			return queueInfoMsg{err: err}
		}

		retryableCount, _ := config.SQSClient.GetQueueAttributes(ctx, retryableURL)
		failedCount, _ := config.SQSClient.GetQueueAttributes(ctx, failedURL)

		return queueInfoMsg{
			retryableURL:   retryableURL,
			failedURL:      failedURL,
			retryableCount: retryableCount,
			failedCount:    failedCount,
		}
	}
}

func loadMessages(client *sqs.Client, queueURL string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		messages, err := client.PeekAllMessages(ctx, queueURL, 100)
		return messagesLoadedMsg{messages: messages, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, keys.Tab):
			if m.activeQueue == RetryableQueue {
				m.activeQueue = FailedQueue
			} else {
				m.activeQueue = RetryableQueue
			}
			m.selectedIdx = 0
			m.detailView = false
			m.loading = true
			return m, m.loadCurrentQueue()
		case key.Matches(msg, keys.Up):
			if !m.detailView && m.selectedIdx > 0 {
				m.selectedIdx--
			} else if m.detailView {
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				cmds = append(cmds, cmd)
			}
		case key.Matches(msg, keys.Down):
			if !m.detailView && m.selectedIdx < len(m.messages)-1 {
				m.selectedIdx++
			} else if m.detailView {
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				cmds = append(cmds, cmd)
			}
		case key.Matches(msg, keys.Enter):
			if len(m.messages) > 0 {
				m.detailView = true
				m.viewport.SetContent(m.formatMessageDetail(m.messages[m.selectedIdx]))
				m.viewport.GotoTop()
			}
		case key.Matches(msg, keys.Back):
			m.detailView = false
		case key.Matches(msg, keys.Refresh):
			m.loading = true
			return m, tea.Batch(loadQueueInfo(m.config), m.loadCurrentQueue())
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		headerHeight := 6
		footerHeight := 2
		verticalMargin := headerHeight + footerHeight

		if !m.viewportReady {
			m.viewport = viewport.New(msg.Width-4, msg.Height-verticalMargin)
			m.viewport.YPosition = headerHeight
			m.viewportReady = true
		} else {
			m.viewport.Width = msg.Width - 4
			m.viewport.Height = msg.Height - verticalMargin
		}

	case queueInfoMsg:
		if msg.err != nil {
			m.err = msg.err
			m.loading = false
			return m, nil
		}
		m.retryableURL = msg.retryableURL
		m.failedURL = msg.failedURL
		m.retryableCount = msg.retryableCount
		m.failedCount = msg.failedCount
		return m, m.loadCurrentQueue()

	case messagesLoadedMsg:
		m.loading = false
		m.lastRefresh = time.Now()
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.messages = msg.messages
		m.err = nil
		if m.selectedIdx >= len(m.messages) {
			m.selectedIdx = max(0, len(m.messages)-1)
		}

	case tickMsg:
		cmds = append(cmds, tea.Batch(loadQueueInfo(m.config), tickCmd()))
	}

	return m, tea.Batch(cmds...)
}

func (m Model) loadCurrentQueue() tea.Cmd {
	if m.activeQueue == RetryableQueue && m.retryableURL != "" {
		return loadMessages(m.config.SQSClient, m.retryableURL)
	} else if m.activeQueue == FailedQueue && m.failedURL != "" {
		return loadMessages(m.config.SQSClient, m.failedURL)
	}
	return nil
}

func (m Model) View() string {
	if m.err != nil {
		return m.errorView()
	}

	var b strings.Builder

	b.WriteString(m.headerView())
	b.WriteString("\n")
	b.WriteString(m.tabsView())
	b.WriteString("\n")

	if m.loading {
		b.WriteString(m.loadingView())
	} else if m.detailView {
		b.WriteString(m.viewport.View())
	} else {
		b.WriteString(m.listView())
	}

	b.WriteString("\n")
	b.WriteString(m.footerView())

	return b.String()
}

func (m Model) headerView() string {
	title := titleStyle.Render("DLQ Viewer")
	info := infoStyle.Render(fmt.Sprintf("Env: %s | Region: %s | Cell: %s",
		m.config.Environment, m.config.Region, m.config.Cell))

	return lipgloss.JoinHorizontal(lipgloss.Top, title, "  ", info)
}

func (m Model) tabsView() string {
	retryableTab := fmt.Sprintf("Retryable (%d)", m.retryableCount)
	failedTab := fmt.Sprintf("Failed (%d)", m.failedCount)

	if m.activeQueue == RetryableQueue {
		retryableTab = activeTabStyle.Render(retryableTab)
		failedTab = inactiveTabStyle.Render(failedTab)
	} else {
		retryableTab = inactiveTabStyle.Render(retryableTab)
		failedTab = activeTabStyle.Render(failedTab)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, retryableTab, " ", failedTab)
}

func (m Model) listView() string {
	if len(m.messages) == 0 {
		return emptyStyle.Render("No messages in queue")
	}

	var rows []string
	visibleRows := m.height - 10
	startIdx := 0
	if m.selectedIdx >= visibleRows {
		startIdx = m.selectedIdx - visibleRows + 1
	}

	for i := startIdx; i < len(m.messages) && i < startIdx+visibleRows; i++ {
		msg := m.messages[i]
		timestamp := msg.Timestamp.Format("2006-01-02 15:04:05")
		preview := truncate(msg.Body, 60)
		retry := msg.RetryCount
		if retry == "" {
			retry = "0"
		}

		row := fmt.Sprintf("%-20s | Retry: %-3s | %s", timestamp, retry, preview)

		if i == m.selectedIdx {
			rows = append(rows, selectedStyle.Render(row))
		} else {
			rows = append(rows, normalStyle.Render(row))
		}
	}

	return strings.Join(rows, "\n")
}

func (m Model) formatMessageDetail(msg sqs.Message) string {
	var b strings.Builder

	b.WriteString(headerDetailStyle.Render("Message ID"))
	b.WriteString("\n")
	b.WriteString(msg.MessageID)
	b.WriteString("\n\n")

	b.WriteString(headerDetailStyle.Render("Timestamp"))
	b.WriteString("\n")
	b.WriteString(msg.Timestamp.Format("2006-01-02 15:04:05 MST"))
	b.WriteString("\n\n")

	b.WriteString(headerDetailStyle.Render("Attributes"))
	b.WriteString("\n")
	for k, v := range msg.Attributes {
		b.WriteString(fmt.Sprintf("  %s: %s\n", attrKeyStyle.Render(k), v))
	}
	b.WriteString("\n")

	b.WriteString(headerDetailStyle.Render("Body"))
	b.WriteString("\n")

	// Try to pretty print JSON
	var prettyJSON map[string]interface{}
	if err := json.Unmarshal([]byte(msg.Body), &prettyJSON); err == nil {
		formatted, _ := json.MarshalIndent(prettyJSON, "", "  ")
		b.WriteString(string(formatted))
	} else {
		b.WriteString(msg.Body)
	}

	return b.String()
}

func (m Model) loadingView() string {
	return loadingStyle.Render("Loading messages...")
}

func (m Model) errorView() string {
	return errorStyle.Render(fmt.Sprintf("Error: %v\n\nPress 'r' to retry or 'q' to quit", m.err))
}

func (m Model) footerView() string {
	var help string
	if m.detailView {
		help = "↑/↓: scroll • esc: back • tab: switch queue • r: refresh • q: quit"
	} else {
		help = "↑/↓: navigate • enter: view details • tab: switch queue • r: refresh • q: quit"
	}

	lastRefresh := ""
	if !m.lastRefresh.IsZero() {
		lastRefresh = fmt.Sprintf(" | Last refresh: %s", m.lastRefresh.Format("15:04:05"))
	}

	return footerStyle.Render(help + lastRefresh)
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}
