package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
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
	config             Config
	activeQueue        QueueType
	messages           []sqs.Message
	retryableMessages  []sqs.Message
	failedMessages     []sqs.Message
	selectedIdx        int
	listScrollOffset   int
	viewport           viewport.Model
	viewportReady      bool
	detailView         bool
	loading            bool
	err                error
	width              int
	height             int
	retryableURL       string
	failedURL          string
	retryableCount     int
	failedCount        int
	lastRefresh        time.Time
}

type messagesLoadedMsg struct {
	messages  []sqs.Message
	queueType QueueType
	err       error
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

func loadMessages(client *sqs.Client, queueURL string, queueType QueueType) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		messages, err := client.PeekAllMessages(ctx, queueURL, 100)
		return messagesLoadedMsg{messages: messages, queueType: queueType, err: err}
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
				m.messages = m.failedMessages
			} else {
				m.activeQueue = RetryableQueue
				m.messages = m.retryableMessages
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
		case key.Matches(msg, keys.PageUp):
			if !m.detailView {
				m.selectedIdx -= 10
				if m.selectedIdx < 0 {
					m.selectedIdx = 0
				}
			} else {
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				cmds = append(cmds, cmd)
			}
		case key.Matches(msg, keys.PageDown):
			if !m.detailView {
				m.selectedIdx += 10
				if m.selectedIdx >= len(m.messages) {
					m.selectedIdx = max(0, len(m.messages)-1)
				}
			} else {
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
		m.err = nil

		// Merge new messages with cached ones
		merged := mergeMessages(m.getCachedMessages(msg.queueType), msg.messages)

		if msg.queueType == RetryableQueue {
			m.retryableMessages = merged
		} else {
			m.failedMessages = merged
		}

		// Update current view if this is the active queue
		if msg.queueType == m.activeQueue {
			m.messages = merged
		}

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
		return loadMessages(m.config.SQSClient, m.retryableURL, RetryableQueue)
	} else if m.activeQueue == FailedQueue && m.failedURL != "" {
		return loadMessages(m.config.SQSClient, m.failedURL, FailedQueue)
	}
	return nil
}

func (m Model) getCachedMessages(qt QueueType) []sqs.Message {
	if qt == RetryableQueue {
		return m.retryableMessages
	}
	return m.failedMessages
}

func mergeMessages(cached, newMsgs []sqs.Message) []sqs.Message {
	seen := make(map[string]bool)
	result := make([]sqs.Message, 0, len(cached)+len(newMsgs))

	// Add all cached messages
	for _, m := range cached {
		if !seen[m.MessageID] {
			seen[m.MessageID] = true
			result = append(result, m)
		}
	}

	// Add new messages not in cache
	for _, m := range newMsgs {
		if !seen[m.MessageID] {
			seen[m.MessageID] = true
			result = append(result, m)
		}
	}

	// Sort by timestamp reverse chronological
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.After(result[j].Timestamp)
	})

	return result
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

	// Header row with count
	header := fmt.Sprintf("%-19s | %-28s | %-36s | %-34s | %-3s   [%d/%d]",
		"Timestamp", "Event Type", "Store ID", "Account SID", "Retry", m.selectedIdx+1, len(m.messages))
	rows = append(rows, headerRowStyle.Render(header))
	rows = append(rows, strings.Repeat("─", min(m.width-4, 170)))

	visibleRows := m.height - 12
	startIdx := 0
	if m.selectedIdx >= visibleRows {
		startIdx = m.selectedIdx - visibleRows + 1
	}

	for i := startIdx; i < len(m.messages) && i < startIdx+visibleRows; i++ {
		msg := m.messages[i]
		timestamp := msg.Timestamp.Format("2006-01-02 15:04:05")
		eventType := truncate(msg.EventType, 28)
		storeID := truncate(msg.StoreID, 36)
		accountSID := truncate(msg.AccountID, 34)
		retry := msg.RetryCount
		if retry == "" {
			retry = "0"
		}
		if eventType == "" {
			eventType = "-"
		}
		if storeID == "" {
			storeID = "-"
		}
		if accountSID == "" {
			accountSID = "-"
		}

		row := fmt.Sprintf("%-19s | %-28s | %-36s | %-34s | %-3s",
			timestamp, eventType, storeID, accountSID, retry)

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

	b.WriteString(headerDetailStyle.Render("Sent Timestamp"))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("%s (age: %s)", msg.Timestamp.Format("2006-01-02 15:04:05 MST"), formatDuration(time.Since(msg.Timestamp))))
	b.WriteString("\n\n")

	// Show ApproximateFirstReceiveTimestamp if available
	if firstReceiveTs, ok := msg.Attributes["ApproximateFirstReceiveTimestamp"]; ok {
		var ts int64
		fmt.Sscanf(firstReceiveTs, "%d", &ts)
		firstReceiveTime := time.UnixMilli(ts)
		b.WriteString(headerDetailStyle.Render("First Receive Timestamp"))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("%s (age: %s)", firstReceiveTime.Format("2006-01-02 15:04:05 MST"), formatDuration(time.Since(firstReceiveTime))))
		b.WriteString("\n\n")
	}

	b.WriteString(headerDetailStyle.Render("Attributes"))
	b.WriteString("\n")
	for k, v := range msg.Attributes {
		// Format epoch timestamps
		if isTimestampAttr(k) {
			var ts int64
			fmt.Sscanf(v, "%d", &ts)
			t := time.UnixMilli(ts)
			v = fmt.Sprintf("%s (%s, age: %s)", v, t.Format("2006-01-02 15:04:05 MST"), formatDuration(time.Since(t)))
		}
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
		help = "↑/↓: scroll • PgUp/PgDn: page • esc: back • tab: switch queue • r: refresh • q: quit"
	} else {
		help = "↑/↓: navigate • PgUp/PgDn: page • enter: view details • tab: switch queue • r: refresh • q: quit"
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

func isTimestampAttr(key string) bool {
	return strings.HasSuffix(key, "Timestamp")
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	return fmt.Sprintf("%dd %dh", days, hours)
}
